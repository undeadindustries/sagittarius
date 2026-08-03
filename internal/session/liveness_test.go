package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// The two-process coverage re-execs the test binary (os.Args[0]) with
// SAG_LIVENESS_HOLDER=1, following the internal/lsp/lspfake_test.go pattern.
// The child process acquires the lock named by SAG_LIVENESS_LOCK and sleeps,
// so the parent can probe it and observe ProbeLive while the child is alive,
// then ProbeFree after the child exits and the kernel releases the flock.

func TestMain(m *testing.M) {
	if os.Getenv("SAG_LIVENESS_HOLDER") == "1" {
		runLivenessHolder()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// runLivenessHolder acquires the requested lock and blocks until killed, so
// the parent test can probe a live lock held by a separate process.
func runLivenessHolder() {
	dir := os.Getenv("SAG_LIVENESS_DIR")
	sid := os.Getenv("SAG_LIVENESS_LOCK")
	release := Acquire(dir, LivenessInfo{SessionID: sid})
	if release == nil {
		os.Exit(2) // signal acquisition failure to the parent
	}
	defer release()
	// Signal readiness by touching a file the parent waits for.
	_ = os.WriteFile(filepath.Join(dir, sid+".ready"), []byte("1"), 0o600)
	time.Sleep(30 * time.Second)
}

// TestLivenessProbeAcrossProcesses acquires a lock in a child process and
// verifies Probe reports live while the child runs and free after it exits.
func TestLivenessProbeAcrossProcesses(t *testing.T) {
	if os.Getenv("SAG_LIVENESS_HOLDER") == "1" {
		t.Skip("holder process")
	}

	dir := t.TempDir()
	sid := "proc-session-1"

	cmd := exec.Command(os.Args[0], "-test.run", "TestLivenessProbeAcrossProcesses")
	cmd.Env = append(os.Environ(),
		"SAG_LIVENESS_HOLDER=1",
		"SAG_LIVENESS_DIR="+dir,
		"SAG_LIVENESS_LOCK="+sid,
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start holder: %v", err)
	}
	// Ensure the child is reaped regardless of outcome.
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	// Wait for the child to signal it holds the lock.
	ready := filepath.Join(dir, sid+".ready")
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("holder did not acquire lock in time")
		}
		time.Sleep(10 * time.Millisecond)
	}

	state, info := Probe(dir, sid)
	if state != ProbeLive {
		t.Fatalf("Probe while holder alive = %v, want ProbeLive", state)
	}
	if info.SessionID != sid {
		t.Errorf("LivenessInfo.SessionID = %q, want %q", info.SessionID, sid)
	}
	if info.PID <= 0 {
		t.Errorf("LivenessInfo.PID = %d, want > 0", info.PID)
	}

	// Kill the holder and wait for the kernel to release the flock.
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill holder: %v", err)
	}
	_ = cmd.Wait()
	cmd.Process = nil // already reaped

	deadline = time.Now().Add(5 * time.Second)
	for {
		state, _ := Probe(dir, sid)
		if state == ProbeFree {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Probe after holder killed = %v, want ProbeFree", state)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestLivenessAcquireAndProbeInProcess verifies single-process semantics:
// while this process holds a lock, a probe from the same process reports live
// (flock is per-open-file-description on Linux, so a second open sees it held),
// and after release the probe reports free.
func TestLivenessAcquireAndProbeInProcess(t *testing.T) {
	dir := t.TempDir()
	sid := "self-session"

	release := Acquire(dir, LivenessInfo{SessionID: sid})
	if release == nil {
		t.Fatal("Acquire returned nil release func")
	}

	state, _ := Probe(dir, sid)
	if state != ProbeLive {
		t.Errorf("Probe while held = %v, want ProbeLive", state)
	}

	release()
	state, _ = Probe(dir, sid)
	if state != ProbeFree {
		t.Errorf("Probe after release = %v, want ProbeFree", state)
	}
}

// TestLivenessFailTowardLive verifies that an indeterminate probe (bad dir /
// empty id) reports unknown, which callers must treat as assume-live.
func TestLivenessFailTowardLive(t *testing.T) {
	if state, _ := Probe("", "x"); state != ProbeUnknown {
		t.Errorf("Probe with empty dir = %v, want ProbeUnknown", state)
	}
	if state, _ := Probe(t.TempDir(), ""); state != ProbeUnknown {
		t.Errorf("Probe with empty session id = %v, want ProbeUnknown", state)
	}
	if release := Acquire("", LivenessInfo{SessionID: "x"}); release != nil {
		t.Error("Acquire with empty dir should return nil release")
	}
}

// TestLivenessSanitizeKey verifies session ids become safe filenames.
func TestLivenessSanitizeKey(t *testing.T) {
	cases := map[string]string{
		"sagittarius-4242": "sagittarius-4242",
		"a/b/c":            "a_b_c",
		"id with spaces":   "id_with_spaces",
		"uuid-dash_ok-123": "uuid-dash_ok-123",
		"":                 "session",
	}
	for in, want := range cases {
		if got := sanitizeLivenessKey(in); got != want {
			t.Errorf("sanitizeLivenessKey(%q) = %q, want %q", in, got, want)
		}
	}
}
