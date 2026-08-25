package tools

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/undeadindustries/sagittarius/internal/bgproc"
)

func newTestShellTool(t *testing.T) *shellTool {
	t.Helper()
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &shellTool{ws: ws, autoBackgroundAfter: defaultAutoBackgroundAfter, bgMgr: nil}
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
	// disown detaches the background sleep from the shell so bash -c exits
	// immediately after echo; without it bash may wait for the job and the
	// tool would not return until autoBackgroundAfter (30s).
	start := time.Now()
	res, err := tool.Execute(context.Background(), map[string]any{
		ShellParamCommand: "sleep 30 & disown; echo $!",
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

func TestShellBackgroundLogIsCapped(t *testing.T) {
	t.Parallel()
	tool := newTestShellTool(t)
	tool.maxLogBytes = 4096
	tool.autoBackgroundAfter = 200 * time.Millisecond

	res, err := tool.Execute(context.Background(), map[string]any{
		ShellParamCommand: "yes",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res["background"] != true {
		t.Fatalf("background = %v, want true (yes never exits)", res["background"])
	}
	pid, ok := res["pid"].(int)
	if !ok || pid <= 0 {
		t.Fatalf("pid = %v", res["pid"])
	}
	defer killGroup(pid)
	logPath, _ := res["log_file"].(string)
	if logPath == "" {
		t.Fatal("missing log_file")
	}
	deadline := time.Now().Add(2 * time.Second)
	var maxSize int64
	for {
		info, statErr := os.Stat(logPath)
		if statErr != nil {
			t.Fatalf("stat log: %v", statErr)
		}
		if info.Size() > maxSize {
			maxSize = info.Size()
		}
		if info.Size() > 4096 {
			t.Fatalf("log grew to %d bytes, want <= 4096", info.Size())
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	var data []byte
	var readErr error
	for i := 0; i < 10; i++ {
		data, readErr = os.ReadFile(logPath)
		if readErr != nil {
			t.Fatalf("read log: %v", readErr)
		}
		if len(data) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if maxSize == 0 && len(data) == 0 {
		t.Fatal("log stayed empty; yes should have produced output")
	}
	if int64(len(data)) > 4096 {
		t.Fatalf("log contents %d bytes, want <= 4096", len(data))
	}
	if !strings.Contains(string(data), "log capped") && !strings.Contains(string(data), "[capped]") && !strings.Contains(string(data), "y") {
		t.Fatalf("capped log missing marker or yes output: %q", clipBytes(data, 200))
	}
}

func TestShellDrainGoroutineExits(t *testing.T) {
	tool := newTestShellTool(t)
	baseline := runtime.NumGoroutine()
	for i := 0; i < 5; i++ {
		if _, err := tool.Execute(context.Background(), map[string]any{
			ShellParamCommand: "echo hello",
		}); err != nil {
			t.Fatalf("Execute: %v", err)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > baseline+4 {
		if time.Now().After(deadline) {
			t.Fatalf("drain goroutine leaked: delta %d", runtime.NumGoroutine()-baseline)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestShellBackgroundCapturesAmpersandChildren(t *testing.T) {
	t.Parallel()
	mgr := bgproc.NewManager()
	t.Cleanup(func() { _ = mgr.Close() })
	tool := newTestShellTool(t)
	tool.bgMgr = mgr
	tool.autoBackgroundAfter = 200 * time.Millisecond

	res, err := tool.Execute(context.Background(), map[string]any{
		ShellParamCommand: "sleep 8 & sleep 1",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	bashPID, _ := res["pid"].(int)
	if bashPID <= 0 {
		t.Fatalf("pid = %v", res["pid"])
	}
	t.Cleanup(func() { killGroup(bashPID) })

	deadline := time.Now().Add(4 * time.Second)
	for {
		list := mgr.List()
		var childPID int
		for _, p := range list {
			if p.PID != bashPID && strings.Contains(p.Command, "& child") {
				childPID = p.PID
				break
			}
		}
		if childPID > 0 {
			t.Cleanup(func() { _ = syscall.Kill(childPID, syscall.SIGKILL) })
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("ampersand child never registered; processes=%v", list)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func clipBytes(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n]) + "..."
	}
	return string(b)
}

func TestShellLsblkDoesNotFillDisk(t *testing.T) {
	if _, err := exec.LookPath("lsblk"); err != nil {
		t.Skip("lsblk not available")
	}
	tool := newTestShellTool(t)
	tool.maxLogBytes = 8192
	tool.autoBackgroundAfter = 300 * time.Millisecond

	res, err := tool.Execute(context.Background(), map[string]any{
		ShellParamCommand: "lsblk -f",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res["background"] == true {
		pid, _ := res["pid"].(int)
		defer killGroup(pid)
		logPath, _ := res["log_file"].(string)
		if logPath == "" {
			t.Fatal("missing log_file")
		}
		time.Sleep(400 * time.Millisecond)
		info, statErr := os.Stat(logPath)
		if statErr != nil {
			t.Fatalf("stat log: %v", statErr)
		}
		if info.Size() > 8192 {
			t.Fatalf("lsblk -f log grew to %d bytes, want <= 8192", info.Size())
		}
		return
	}
	out, _ := res["output"].(string)
	if len(out) > 64*1024 {
		t.Fatalf("completed lsblk output is %d bytes; expected a small table", len(out))
	}
}

type failWriter struct{ err error }

func (w failWriter) Write([]byte) (int, error) { return 0, w.err }

func TestCompletedResultEmptyWriteErr(t *testing.T) {
	t.Parallel()
	tool := newTestShellTool(t)
	path := filepath.Join(t.TempDir(), "empty.log")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := tool.completedResult(path, nil, syscall.ENOSPC)
	if err != nil {
		t.Fatalf("completedResult: %v", err)
	}
	out, _ := res["output"].(string)
	if out == emptyShellOutput || strings.Contains(out, emptyShellOutput) {
		t.Fatalf("output = %q, must not be %s", out, emptyShellOutput)
	}
	if !strings.Contains(out, "disk") {
		t.Fatalf("output = %q, want disk-full wording", out)
	}
	errKey, _ := res["error"].(string)
	if errKey == "" {
		t.Fatal("want error key so the TUI card shows as failed")
	}
}

func TestCompletedResultKeepsSnapshotOnWriteErr(t *testing.T) {
	t.Parallel()
	tool := newTestShellTool(t)
	path := filepath.Join(t.TempDir(), "partial.log")
	if err := os.WriteFile(path, []byte("captured tail\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := tool.completedResult(path, nil, syscall.ENOSPC)
	if err != nil {
		t.Fatalf("completedResult: %v", err)
	}
	out, _ := res["output"].(string)
	if !strings.Contains(out, "captured tail") {
		t.Fatalf("output = %q, want captured snapshot kept", out)
	}
	if !strings.Contains(out, "disk") {
		t.Fatalf("output = %q, want a write-failure warning", out)
	}
	if _, ok := res["error"]; ok {
		t.Fatalf("error key = %v, want unset so the tail stays visible", res["error"])
	}
}

func TestShellLogWriteFailureIsNotEmpty(t *testing.T) {
	t.Parallel()
	tool := newTestShellTool(t)
	tool.wrapLog = func(io.Writer) io.Writer {
		return failWriter{err: syscall.ENOSPC}
	}
	res, err := tool.Execute(context.Background(), map[string]any{
		ShellParamCommand: "echo hello-from-pty",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out, _ := res["output"].(string)
	if out == emptyShellOutput {
		t.Fatal("Execute returned (empty) after a log write failure")
	}
	if strings.Contains(out, "hello-from-pty") && res["error"] == nil {
		t.Fatal("write was supposed to fail before any bytes landed")
	}
	if !strings.Contains(out, "disk") && !strings.Contains(strings.ToUpper(out), "ENOSPC") {
		t.Fatalf("output = %q, want a disk-full / write-failed message", out)
	}
	errKey, _ := res["error"].(string)
	if errKey == "" {
		t.Fatalf("error key missing; output = %q", out)
	}
}
