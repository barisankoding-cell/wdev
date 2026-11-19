package runner

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
	"wdev/internal/store"
)

type EventType string

const (
	EventLog    EventType = "log"
	EventStart  EventType = "start"
	EventFinish EventType = "finish"
)

type TaskEvent struct {
	TaskID    int64     `json:"task_id"`
	Type      EventType `json:"type"`
	Message   string    `json:"message"`
	Status    string    `json:"status,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

type Runner struct {
	store       *store.Store
	mu          sync.RWMutex
	subscribers map[chan TaskEvent]struct{}
	activeTask  *ActiveTask
}

type ActiveTask struct {
	ID        int64
	Command   string
	Cmd       *exec.Cmd
	StartedAt time.Time
}

func New(s *store.Store) *Runner {
	return &Runner{
		store:       s,
		subscribers: make(map[chan TaskEvent]struct{}),
	}
}

func (r *Runner) Subscribe() chan TaskEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch := make(chan TaskEvent, 100)
	r.subscribers[ch] = struct{}{}
	return ch
}

func (r *Runner) Unsubscribe(ch chan TaskEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.subscribers, ch)
	close(ch)
}

func (r *Runner) broadcast(event TaskEvent) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for ch := range r.subscribers {
		select {
		case ch <- event:
		default:
			// Buffer full, skip to prevent blocking
		}
	}
}

// StartCommand executes a bash command and broadcasts logs in real time
func (r *Runner) StartCommand(commandStr string, workingDir string) (int64, error) {
	r.mu.Lock()
	if r.activeTask != nil {
		r.mu.Unlock()
		return 0, fmt.Errorf("ada tugas yang sedang berjalan: %s", r.activeTask.Command)
	}

	taskID, err := r.store.CreateTask(commandStr)
	if err != nil {
		r.mu.Unlock()
		return 0, err
	}

	cmd := exec.Command("bash", "-c", commandStr)
	if workingDir != "" {
		cmd.Dir = workingDir
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		r.mu.Unlock()
		return 0, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		r.mu.Unlock()
		return 0, err
	}

	r.activeTask = &ActiveTask{
		ID:        taskID,
		Command:   commandStr,
		Cmd:       cmd,
		StartedAt: time.Now(),
	}
	r.mu.Unlock()

	r.broadcast(TaskEvent{
		TaskID:    taskID,
		Type:      EventStart,
		Message:   fmt.Sprintf("Memulai: %s", commandStr),
		Timestamp: time.Now(),
	})

	if err := cmd.Start(); err != nil {
		r.mu.Lock()
		r.activeTask = nil
		r.mu.Unlock()
		_ = r.store.FinishTask(taskID, "failed", err.Error())
		r.broadcast(TaskEvent{
			TaskID:    taskID,
			Type:      EventFinish,
			Status:    "failed",
			Message:   err.Error(),
			Timestamp: time.Now(),
		})
		return 0, err
	}

	// Read stdout and stderr concurrently
	go func() {
		var logBuf strings.Builder
		var logMu sync.Mutex

		readPipe := func(pipe io.Reader, prefix string) {
			scanner := bufio.NewScanner(pipe)
			for scanner.Scan() {
				line := scanner.Text()
				logMu.Lock()
				logBuf.WriteString(line + "\n")
				logMu.Unlock()

				r.broadcast(TaskEvent{
					TaskID:    taskID,
					Type:      EventLog,
					Message:   line,
					Timestamp: time.Now(),
				})
			}
		}

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			readPipe(stdout, "")
		}()
		go func() {
			defer wg.Done()
			readPipe(stderr, "")
		}()

		wg.Wait()
		waitErr := cmd.Wait()

		status := "success"
		msg := "Selesai dengan sukses."
		if waitErr != nil {
			status = "failed"
			msg = fmt.Sprintf("Gagal dengan error: %v", waitErr)
		}

		r.mu.Lock()
		r.activeTask = nil
		r.mu.Unlock()

		_ = r.store.FinishTask(taskID, status, logBuf.String())

		r.broadcast(TaskEvent{
			TaskID:    taskID,
			Type:      EventFinish,
			Status:    status,
			Message:   msg,
			Timestamp: time.Now(),
		})
	}()

	return taskID, nil
}

// StartNativeTask runs a native Go function while broadcasting logs
func (r *Runner) StartNativeTask(name string, fn func(ctx context.Context, cb func(string)) error) (int64, error) {
	r.mu.Lock()
	if r.activeTask != nil {
		r.mu.Unlock()
		return 0, fmt.Errorf("ada tugas yang sedang berjalan: %s", r.activeTask.Command)
	}

	taskID, err := r.store.CreateTask(name)
	if err != nil {
		r.mu.Unlock()
		return 0, err
	}

	r.activeTask = &ActiveTask{
		ID:        taskID,
		Command:   name,
		StartedAt: time.Now(),
	}
	r.mu.Unlock()

	r.broadcast(TaskEvent{
		TaskID:    taskID,
		Type:      EventStart,
		Message:   fmt.Sprintf("Memulai: %s", name),
		Timestamp: time.Now(),
	})

	go func() {
		ctx := context.Background()
		var logBuf strings.Builder
		var logMu sync.Mutex

		cb := func(line string) {
			logMu.Lock()
			logBuf.WriteString(line + "\n")
			logMu.Unlock()

			r.broadcast(TaskEvent{
				TaskID:    taskID,
				Type:      EventLog,
				Message:   line,
				Timestamp: time.Now(),
			})
		}

		err := fn(ctx, cb)
		status := "success"
		msg := "Selesai dengan sukses."
		if err != nil {
			status = "failed"
			msg = fmt.Sprintf("Gagal dengan error: %v", err)
		}

		r.mu.Lock()
		r.activeTask = nil
		r.mu.Unlock()

		_ = r.store.FinishTask(taskID, status, logBuf.String())

		r.broadcast(TaskEvent{
			TaskID:    taskID,
			Type:      EventFinish,
			Status:    status,
			Message:   msg,
			Timestamp: time.Now(),
		})
	}()

	return taskID, nil
}

func (r *Runner) GetActiveTask() *ActiveTask {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.activeTask
}
