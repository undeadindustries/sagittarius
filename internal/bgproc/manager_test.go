package bgproc

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestManager(t *testing.T) {
	mgr := NewManager()

	cmd := exec.Command("sleep", "2")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { _ = cmd.Wait() }()

	mgr.Register(cmd.Process.Pid, cmd.Process.Pid, "sleep 2", "")

	// kill it
	if err := mgr.Kill(cmd.Process.Pid); err != nil {
		t.Fatal(err)
	}

	time.Sleep(2 * time.Second) // wait for reaper

	p, ok := mgr.Get(cmd.Process.Pid)
	if !ok {
		t.Fatal("expected proc to still be in registry")
	}
	if p.Status != StatusExited {
		t.Fatalf("expected exited, got %s", p.Status)
	}
}

// TestReaperIsSingleGoroutineAndStops asserts that registering N processes
// spawns one reaper goroutine (not one per PID, the old behavior) and that
// Close() stops it. Goroutine counts are inherently noisy, so the assertions use
// generous deltas and poll with timeouts rather than exact equality.
func TestReaperIsSingleGoroutineAndStops(t *testing.T) {
	baseline := runtime.NumGoroutine()

	mgr := NewManager()
	const n = 5
	for i := 0; i < n; i++ {
		cmd := exec.Command("sleep", "0.05")
		if err := cmd.Start(); err != nil {
			t.Fatalf("start: %v", err)
		}
		pid := cmd.Process.Pid
		// Wait so the PID is truly gone (reaped), exercising the reaper's
		// exit-detection path without leaving zombies.
		_ = cmd.Wait()
		mgr.Register(pid, 0, "sleep", "")
	}

	// Let the single reaper start. A goroutine-per-PID design would show a delta
	// near n; a single reaper shows ~1 (allow slack for scheduler/runtime noise).
	time.Sleep(50 * time.Millisecond)
	if delta := runtime.NumGoroutine() - baseline; delta > 2 {
		t.Fatalf("goroutine delta = %d after %d registers; expected a single reaper, not per-PID", delta, n)
	}

	if got := len(mgr.List()); got != n {
		t.Fatalf("List() len = %d, want %d", got, n)
	}

	// The reaper should eventually mark the (dead) PIDs exited.
	deadline := time.Now().Add(3 * time.Second)
	for {
		allExited := true
		for _, p := range mgr.List() {
			if p.Status != StatusExited {
				allExited = false
			}
		}
		if allExited {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("reaper did not mark registered processes exited")
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Close stops the reaper goroutine; the count should return to baseline.
	if err := mgr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	stopDeadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > baseline+1 {
		if time.Now().After(stopDeadline) {
			t.Fatalf("reaper goroutine did not stop after Close (delta %d)", runtime.NumGoroutine()-baseline)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestCloseRemovesExitedLogsKeepsRunning(t *testing.T) {
	mgr := NewManager()
	t.Cleanup(func() { _ = mgr.Close() })

	running := exec.Command("sleep", "8")
	if err := running.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = running.Process.Kill()
		_ = running.Wait()
	})

	dir := t.TempDir()
	runningLog := filepath.Join(dir, "running.log")
	exitedLog := filepath.Join(dir, "exited.log")
	for _, p := range []string{runningLog, exitedLog} {
		if err := os.WriteFile(p, []byte("log"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	dead := exec.Command("true")
	if err := dead.Run(); err != nil {
		t.Fatal(err)
	}

	mgr.Register(running.Process.Pid, running.Process.Pid, "sleep 8", runningLog)
	mgr.Register(dead.Process.Pid, 0, "true", exitedLog)
	mgr.reapOnce()

	if err := mgr.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(exitedLog); !os.IsNotExist(err) {
		t.Fatalf("exited log still present: %v", err)
	}
	if _, err := os.Stat(runningLog); err != nil {
		t.Fatalf("running log was removed: %v", err)
	}
	p, ok := mgr.Get(dead.Process.Pid)
	if !ok {
		t.Fatal("exited process row missing from List")
	}
	if p.LogPath != "" {
		t.Fatalf("exited LogPath = %q, want empty after Close", p.LogPath)
	}
}

// TestSharedLogSurvivesParentExit covers the `&`-child case: captureJobs
// registers the child with the parent shell's log path, so the parent exiting
// must not unlink a log the live child is still writing to.
func TestSharedLogSurvivesParentExit(t *testing.T) {
	mgr := NewManager()
	t.Cleanup(func() { _ = mgr.Close() })

	child := exec.Command("sleep", "8")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = child.Process.Kill()
		_ = child.Wait()
	})

	sharedLog := filepath.Join(t.TempDir(), "shared.log")
	if err := os.WriteFile(sharedLog, []byte("log"), 0o600); err != nil {
		t.Fatal(err)
	}

	parent := exec.Command("true")
	if err := parent.Run(); err != nil {
		t.Fatal(err)
	}

	mgr.Register(parent.Process.Pid, parent.Process.Pid, "sleep 8 &", sharedLog)
	mgr.Register(child.Process.Pid, 0, "sleep 8 & (& child)", sharedLog)
	mgr.reapOnce()

	if _, err := os.Stat(sharedLog); err != nil {
		t.Fatalf("shared log removed while the child is running: %v", err)
	}
	if err := mgr.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sharedLog); err != nil {
		t.Fatalf("Close removed a log the running child still writes to: %v", err)
	}
	live, ok := mgr.Get(child.Process.Pid)
	if !ok {
		t.Fatal("child process row missing")
	}
	if live.LogPath != sharedLog {
		t.Fatalf("child LogPath = %q, want %q", live.LogPath, sharedLog)
	}
}

func TestRetainedLogsEvictOldest(t *testing.T) {
	mgr := NewManager()
	t.Cleanup(func() { _ = mgr.Close() })

	dir := t.TempDir()
	n := maxRetainedLogs + 1
	pids := make([]int, n)
	logs := make([]string, n)
	seen := map[int]bool{}
	for i := 0; i < n; i++ {
		cmd := exec.Command("true")
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
		pids[i] = cmd.Process.Pid
		if seen[pids[i]] {
			t.Fatalf("PID %d reused; cannot test eviction", pids[i])
		}
		seen[pids[i]] = true
		logs[i] = filepath.Join(dir, "log-"+strconv.Itoa(i)+".log")
		if err := os.WriteFile(logs[i], []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		mgr.Register(pids[i], 0, "true", logs[i])
	}
	mgr.reapOnce()

	if _, err := os.Stat(logs[0]); !os.IsNotExist(err) {
		t.Fatalf("oldest log still present: %v", err)
	}
	oldest, ok := mgr.Get(pids[0])
	if !ok {
		t.Fatal("oldest process missing from List")
	}
	if oldest.LogPath != "" {
		t.Fatalf("evicted LogPath = %q, want empty", oldest.LogPath)
	}
	if oldest.Status != StatusExited {
		t.Fatalf("status = %s, want exited", oldest.Status)
	}
	for i := 1; i < n; i++ {
		if _, err := os.Stat(logs[i]); err != nil {
			t.Fatalf("retained log %d missing: %v", i, err)
		}
		p, ok := mgr.Get(pids[i])
		if !ok || p.LogPath != logs[i] {
			t.Fatalf("pid %d LogPath = %q, want %q", pids[i], p.LogPath, logs[i])
		}
	}
}
