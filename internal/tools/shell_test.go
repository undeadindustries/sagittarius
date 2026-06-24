package tools

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

func newTestShellTool(t *testing.T) *shellTool {
	t.Helper()
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &shellTool{ws: ws, autoBackgroundAfter: defaultAutoBackgroundAfter}
}

// processAlive reports whether pid is a live process (signal 0 probes existence
// without actually delivering a signal).
func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// killGroup terminates a background process group started by a test so no
// orphaned children survive the run.
func killGroup(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}

func TestShellForegroundCapturesOutput(t *testing.T) {
	t.Parallel()
	tool := newTestShellTool(t)

	res, err := tool.Execute(context.Background(), map[string]any{
		ShellParamCommand: "echo hello world",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := res["output"]; got != "hello world" {
		t.Fatalf("output = %q, want %q", got, "hello world")
	}
	if _, ok := res["exit_code"]; ok {
		t.Fatalf("exit_code should be absent on success, got %v", res["exit_code"])
	}
}

func TestShellForegroundNonZeroExit(t *testing.T) {
	t.Parallel()
	tool := newTestShellTool(t)

	res, err := tool.Execute(context.Background(), map[string]any{
		ShellParamCommand: "echo oops >&2; exit 7",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res["exit_code"] != 7 {
		t.Fatalf("exit_code = %v, want 7", res["exit_code"])
	}
	if !strings.Contains(res["output"].(string), "oops") {
		t.Fatalf("output = %q, want it to contain stderr 'oops'", res["output"])
	}
}

// TestShellAmpersandBackgroundDoesNotHang is the regression test for the
// WaitDelay bug: a `cmd &` shell must return promptly once the shell itself
// exits, even though the backgrounded child keeps the inherited pipe open.
// Before the fix this blocked for WaitDelay (5s) and then errored.
func TestShellAmpersandBackgroundDoesNotHang(t *testing.T) {
	t.Parallel()
	tool := newTestShellTool(t)

	// Capture the child PID so we can clean it up; the shell backgrounds a
	// 30s sleep and exits immediately.
	start := time.Now()
	res, err := tool.Execute(context.Background(), map[string]any{
		ShellParamCommand: "sleep 30 & echo $!",
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("foreground-with-& took %s, want prompt return (<3s)", elapsed)
	}
	// Clean up the orphaned sleep.
	if pidStr := strings.TrimSpace(res["output"].(string)); pidStr != "" {
		if pid := atoiSafe(pidStr); pid > 0 {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
}

func TestShellBackgroundReturnsImmediately(t *testing.T) {
	t.Parallel()
	tool := newTestShellTool(t)

	start := time.Now()
	res, err := tool.Execute(context.Background(), map[string]any{
		ShellParamCommand:      "echo serving; sleep 30",
		ShellParamIsBackground: true,
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Should return after roughly the grace window, well under the 30s sleep.
	if elapsed > 3*time.Second {
		t.Fatalf("background start took %s, want ~grace (<3s)", elapsed)
	}
	if res["background"] != true {
		t.Fatalf("background = %v, want true", res["background"])
	}
	pid, ok := res["pid"].(int)
	if !ok || pid <= 0 {
		t.Fatalf("pid = %v, want a positive int", res["pid"])
	}
	defer killGroup(pid)

	if !processAlive(pid) {
		t.Fatalf("pid %d not alive; background process should still be running", pid)
	}
	if !strings.Contains(res["output"].(string), "serving") {
		t.Fatalf("output = %q, want captured startup line 'serving'", res["output"])
	}
	logPath, ok := res["log_file"].(string)
	if !ok || logPath == "" {
		t.Fatalf("log_file = %v, want a path", res["log_file"])
	}
	if _, statErr := os.Stat(logPath); statErr != nil {
		t.Fatalf("log file %q not present: %v", logPath, statErr)
	}
}

// TestShellBackgroundImmediateFailure verifies that a command which exits within
// the grace window (e.g. a bind error) is reported as a completed failed run,
// not as a backgrounded process.
func TestShellBackgroundImmediateFailure(t *testing.T) {
	t.Parallel()
	tool := newTestShellTool(t)

	res, err := tool.Execute(context.Background(), map[string]any{
		ShellParamCommand:      "echo 'address already in use' >&2; exit 1",
		ShellParamIsBackground: true,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res["background"] != false {
		t.Fatalf("background = %v, want false for a command that exited", res["background"])
	}
	if res["exit_code"] != 1 {
		t.Fatalf("exit_code = %v, want 1", res["exit_code"])
	}
	if !strings.Contains(res["output"].(string), "address already in use") {
		t.Fatalf("output = %q, want the failure message", res["output"])
	}
}

func TestShellBackgroundBadArgType(t *testing.T) {
	t.Parallel()
	tool := newTestShellTool(t)

	_, err := tool.Execute(context.Background(), map[string]any{
		ShellParamCommand:      "echo hi",
		ShellParamIsBackground: "yes", // wrong type
	})
	if err == nil {
		t.Fatal("expected error for non-boolean is_background")
	}
}

// TestShellBackgroundCancelDuringGrace ensures a context canceled during the
// grace window kills the process group and returns the context error.
func TestShellBackgroundCancelDuringGrace(t *testing.T) {
	t.Parallel()
	tool := newTestShellTool(t)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	_, err := tool.Execute(ctx, map[string]any{
		ShellParamCommand:      "sleep 30",
		ShellParamIsBackground: true,
	})
	if err == nil {
		t.Fatal("expected context error when canceled during grace window")
	}
}

// TestShellForegroundAutoBackground is the key regression test for the
// "model forgot is_background, server hangs the turn" failure. A foreground
// command (NO is_background) that does not exit must be auto-backgrounded once
// it exceeds autoBackgroundAfter, so the tool always returns a result. The
// threshold is shortened here so the test runs fast.
func TestShellForegroundAutoBackground(t *testing.T) {
	t.Parallel()
	tool := newTestShellTool(t)
	tool.autoBackgroundAfter = 300 * time.Millisecond

	start := time.Now()
	res, err := tool.Execute(context.Background(), map[string]any{
		ShellParamCommand: "echo serving; sleep 30", // no is_background
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("foreground auto-background took %s, want ~threshold (<3s)", elapsed)
	}
	if res["background"] != true {
		t.Fatalf("background = %v, want true (foreground command should auto-background)", res["background"])
	}
	pid, ok := res["pid"].(int)
	if !ok || pid <= 0 {
		t.Fatalf("pid = %v, want a positive int", res["pid"])
	}
	defer killGroup(pid)
	if !processAlive(pid) {
		t.Fatalf("pid %d not alive; auto-backgrounded process should still run", pid)
	}
	out := res["output"].(string)
	if !strings.Contains(out, "moved to the background") {
		t.Fatalf("output = %q, want auto-background explanation", out)
	}
	if !strings.Contains(out, "serving") {
		t.Fatalf("output = %q, want captured startup line 'serving'", out)
	}
}

// TestShellForegroundCompletesUnderThreshold confirms that a normal foreground
// command finishing before the threshold returns synchronously with full output
// and background=false (no behavior change for ordinary commands).
func TestShellForegroundCompletesUnderThreshold(t *testing.T) {
	t.Parallel()
	tool := newTestShellTool(t)
	tool.autoBackgroundAfter = 5 * time.Second

	res, err := tool.Execute(context.Background(), map[string]any{
		ShellParamCommand: "echo quick",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res["background"] != false {
		t.Fatalf("background = %v, want false", res["background"])
	}
	if res["output"] != "quick" {
		t.Fatalf("output = %q, want %q", res["output"], "quick")
	}
}

// TestShellBackgroundHTTPServerReachable is the end-to-end analogue of the
// user's scenario: start `python3 -m http.server` in the background and confirm
// the tool returns immediately AND the server is actually listening. This is
// the exact case that previously hung the agent turn forever in the foreground.
func TestShellBackgroundHTTPServerReachable(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	tool := newTestShellTool(t)

	port := freePort(t)
	res, err := tool.Execute(context.Background(), map[string]any{
		ShellParamCommand:      fmt.Sprintf("python3 -m http.server %d", port),
		ShellParamIsBackground: true,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res["background"] != true {
		t.Fatalf("background = %v, want true (server should still be running)", res["background"])
	}
	pid := res["pid"].(int)
	defer killGroup(pid)

	// The server should be accepting connections shortly after start.
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	var connErr error
	for i := 0; i < 20; i++ {
		conn, dialErr := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			connErr = nil
			break
		}
		connErr = dialErr
		time.Sleep(100 * time.Millisecond)
	}
	if connErr != nil {
		t.Fatalf("server not reachable on %s: %v", addr, connErr)
	}
}

// freePort asks the OS for an ephemeral port, then releases it so the command
// under test can bind it. A brief race exists but is acceptable for a test.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

// atoiSafe parses a positive integer, returning 0 on any error.
func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}
