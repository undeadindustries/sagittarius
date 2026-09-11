package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/tools"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

const (
	defaultWaitInterval = 10 * time.Second
	minWaitInterval     = 2 * time.Second
	defaultWaitTimeout  = 300 * time.Second
	maxWaitTimeout      = 1800 * time.Second
	waitCheckOutputCap  = 4096
)

// waitUntilTool polls a read-only shell check until it exits 0 or the timeout
// elapses. It is registered on the parent runner only: a subagent has nobody
// to answer an unknown-command confirm, and stalling a child for minutes is
// not a capability we want to ship.
type waitUntilTool struct {
	workDir  string
	runCheck func(ctx context.Context, command string) (exit int, output string, err error)
	sleep    func(ctx context.Context, d time.Duration) error
}

func newWaitUntilTool(workDir string) *waitUntilTool {
	return &waitUntilTool{workDir: workDir}
}

func registerWaitUntilTool(r *Runner, registry *tools.Registry) {
	if r == nil || registry == nil || r.isSubagent() {
		return
	}
	registry.Register(newWaitUntilTool(r.workDir))
}

func (t *waitUntilTool) Name() string               { return tools.WaitUntilToolName }
func (t *waitUntilTool) RequiresConfirmation() bool { return false }

func (t *waitUntilTool) Declaration() provider.ToolDeclaration {
	return provider.ToolDeclaration{
		Name: tools.WaitUntilToolName,
		Description: "Poll a read-only shell check until it succeeds (exit 0) or the timeout elapses. " +
			"Use this instead of sleep after starting long-running work with run_shell_command is_background=true. " +
			"The check runs immediately, then on each interval. Mutating commands are rejected. " +
			"Cancel with Esc or /stop.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				tools.WaitUntilParamCommand: map[string]any{
					"type":        "string",
					"description": "Read-only shell check. Exit 0 means the condition is met (e.g. test -f /tmp/done, systemctl is-active nginx, curl -sf http://127.0.0.1:8080/health).",
				},
				tools.WaitUntilParamInterval: map[string]any{
					"type":        "integer",
					"description": "Seconds between checks (default 10, minimum 2).",
				},
				tools.WaitUntilParamTimeout: map[string]any{
					"type":        "integer",
					"description": "Maximum seconds to wait (default 300, maximum 1800).",
				},
				tools.WaitUntilParamDescription: map[string]any{
					"type":        "string",
					"description": "Short label shown on the tool card (what you are waiting for).",
				},
			},
			"required": []string{tools.WaitUntilParamCommand},
		},
	}
}

func (t *waitUntilTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	return t.ExecuteStream(ctx, args, nil)
}

func (t *waitUntilTool) ExecuteStream(ctx context.Context, args map[string]any, sink tools.ToolOutputSink) (map[string]any, error) {
	return t.executeStreamFor(ctx, args, sink, false, nil,
		minWaitInterval, defaultWaitInterval, defaultWaitTimeout, maxWaitTimeout)
}

func (t *waitUntilTool) ExecuteInteractive(
	ctx context.Context,
	args map[string]any,
	interactive bool,
	emit func(ui.StreamEvent),
) (map[string]any, error) {
	var sink tools.ToolOutputSink
	if emit != nil {
		sink = func(text string) {
			emit(ui.StreamEvent{Type: ui.StreamToolOutput, Text: text})
		}
	}
	return t.executeStreamFor(ctx, args, sink, interactive, emit,
		minWaitInterval, defaultWaitInterval, defaultWaitTimeout, maxWaitTimeout)
}

func (t *waitUntilTool) executeStreamFor(
	ctx context.Context,
	args map[string]any,
	sink tools.ToolOutputSink,
	interactive bool,
	emit func(ui.StreamEvent),
	minInterval, defaultInterval, defaultTimeout, maxTimeout time.Duration,
) (map[string]any, error) {
	command, err := stringArg(args, tools.WaitUntilParamCommand)
	if err != nil {
		return nil, &tools.ToolError{Code: tools.ErrCodeInvalidArgs, Message: err.Error()}
	}
	intervalSec, _, err := waitIntArg(args, tools.WaitUntilParamInterval)
	if err != nil {
		return nil, &tools.ToolError{Code: tools.ErrCodeInvalidArgs, Message: err.Error()}
	}
	timeoutSec, _, err := waitIntArg(args, tools.WaitUntilParamTimeout)
	if err != nil {
		return nil, &tools.ToolError{Code: tools.ErrCodeInvalidArgs, Message: err.Error()}
	}
	interval, timeout := clampWaitTimingFor(intervalSec, timeoutSec, minInterval, defaultInterval, defaultTimeout, maxTimeout)

	needsConfirm, admitErr := admitWaitCommand(command)
	if admitErr != nil {
		return nil, admitErr
	}
	if needsConfirm {
		if err := t.confirmUnknown(ctx, command, interval, timeout, interactive, emit); err != nil {
			return nil, err
		}
	}

	return t.pollFor(ctx, command, interval, timeout, sink)
}

func (t *waitUntilTool) confirmUnknown(
	ctx context.Context,
	command string,
	interval, timeout time.Duration,
	interactive bool,
	emit func(ui.StreamEvent),
) error {
	if !interactive || emit == nil {
		return &tools.ToolError{
			Code:    tools.ErrCodeInvalidArgs,
			Message: "wait_until: command is not recognized as read-only; refusing to poll it unattended",
		}
	}
	replyCh := make(chan ui.ConfirmDecision, 1)
	emit(ui.StreamEvent{
		Type:         ui.StreamToolConfirm,
		Text:         fmt.Sprintf("Poll %q every %s for up to %s?", command, interval, timeout),
		ConfirmReply: replyCh,
	})
	select {
	case <-ctx.Done():
		return ctx.Err()
	case decision := <-replyCh:
		if decision == ui.ConfirmDeny {
			return &tools.ToolError{Code: tools.ErrCodeUserDenied, Message: "user denied wait_until command"}
		}
		return nil
	}
}

func (t *waitUntilTool) pollFor(
	ctx context.Context,
	command string,
	interval, timeout time.Duration,
	sink tools.ToolOutputSink,
) (map[string]any, error) {
	deadline := time.Now().Add(timeout)
	started := time.Now()
	checks := 0
	var lastExit int
	var lastOutput string

	for {
		if err := ctx.Err(); err != nil {
			return waitResult("canceled", command, checks, time.Since(started), lastExit, lastOutput), err
		}
		exit, output, err := t.check(ctx, command)
		if err != nil && ctx.Err() != nil {
			return waitResult("canceled", command, checks, time.Since(started), lastExit, lastOutput), ctx.Err()
		}
		checks++
		lastExit = exit
		lastOutput = output
		elapsed := time.Since(started)
		t.emitStatus(sink, elapsed, timeout, checks, lastExit, err)

		if err == nil && exit == 0 {
			return waitResult("ready", command, checks, elapsed, 0, lastOutput), nil
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, waitTimeoutError(command, checks, elapsed, lastExit, lastOutput)
		}
		sleepFor := interval
		if sleepFor > remaining {
			sleepFor = remaining
		}
		if err := t.pause(ctx, sleepFor); err != nil {
			return waitResult("canceled", command, checks, time.Since(started), lastExit, lastOutput), err
		}
	}
}

func (t *waitUntilTool) check(ctx context.Context, command string) (int, string, error) {
	if t.runCheck != nil {
		return t.runCheck(ctx, command)
	}
	return runWaitCheck(ctx, t.workDir, command)
}

func (t *waitUntilTool) pause(ctx context.Context, d time.Duration) error {
	if t.sleep != nil {
		return t.sleep(ctx, d)
	}
	return sleepCtx(ctx, d)
}

func (t *waitUntilTool) emitStatus(sink tools.ToolOutputSink, elapsed, timeout time.Duration, checks, lastExit int, checkErr error) {
	if sink == nil {
		return
	}
	status := fmt.Sprintf("waiting %s / %s · %d checks · last exit %d",
		formatWaitDur(elapsed), formatWaitDur(timeout), checks, lastExit)
	if checkErr != nil {
		status += " · " + checkErr.Error()
	}
	sink(status)
}

func admitWaitCommand(command string) (needsConfirm bool, err error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return false, &tools.ToolError{Code: tools.ErrCodeInvalidArgs, Message: "wait_until: command is empty"}
	}
	if tools.IsDangerousCommand(command) {
		return false, &tools.ToolError{
			Code:    tools.ErrCodeInvalidArgs,
			Message: "wait_until: refusing to poll a dangerous command",
		}
	}
	verdict, reason := tools.ClassifyShellReadOnly(command)
	switch verdict {
	case tools.VerdictMutating:
		return false, &tools.ToolError{
			Code:    tools.ErrCodeInvalidArgs,
			Message: "wait_until: refusing to poll a mutating command: " + reason,
		}
	case tools.VerdictUnknown:
		return true, nil
	default:
		return false, nil
	}
}

func clampWaitTiming(intervalSec, timeoutSec int) (interval, timeout time.Duration) {
	return clampWaitTimingFor(intervalSec, timeoutSec, minWaitInterval, defaultWaitInterval, defaultWaitTimeout, maxWaitTimeout)
}

func clampWaitTimingFor(
	intervalSec, timeoutSec int,
	minInterval, defaultInterval, defaultTimeout, maxTimeout time.Duration,
) (interval, timeout time.Duration) {
	timeout = defaultTimeout
	if timeoutSec > 0 {
		timeout = time.Duration(timeoutSec) * time.Second
	}
	if timeout > maxTimeout {
		timeout = maxTimeout
	}
	if timeout < minInterval {
		timeout = minInterval
	}

	interval = defaultInterval
	if intervalSec > 0 {
		interval = time.Duration(intervalSec) * time.Second
	}
	if interval < minInterval {
		interval = minInterval
	}
	if interval > timeout {
		interval = timeout
	}
	return interval, timeout
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func runWaitCheck(ctx context.Context, workDir, command string) (int, string, error) {
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	if workDir != "" {
		cmd.Dir = workDir
	}
	cmd.Env = append(os.Environ(), tools.NestedAgentEnvVar+"=1")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	output := capRunes(buf.String(), waitCheckOutputCap)
	if err == nil {
		return 0, output, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), output, nil
	}
	return -1, output, err
}

func waitResult(status, command string, checks int, elapsed time.Duration, exit int, output string) map[string]any {
	return map[string]any{
		"status":          status,
		"command":         command,
		"checks":          checks,
		"elapsed":         formatWaitDur(elapsed),
		"elapsed_seconds": int(elapsed.Seconds()),
		"exit_code":       exit,
		"output":          output,
	}
}

func waitTimeoutError(command string, checks int, elapsed time.Duration, exit int, output string) error {
	msg := fmt.Sprintf("wait_until timed out after %s (%d checks, last exit %d)", formatWaitDur(elapsed), checks, exit)
	if strings.TrimSpace(output) != "" {
		msg += "\n" + output
	}
	return &tools.ToolError{
		Code:    tools.ErrCodeExecutionTimeout,
		Message: msg,
		Details: waitResult("timeout", command, checks, elapsed, exit, output),
	}
}

func formatWaitDur(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	min := int(d / time.Minute)
	sec := int((d % time.Minute) / time.Second)
	if min == 0 {
		return fmt.Sprintf("%ds", sec)
	}
	if sec == 0 {
		return fmt.Sprintf("%dm", min)
	}
	return fmt.Sprintf("%dm%ds", min, sec)
}

func capRunes(s string, max int) string {
	if max <= 0 || s == "" {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}

func waitIntArg(args map[string]any, key string) (int, bool, error) {
	raw, ok := args[key]
	if !ok {
		return 0, false, nil
	}
	switch v := raw.(type) {
	case int:
		return v, true, nil
	case int64:
		return int(v), true, nil
	case float64:
		return int(v), true, nil
	default:
		return 0, false, fmt.Errorf("parameter %q must be an integer", key)
	}
}
