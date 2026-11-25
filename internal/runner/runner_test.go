package runner

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"wdev/internal/store"
)

func TestRunnerReplayAndActiveSnapshot(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "runner.db"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer db.Close()

	r := New(db)

	started := make(chan struct{})
	release := make(chan struct{})

	taskID, err := r.StartNativeTask("Uji Live Log", func(ctx context.Context, cb func(string)) error {
		cb("baris-1")
		cb("baris-2")
		close(started)
		<-release
		cb("baris-3")
		return nil
	})
	if err != nil {
		t.Fatalf("StartNativeTask failed: %v", err)
	}
	<-started

	// Active snapshot must expose accumulated logs while task is still running
	active, liveLog := r.GetActiveTaskSnapshot()
	if active == nil || active.ID != taskID {
		t.Fatalf("expected active task ID %d, got %+v", taskID, active)
	}
	if !strings.Contains(liveLog, "baris-1\n") || !strings.Contains(liveLog, "baris-2\n") {
		t.Fatalf("expected active log to contain baris-1 and baris-2, got %q", liveLog)
	}

	// Reconnecting subscriber (simulates reopening terminal/browser mid-task) receives replay
	replayAll, chAll := r.SubscribeFrom("", 0)
	defer r.Unsubscribe(chAll)
	if len(replayAll) < 3 {
		t.Fatalf("expected >= 3 replayed events (start + 2 logs), got %d", len(replayAll))
	}
	if replayAll[0].Type != EventStart || !replayAll[0].Replay {
		t.Fatalf("expected first replayed event to be start with Replay=true, got %+v", replayAll[0])
	}

	if !strings.Contains(replayAll[0].Message, "Starting:") {
		t.Fatalf("expected default start message to use English 'Starting:', got %q", replayAll[0].Message)
	}

	// Subscriber resuming with matching instanceID + lastSeq skips already-seen events
	lastSeq := replayAll[1].Seq
	replayDelta, chDelta := r.SubscribeFrom(r.InstanceID(), lastSeq)
	defer r.Unsubscribe(chDelta)
	for _, ev := range replayDelta {
		if ev.Seq <= lastSeq {
			t.Fatalf("unexpected duplicate event seq %d <= %d", ev.Seq, lastSeq)
		}
	}

	// Subscriber with stale instanceID (server restarted) replays from beginning
	replayStale, chStale := r.SubscribeFrom("old-instance", lastSeq)
	defer r.Unsubscribe(chStale)
	if len(replayStale) != len(replayAll) {
		t.Fatalf("expected full replay on mismatched instanceID (%d), got %d", len(replayAll), len(replayStale))
	}

	close(release)

	// Wait for EventFinish on live channel
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-chAll:
			if ev.Type == EventFinish {
				// Reconnecting AFTER task finished must still replay full logs from liveEvents
				replayFinished, chFinished := r.SubscribeFrom("", 0)
				defer r.Unsubscribe(chFinished)
				if len(replayFinished) < 5 {
					t.Fatalf("expected finished task replay (>=5 events), got %d", len(replayFinished))
				}
				if replayFinished[len(replayFinished)-1].Type != EventFinish {
					t.Fatalf("expected last replayed event to be finish, got %+v", replayFinished[len(replayFinished)-1])
				}

				tasks, err := db.ListRecentTasks(5)
				if err != nil || len(tasks) == 0 {
					t.Fatalf("expected task persisted in store: %v", err)
				}
				if !strings.Contains(tasks[0].LogOutput, "baris-3") {
					t.Fatalf("expected persisted store log_output to include baris-3, got %q", tasks[0].LogOutput)
				}
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for EventFinish")
		}
	}
}

func TestStartNativeTaskAfterRunsHookAfterFinishPersisted(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "runner-after.db"))
	if err != nil {
		t.Fatalf("store.New failed: %v", err)
	}
	defer db.Close()

	r := New(db)
	ch := r.Subscribe()
	defer r.Unsubscribe(ch)

	hookDone := make(chan string, 1)
	var hookTasks []store.TaskRecord
	_, err = r.StartNativeTaskAfter("Pembaruan Biner wdev", func(ctx context.Context, cb func(string)) error {
		cb("unduh selesai")
		return nil
	}, func(status string) {
		hookTasks, _ = db.ListRecentTasks(1)
		hookDone <- status
	})
	if err != nil {
		t.Fatalf("StartNativeTaskAfter failed: %v", err)
	}

	var gotStatus string
	select {
	case gotStatus = <-hookDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for afterFinish hook")
	}
	if gotStatus != "success" {
		t.Fatalf("afterFinish status = %q, want success", gotStatus)
	}
	if len(hookTasks) != 1 || hookTasks[0].Status != "success" {
		t.Fatalf("expected FinishTask persisted before afterFinish hook, got %+v", hookTasks)
	}

	sawFinish := false
	deadline := time.After(time.Second)
	for !sawFinish {
		select {
		case ev := <-ch:
			if ev.Type == EventFinish && ev.Status == "success" {
				sawFinish = true
			}
		case <-deadline:
			t.Fatal("expected EventFinish emitted before afterFinish")
		}
	}
}
