package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"

	"github.com/undeadindustries/sagittarius/internal/bgproc"
	"github.com/undeadindustries/sagittarius/internal/provider"
)

// backgroundStartGrace is how long an explicit background command is observed
// after Start before the tool returns. Long enough to surface immediate
// failures (e.g. "address already in use", syntax errors) and capture a startup
// banner, short enough that the agent turn is not blocked.
const backgroundStartGrace = 750 * time.Millisecond

// defaultAutoBackgroundAfter is how long a FOREGROUND command may run before it
// is automatically moved to the background. This is the safety net that
// guarantees the agent turn always receives a result: a server invoked without
// is_background (e.g. `python3 -m http.server`) would otherwise block forever in
// cmd.Wait(). Commands that finish under this threshold return their full output
// synchronously as before; only genuinely long-lived processes are backgrounded.
const defaultAutoBackgroundAfter = 30 * time.Second

type shellTool struct {
	ws *Workspace
	// autoBackgroundAfter is the foreground auto-background threshold. A field
	// (not a const) so tests can shorten it; production uses the default.
	autoBackgroundAfter time.Duration
	bgMgr               *bgproc.Manager
	spillDir            string
	// maxLogBytes caps the per-command log file. Zero means maxShellLogBytes.
	// Tests set a smaller value to assert rotation without writing megabytes.
	maxLogBytes int64
	// wrapLog, if set, wraps the PTY log writer. Tests inject a failing
	// writer; production leaves it nil.
	wrapLog func(io.Writer) io.Writer
}

func newShellTool(ws *Workspace, bgMgr *bgproc.Manager, spillDir string) Tool {
	return &shellTool{ws: ws, autoBackgroundAfter: defaultAutoBackgroundAfter, bgMgr: bgMgr, spillDir: spillDir}
}

func (t *shellTool) Name() string { return ShellToolName }

func (t *shellTool) RequiresConfirmation() bool { return true }

func (t *shellTool) Declaration() provider.ToolDeclaration {
	return provider.ToolDeclaration{
		Name: ShellToolName,
		Description: "Executes a given shell command as `bash -c <command>`. " +
			"Command is executed as a subprocess that leads its own process group. " +
			"For long-running processes that do not exit on their own (e.g. dev " +
			"servers like `python3 -m http.server`, `npm run dev`, `node server.js`), " +
			"set is_background=true: the command is started, observed briefly to catch " +
			"immediate failures, then left running while the tool returns its PID and a " +
			"log-file path. Do NOT append `&` yourself — use is_background instead. A " +
			"foreground command that runs too long is automatically moved to the " +
			"background so the turn is never blocked indefinitely.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				ShellParamCommand: map[string]any{
					"type":        "string",
					"description": "Exact bash command to execute as `bash -c <command>`",
				},
				ShellParamIsBackground: map[string]any{
					"type": "boolean",
					"description": "Run the command in the background and return immediately. " +
						"Use for servers and other processes that never exit on their own. " +
						"Output is streamed to a log file whose path is returned.",
				},
			},
			"required": []string{ShellParamCommand},
		},
	}
}

func (t *shellTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	return t.ExecuteStream(ctx, args, nil)
}

func (t *shellTool) ExecuteStream(ctx context.Context, args map[string]any, sink ToolOutputSink) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	command, err := stringArg(args, ShellParamCommand)
	if err != nil {
		return nil, err
	}
	if IsDangerousCommand(command) {
		return nil, &ToolError{
			Code:    ErrCodeSandboxDenial,
			Message: fmt.Sprintf("command blocked by safety policy: %s", command),
		}
	}

	background, _, err := boolArg(args, ShellParamIsBackground)
	if err != nil {
		return nil, err
	}

	grace := t.autoBackgroundAfter
	if background {
		grace = backgroundStartGrace
	}
	result, err := t.run(ctx, command, background, grace, sink, nil)
	if err != nil && errors.Is(err, context.DeadlineExceeded) {
		return nil, &ToolError{Code: ErrCodeExecutionTimeout, Message: "command timed out"}
	}
	return result, err
}

// userShellWaitForever disables auto-background so a user `!` command
// stays in the foreground until it exits or the context is canceled.
const userShellWaitForever time.Duration = 0

// ExecuteUser runs command in the PTY without auto-background, optionally
// accepting keystrokes on stdin. It is the user-`!` path, not a model tool call.
func (t *shellTool) ExecuteUser(ctx context.Context, command string, sink ToolOutputSink, stdin *PTYStdin) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if IsDangerousCommand(command) {
		return nil, &ToolError{
			Code:    ErrCodeSandboxDenial,
			Message: fmt.Sprintf("command blocked by safety policy: %s", command),
		}
	}
	return t.run(ctx, command, false, userShellWaitForever, sink, stdin)
}

// run starts command with stdout+stderr redirected to a temp log file, then
// waits for one of three outcomes:
//
//   - the process exits within grace -> return its output + exit code (the
//     normal synchronous case; the log file is removed);
//   - ctx is canceled -> kill the process group and return ctx.Err();
//   - grace elapses while the process is still running -> leave it running and
//     return its PID, log-file path, and any startup output captured so far.
//
// A log file (not a pipe) is used so the child can keep writing after the tool
// returns without risking SIGPIPE on the writer or leaking a copy goroutine.
// The process is started under context.Background, not ctx, so a backgrounded
// process outlives the agent turn; cancellation is handled explicitly below.
func (t *shellTool) run(ctx context.Context, command string, explicitBackground bool, grace time.Duration, sink ToolOutputSink, stdin *PTYStdin) (map[string]any, error) {
	logFile, err := os.CreateTemp("", shellLogPattern)
	if err != nil {
		return nil, fmt.Errorf("shell: create log file: %w", err)
	}
	logPath := logFile.Name()

	jobsFile, err := os.CreateTemp("", jobsPidPattern)
	if err != nil {
		_ = logFile.Close()
		_ = os.Remove(logPath)
		return nil, fmt.Errorf("shell: create jobs file: %w", err)
	}
	jobsPath := jobsFile.Name()
	_ = jobsFile.Close()
	var backgrounded atomic.Bool
	defer func() {
		if !backgrounded.Load() {
			_ = os.Remove(jobsPath)
		}
	}()

	wrappedCommand := fmt.Sprintf(`trap 'jobs -p > %q' EXIT; %s`, jobsPath, command)

	cmd := exec.Command("bash", "-c", wrappedCommand)
	cmd.Dir = t.ws.Root()
	cmd.Env = os.Environ()

	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		_ = logFile.Close()
		_ = os.Remove(logPath)
		return nil, fmt.Errorf("shell pty failed: %w", err)
	}

	stdin.attach(f)
	defer stdin.detach()

	pid := cmd.Process.Pid
	term := vt.NewEmulator(80, 24)
	term.SetScrollbackSize(100)
	var isDone atomic.Bool

	ioDone := make(chan struct{})
	var logWriteErr atomic.Value
	// Copy output from PTY to the capped log and emulator. The loop keeps
	// draining after a write error so a full PTY buffer cannot freeze the
	// child; it stops on a spinning zero-byte read, on Read error (including
	// the master being closed after Wait), or after maxZeroReads.
	go func() {
		defer close(ioDone)
		defer func() { _ = logFile.Close() }()
		buf := make([]byte, ptyReadBufSize)
		writer := t.newLogWriter(logFile)
		var zeroReads int
		var writeFailed bool
		for {
			n, err := f.Read(buf)
			if n == 0 && err == nil {
				zeroReads++
				if zeroReads >= maxZeroReads {
					slog.Warn("shell: PTY read spun on zero-byte reads; stopping drain", "path", logPath)
					break
				}
				continue
			}
			zeroReads = 0
			if n > 0 {
				if !writeFailed {
					if _, werr := writer.Write(buf[:n]); werr != nil {
						slog.Warn("shell: log write failed; discarding further output", "path", logPath, "err", werr)
						writeFailed = true
						logWriteErr.Store(werr)
					}
				}
				if !isDone.Load() {
					_, _ = term.Write(buf[:n])
				}
			}
			if err != nil {
				break
			}
		}
	}()

	waitErr := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		// Give the PTY read loop a brief moment to flush OS buffers before
		// we close the master and potentially drop unread data.
		time.Sleep(50 * time.Millisecond)
		_ = f.Close() // Close PTY after wait to unblock Read
		waitErr <- err
		if backgrounded.Load() {
			t.captureJobs(jobsPath, command, logPath)
			_ = os.Remove(jobsPath)
		}
	}()

	var tailCancel context.CancelFunc
	if sink != nil {
		var tailCtx context.Context
		tailCtx, tailCancel = context.WithCancel(ctx)
		go func() {
			ticker := time.NewTicker(250 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-tailCtx.Done():
					return
				case <-ticker.C:
					sink(renderEmulator(term))
				}
			}
		}()
	}

	if grace <= 0 {
		select {
		case err = <-waitErr:
			<-ioDone
			isDone.Store(true)
			if tailCancel != nil {
				tailCancel()
			}
			if sink != nil {
				sink(renderEmulator(term))
			}
			t.captureJobs(jobsPath, command, logPath)
			return t.completedResult(logPath, err, storedErr(&logWriteErr))
		case <-ctx.Done():
			isDone.Store(true)
			if tailCancel != nil {
				tailCancel()
			}
			_ = syscall.Kill(-pid, syscall.SIGKILL)
			_ = f.Close()
			<-ioDone
			_ = os.Remove(logPath)
			return nil, ctx.Err()
		}
	}

	timer := time.NewTimer(grace)
	defer timer.Stop()

	select {
	case err = <-waitErr:
		<-ioDone // wait for remaining output to flush
		isDone.Store(true)
		if tailCancel != nil {
			tailCancel()
		}
		if sink != nil {
			sink(renderEmulator(term))
		}

		// Capture background jobs started by '&'
		t.captureJobs(jobsPath, command, logPath)

		return t.completedResult(logPath, err, storedErr(&logWriteErr))
	case <-ctx.Done():
		isDone.Store(true)
		if tailCancel != nil {
			tailCancel()
		}
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		_ = f.Close()
		<-ioDone
		_ = os.Remove(logPath)
		return nil, ctx.Err()
	case <-timer.C:
		// Prefer a concurrent exit over backgrounding when both are ready.
		select {
		case err = <-waitErr:
			<-ioDone
			isDone.Store(true)
			if tailCancel != nil {
				tailCancel()
			}
			if sink != nil {
				sink(renderEmulator(term))
			}
			t.captureJobs(jobsPath, command, logPath)
			return t.completedResult(logPath, err, storedErr(&logWriteErr))
		default:
			backgrounded.Store(true)
			isDone.Store(true)
			if tailCancel != nil {
				tailCancel()
			}
			if t.bgMgr != nil {
				t.bgMgr.Register(pid, pid, command, logPath)
			}
			return backgroundedResult(pid, logPath, explicitBackground, grace, storedErr(&logWriteErr)), nil
		}
	}
}

func (t *shellTool) captureJobs(jobsPath, command, logPath string) {
	if t.bgMgr == nil {
		return
	}
	data, err := os.ReadFile(jobsPath)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var pid int
		if _, err := fmt.Sscanf(line, "%d", &pid); err == nil && pid > 0 {
			t.bgMgr.Register(pid, 0, command+" (& child)", logPath)
		}
	}
}

// completedResult builds the tool result for a command that ran to completion,
// reading its captured output from the log file and mapping any non-zero exit.
// writeErr is the first PTY-log Write failure (nil if the drain wrote cleanly).
func (t *shellTool) completedResult(logPath string, waitErr, writeErr error) (map[string]any, error) {
	snapshot := readLogSnapshot(logPath)
	_ = os.Remove(logPath)
	output, errKey := applyLogWriteStatus(snapshot, writeErr)
	spill := maybeSpillOutput(output, t.spillDir)
	result := map[string]any{
		"output":     spill.output,
		"background": false,
	}
	applySpillMeta(result, spill)
	if errKey != "" {
		result["error"] = errKey
	}
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			result["exit_code"] = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("shell execution failed: %w", waitErr)
		}
	}
	return result, nil
}

// backgroundedResult builds the tool result for a still-running process,
// distinguishing an explicitly-requested background start from a foreground
// command that was auto-backgrounded because it exceeded the threshold.
func backgroundedResult(pid int, logPath string, explicit bool, after time.Duration, writeErr error) map[string]any {
	var msg string
	if explicit {
		msg = fmt.Sprintf("Started in background (pid %d). Output is being written to %s.", pid, logPath)
	} else {
		msg = fmt.Sprintf(
			"Command still running after %s and was moved to the background (pid %d) so the turn is not blocked. "+
				"It is still running; read %s to check its output or progress.",
			after, pid, logPath,
		)
	}
	startup := readLogSnapshot(logPath)
	if startup != "" {
		// The live log_file already holds the full stream; cap only what we
		// echo back so a huge startup banner does not bloat the next turn.
		head, tail, omitted := splitHeadTail(startup, maxModelOutputBytes)
		if omitted == 0 {
			msg += "\nOutput so far:\n" + startup
		} else {
			marker := backgroundOmittedMarker(omitted, countOutputLines(startup), logPath)
			msg += "\nOutput so far:\n" + assemble(head, tail, marker)
		}
	}
	if writeErr != nil {
		msg += "\n" + logWriteErrorMessage(writeErr)
	}
	result := map[string]any{
		"output":     msg,
		"background": true,
		"pid":        pid,
		"log_file":   logPath,
	}
	if writeErr != nil && startup == "" {
		result["error"] = logWriteErrorMessage(writeErr)
	}
	return result
}

// readLogSnapshot reads the current contents of a command's log file, returning
// trimmed text. Errors yield an empty string (best-effort snapshot).
func readLogSnapshot(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(ansi.Strip(string(data)))
}

func storedErr(v *atomic.Value) error {
	if v == nil {
		return nil
	}
	e, _ := v.Load().(error)
	return e
}
