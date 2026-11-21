package store

import (
	"path/filepath"
	"testing"
)

func TestTaskHistoryRunningAndFinishedScan(t *testing.T) {
	db, err := New(filepath.Join(t.TempDir(), "tasks.db"))
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer db.Close()

	runningID, err := db.CreateTask("Install NetBird")
	if err != nil {
		t.Fatalf("CreateTask(running) failed: %v", err)
	}

	finishedID, err := db.CreateTask("Check Updates")
	if err != nil {
		t.Fatalf("CreateTask(finished) failed: %v", err)
	}
	if err := db.FinishTask(finishedID, "success", "line-a\nline-b\n"); err != nil {
		t.Fatalf("FinishTask failed: %v", err)
	}

	tasks, err := db.ListRecentTasks(10)
	if err != nil {
		t.Fatalf("ListRecentTasks failed: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}

	if tasks[0].ID != finishedID || tasks[0].Status != "success" || tasks[0].LogOutput != "line-a\nline-b\n" {
		t.Fatalf("unexpected finished task: %+v", tasks[0])
	}
	if tasks[0].FinishedAt.IsZero() {
		t.Fatalf("expected non-zero FinishedAt for finished task")
	}

	if tasks[1].ID != runningID || tasks[1].Status != "running" {
		t.Fatalf("unexpected running task: %+v", tasks[1])
	}
	if !tasks[1].FinishedAt.Equal(tasks[1].StartedAt) {
		t.Fatalf("expected running task FinishedAt fallback to StartedAt, got started=%v finished=%v", tasks[1].StartedAt, tasks[1].FinishedAt)
	}
}
