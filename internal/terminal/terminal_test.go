package terminal

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"wdev/internal/config"
)

func TestResolveShellAndStartDir(t *testing.T) {
	mgr := New(&config.Config{WebRootBase: t.TempDir()}, nil)

	shells := mgr.AvailableShells()
	if len(shells) == 0 {
		t.Fatalf("expected at least one usable shell")
	}

	target, err := mgr.ResolveTargetUser("")
	if err != nil || target.Username == "" {
		t.Fatalf("ResolveTargetUser default failed: target=%+v err=%v", target, err)
	}

	if _, err := mgr.ResolveTargetUser("nonexistent_wdev_user_xyz"); err == nil {
		t.Fatalf("expected nonexistent target user to be rejected")
	}

	sh, err := mgr.ResolveShell("", target.Shell)
	if err != nil || sh == "" {
		t.Fatalf("ResolveShell default failed: sh=%q err=%v", sh, err)
	}

	if _, err := mgr.ResolveShell("/bin/rm", target.Shell); err == nil {
		t.Fatalf("expected non-shell binary /bin/rm to be rejected")
	}

	dir := t.TempDir()
	filePath := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(filePath, []byte("ok"), 0644); err != nil {
		t.Fatal(err)
	}

	gotDir, note := mgr.ResolveStartDir(dir, target)
	if gotDir != dir || note != "" {
		t.Fatalf("ResolveStartDir(dir) = (%q, %q), want (%q, \"\")", gotDir, note, dir)
	}

	gotFileParent, _ := mgr.ResolveStartDir(filePath, target)
	if gotFileParent != dir {
		t.Fatalf("ResolveStartDir(file) = %q, want parent %q", gotFileParent, dir)
	}

	missing := filepath.Join(dir, "does-not-exist")
	gotFallback, fbNote := mgr.ResolveStartDir(missing, target)
	if gotFallback == missing || fbNote == "" {
		t.Fatalf("expected fallback and reason for missing dir, got (%q, %q)", gotFallback, fbNote)
	}
}

func TestPTYSessionStartReadWriteResizeAndCleanup(t *testing.T) {
	workDir := t.TempDir()
	mgr := New(&config.Config{WebRootBase: workDir}, nil)

	sess, _, err := mgr.StartSession(StartOptions{
		Cwd:   workDir,
		Shell: "/bin/sh",
		Cols:  100,
		Rows:  30,
	})
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}

	if info := mgr.SystemInfo(); info.ActiveSessions != 1 {
		t.Fatalf("expected 1 active session, got %d", info.ActiveSessions)
	}

	if err := sess.Resize(132, 40); err != nil {
		t.Fatalf("Resize failed: %v", err)
	}

	outCh := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		chunk := make([]byte, 4096)
		for {
			n, rerr := sess.Read(chunk)
			if n > 0 {
				buf.Write(chunk[:n])
			}
			if rerr != nil {
				break
			}
		}
		outCh <- buf.String()
	}()

	cmdSeq := "pwd && echo __WDEV_PTY_OK__ && exit 0\n"
	if _, err := sess.Write([]byte(cmdSeq)); err != nil {
		t.Fatalf("Write to PTY failed: %v", err)
	}

	done := make(chan int, 1)
	go func() {
		code, _ := sess.Wait()
		_ = sess.Close()
		done <- code
	}()

	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
	case <-time.After(5 * time.Second):
		_ = sess.Close()
		t.Fatal("timed out waiting for PTY session exit")
	}

	var output string
	select {
	case output = <-outCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for PTY output")
	}

	if !strings.Contains(output, "__WDEV_PTY_OK__") {
		t.Fatalf("expected __WDEV_PTY_OK__ in PTY output, got:\n%s", output)
	}

	if info := mgr.SystemInfo(); info.ActiveSessions != 0 {
		t.Fatalf("expected 0 active sessions after exit, got %d", info.ActiveSessions)
	}
}

func TestPTYSessionIdleTimeoutClosesShell(t *testing.T) {
	workDir := t.TempDir()
	mgr := New(&config.Config{WebRootBase: workDir}, nil)

	sess, _, err := mgr.StartSession(StartOptions{
		Cwd:         workDir,
		Shell:       "/bin/sh",
		IdleTimeout: 120 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}
	defer sess.Close()

	done := make(chan struct{})
	go func() {
		_, _ = sess.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("expected idle PTY session to auto-close")
	}

	if sess.Reason() != "idle_timeout" {
		t.Fatalf("expected close reason idle_timeout, got %q", sess.Reason())
	}
	if info := mgr.SystemInfo(); info.ActiveSessions != 0 {
		t.Fatalf("expected 0 active sessions after idle timeout, got %d", info.ActiveSessions)
	}
}
