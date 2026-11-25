package runner

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
	"wdev/internal/i18n"
	"wdev/internal/store"
)

type EventType string

const (
	EventLog    EventType = "log"
	EventStart  EventType = "start"
	EventFinish EventType = "finish"

	maxLiveEvents = 5000
)

type TaskEvent struct {
	Seq       int64     `json:"seq"`
	TaskID    int64     `json:"task_id"`
	Type      EventType `json:"type"`
	Message   string    `json:"message"`
	Status    string    `json:"status,omitempty"`
	Replay    bool      `json:"replay,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

type Runner struct {
	store       *store.Store
	instanceID  string
	mu          sync.RWMutex
	subscribers map[chan TaskEvent]struct{}
	activeTask  *ActiveTask
	activeLog   strings.Builder
	liveEvents  []TaskEvent
	seq         int64
}

type ActiveTask struct {
	ID        int64
	Command   string
	StartedAt time.Time
}

func New(s *store.Store) *Runner {
	return &Runner{
		store:       s,
		instanceID:  fmt.Sprintf("%d", time.Now().UnixNano()),
		subscribers: make(map[chan TaskEvent]struct{}),
	}
}

func (r *Runner) InstanceID() string {
	return r.instanceID
}

func (r *Runner) Subscribe() chan TaskEvent {
	_, ch := r.SubscribeFrom("", 0)
	return ch
}

// SubscribeFrom secara atomik mengambil snapshot event yang belum diterima klien
// (berdasarkan instanceID dan lastSeq) serta mendaftarkan channel live baru.
func (r *Runner) SubscribeFrom(clientInstanceID string, lastSeq int64) ([]TaskEvent, chan TaskEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if clientInstanceID != "" && clientInstanceID != r.instanceID {
		lastSeq = 0
	}

	var replay []TaskEvent
	for _, ev := range r.liveEvents {
		if ev.Seq > lastSeq {
			ev.Replay = true
			replay = append(replay, ev)
		}
	}

	ch := make(chan TaskEvent, 256)
	r.subscribers[ch] = struct{}{}
	return replay, ch
}

func (r *Runner) Unsubscribe(ch chan TaskEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.subscribers, ch)
	close(ch)
}

func (r *Runner) emit(taskID int64, typ EventType, message, status string, clearActive bool) {
	r.mu.Lock()
	evt := r.appendAndBroadcastLocked(taskID, typ, message, status, clearActive)
	_ = evt
	r.mu.Unlock()
}

func (r *Runner) appendAndBroadcastLocked(taskID int64, typ EventType, message, status string, clearActive bool) TaskEvent {
	r.seq++
	evt := TaskEvent{
		Seq:       r.seq,
		TaskID:    taskID,
		Type:      typ,
		Message:   message,
		Status:    status,
		Timestamp: time.Now(),
	}

	if typ == EventStart {
		r.liveEvents = append([]TaskEvent(nil), evt)
		r.activeLog.Reset()
	} else {
		if typ == EventLog {
			r.activeLog.WriteString(message + "\n")
		}
		if len(r.liveEvents) >= maxLiveEvents {
			if len(r.liveEvents) > 1 && r.liveEvents[0].Type == EventStart {
				copy(r.liveEvents[1:], r.liveEvents[2:])
				r.liveEvents[len(r.liveEvents)-1] = evt
			} else {
				copy(r.liveEvents, r.liveEvents[1:])
				r.liveEvents[len(r.liveEvents)-1] = evt
			}
		} else {
			r.liveEvents = append(r.liveEvents, evt)
		}
	}

	if clearActive {
		r.activeTask = nil
	}

	for ch := range r.subscribers {
		select {
		case ch <- evt:
		default:
			// Buffer full, skip to prevent blocking
		}
	}
	return evt
}

// StartNativeTask runs a native Go function while broadcasting logs (English default)
func (r *Runner) StartNativeTask(name string, fn func(ctx context.Context, cb func(string)) error) (int64, error) {
	return r.StartNativeTaskAfterLocale(i18n.DefaultLocale, name, fn, nil)
}

// StartNativeTaskLocale runs a native Go function while broadcasting localized logs.
func (r *Runner) StartNativeTaskLocale(lang, name string, fn func(ctx context.Context, cb func(string)) error) (int64, error) {
	return r.StartNativeTaskAfterLocale(lang, name, fn, nil)
}

// StartNativeTaskAfter runs a native Go function while broadcasting logs and invokes
// afterFinish only after the task record and SSE finish event have been emitted.
func (r *Runner) StartNativeTaskAfter(name string, fn func(ctx context.Context, cb func(string)) error, afterFinish func(status string)) (int64, error) {
	return r.StartNativeTaskAfterLocale(i18n.DefaultLocale, name, fn, afterFinish)
}

// StartNativeTaskAfterLocale is the locale-aware implementation of StartNativeTaskAfter.
func (r *Runner) StartNativeTaskAfterLocale(lang, name string, fn func(ctx context.Context, cb func(string)) error, afterFinish func(status string)) (int64, error) {
	loc := i18n.Normalize(lang)
	r.mu.Lock()
	if r.activeTask != nil {
		r.mu.Unlock()
		return 0, fmt.Errorf(i18n.Pick(loc, "a task is already running: %s", "ada tugas yang sedang berjalan: %s"), r.activeTask.Command)
	}

	var taskID int64
	if r.store != nil {
		var err error
		taskID, err = r.store.CreateTask(name)
		if err != nil {
			r.mu.Unlock()
			return 0, err
		}
	}

	r.activeTask = &ActiveTask{
		ID:        taskID,
		Command:   name,
		StartedAt: time.Now(),
	}
	r.appendAndBroadcastLocked(taskID, EventStart, fmt.Sprintf(i18n.Pick(loc, "Starting: %s", "Memulai: %s"), name), "", false)
	r.mu.Unlock()

	go func() {
		ctx := context.Background()
		var logBuf strings.Builder
		var logMu sync.Mutex

		cb := func(line string) {
			logMu.Lock()
			logBuf.WriteString(line + "\n")
			logMu.Unlock()

			r.emit(taskID, EventLog, line, "", false)
		}

		err := fn(ctx, cb)
		status := "success"
		msg := i18n.Pick(loc, "Completed successfully.", "Selesai dengan sukses.")
		if err != nil {
			status = "failed"
			msg = fmt.Sprintf(i18n.Pick(loc, "Failed with error: %v", "Gagal dengan error: %v"), err)
		}

		if r.store != nil {
			_ = r.store.FinishTask(taskID, status, logBuf.String())
		}
		r.emit(taskID, EventFinish, msg, status, true)
		if afterFinish != nil {
			afterFinish(status)
		}
	}()

	return taskID, nil
}

func (r *Runner) GetActiveTask() *ActiveTask {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.activeTask == nil {
		return nil
	}
	cp := *r.activeTask
	return &cp
}

// GetActiveTaskSnapshot mengembalikan metadata tugas aktif beserta log sementara yang sudah terkumpul.
func (r *Runner) GetActiveTaskSnapshot() (*ActiveTask, string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.activeTask == nil {
		return nil, ""
	}
	cp := *r.activeTask
	return &cp, r.activeLog.String()
}
