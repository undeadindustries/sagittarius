package bgproc

import (
	"os/exec"
	"runtime"
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
	go cmd.Wait()

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
