package tools

import (
	"context"
	"fmt"
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
}

func newShellTool(ws *Workspace, bgMgr *bgproc.Manager) Tool {
	return &shellTool{ws: ws, autoBackgroundAfter: defaultAutoBackgroundAfter, bgMgr: bgMgr}
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
		return nil, fmt.Errorf("command blocked by safety policy: %s", command)
	}

	background, _, err := boolArg(args, ShellParamIsBackground)
	if err != nil {
		return nil, err
	}

	grace := t.autoBackgroundAfter
	if background {
		grace = backgroundStartGrace
	}
	return t.run(ctx, command, background, grace, sink)
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
func (t *shellTool) run(ctx context.Context, command string, explicitBackground bool, grace time.Duration, sink ToolOutputSink) (map[string]any, error) {
	logFile, err := os.CreateTemp("", "sagittarius-shell-*.log")
	if err != nil {
		return nil, fmt.Errorf("shell: create log file: %w", err)
	}
	logPath := logFile.Name()

	jobsFile, err := os.CreateTemp("", "sagittarius-jobs-*.pid")
	if err != nil {
		return nil, fmt.Errorf("shell: create jobs file: %w", err)
	}
	jobsPath := jobsFile.Name()
	_ = jobsFile.Close()
	defer os.Remove(jobsPath)

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

	pid := cmd.Process.Pid
	term := vt.NewEmulator(80, 24)
	term.SetScrollbackSize(100)
	var isDone atomic.Bool

	ioDone := make(chan struct{})
	// Copy output from PTY to logFile and emulator
	go func() {
		defer close(ioDone)
		buf := make([]byte, 1024)
		for {
			n, err := f.Read(buf)
			if n > 0 {
				_, _ = logFile.Write(buf[:n])
				if !isDone.Load() {
					_, _ = term.Write(buf[:n])
				}
			}
			if err != nil {
				break
			}
		}
		_ = logFile.Close()
	}()

	waitErr := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		// Give the PTY read loop a brief moment to flush OS buffers before
		// we close the master and potentially drop unread data.
		time.Sleep(50 * time.Millisecond)
		_ = f.Close() // Close PTY after wait to unblock Read
		waitErr <- err
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

		return t.completedResult(logPath, err)
	case <-ctx.Done():
		isDone.Store(true)
		if tailCancel != nil {
			tailCancel()
		}
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		_ = os.Remove(logPath)
		return nil, ctx.Err()
	case <-time.After(grace):
		isDone.Store(true)
		if tailCancel != nil {
			tailCancel()
		}
		if t.bgMgr != nil {
			t.bgMgr.Register(pid, pid, command, logPath)
		}
		return backgroundedResult(pid, logPath, explicitBackground, grace), nil
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
func (t *shellTool) completedResult(logPath string, waitErr error) (map[string]any, error) {
	output := readLogSnapshot(logPath)
	_ = os.Remove(logPath)
	if output == "" {
		output = "(empty)"
	}
	result := map[string]any{
		"output":     output,
		"background": false,
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
func backgroundedResult(pid int, logPath string, explicit bool, after time.Duration) map[string]any {
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
	if startup := readLogSnapshot(logPath); startup != "" {
		msg += "\nOutput so far:\n" + startup
	}
	return map[string]any{
		"output":     msg,
		"background": true,
		"pid":        pid,
		"log_file":   logPath,
	}
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
