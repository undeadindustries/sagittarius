package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

const (
	DefaultHookTimeout = 60 * time.Second
	MaxHookOutputBytes = 1024 * 1024 // 1MB stdout/stderr cap
	MaxParallelHooks   = 8
)

// Runner executes command hooks.
type Runner struct {
	// PlansDir is optional path to plans directory for $GEMINI_PLANS_DIR
	PlansDir string
}

// NewRunner creates a new hook runner.
func NewRunner(plansDir string) *Runner {
	return &Runner{PlansDir: plansDir}
}

// ExecuteHook executes a single hook configuration.
func (r *Runner) ExecuteHook(ctx context.Context, config HookConfig, event HookEventName, input HookInput) ExecutionResult {
	start := time.Now()

	if config.Type == TypeRuntime {
		return ExecutionResult{
			HookConfig: config,
			EventName:  event,
			Success:    false,
			Error:      fmt.Errorf("runtime hooks not supported"),
			Duration:   time.Since(start),
		}
	}

	if strings.TrimSpace(config.Command) == "" {
		return ExecutionResult{
			HookConfig: config,
			EventName:  event,
			Success:    false,
			Error:      fmt.Errorf("command hook missing command field"),
			Duration:   time.Since(start),
		}
	}

	timeout := DefaultHookTimeout
	if config.Timeout > 0 {
		timeout = time.Duration(config.Timeout) * time.Second
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmdStr := expandCommand(config.Command, input, r.PlansDir)
	cmd := exec.CommandContext(execCtx, "sh", "-c", cmdStr)
	if input.CWD != "" {
		cmd.Dir = input.CWD
	}

	// Environment setup
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env,
		"GEMINI_PROJECT_DIR="+input.CWD,
		"GEMINI_CWD="+input.CWD,
		"GEMINI_PLANS_DIR="+r.PlansDir,
		"GEMINI_SESSION_ID="+input.SessionID,
		"CLAUDE_PROJECT_DIR="+input.CWD,
		"SAGITTARIUS_PROJECT_DIR="+input.CWD,
		"SAGITTARIUS_CWD="+input.CWD,
		"SAGITTARIUS_SESSION_ID="+input.SessionID,
	)
	for k, v := range config.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	// Process group & cancellation behavior
	setSysProcAttr(cmd)
	cmd.WaitDelay = 5 * time.Second

	// Pipes
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return ExecutionResult{
			HookConfig: config,
			EventName:  event,
			Success:    false,
			Error:      fmt.Errorf("stdin pipe error: %w", err),
			Duration:   time.Since(start),
		}
	}

	var stdoutBuf, stderrBuf limitedBuffer
	stdoutBuf.cap = MaxHookOutputBytes
	stderrBuf.cap = MaxHookOutputBytes
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return ExecutionResult{
			HookConfig: config,
			EventName:  event,
			Success:    false,
			Error:      fmt.Errorf("start command error: %w", err),
			Duration:   time.Since(start),
		}
	}

	// Write JSON input to stdin
	go func() {
		defer func() { _ = stdinPipe.Close() }()
		_ = json.NewEncoder(stdinPipe).Encode(input)
	}()

	waitErr := cmd.Wait()
	duration := time.Since(start)

	if execCtx.Err() == context.DeadlineExceeded {
		return ExecutionResult{
			HookConfig: config,
			EventName:  event,
			Success:    false,
			Error:      fmt.Errorf("hook timed out after %v", timeout),
			Stdout:     stdoutBuf.String(),
			Stderr:     stderrBuf.String(),
			ExitCode:   -1,
			Duration:   duration,
		}
	}

	exitCode := ExitCodeSuccess
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return ExecutionResult{
				HookConfig: config,
				EventName:  event,
				Success:    false,
				Error:      waitErr,
				Stdout:     stdoutBuf.String(),
				Stderr:     stderrBuf.String(),
				ExitCode:   -1,
				Duration:   duration,
			}
		}
	}

	stdoutStr := stdoutBuf.String()
	stderrStr := stderrBuf.String()

	output, outputFormat := parseHookOutput(stdoutStr, stderrStr, exitCode)

	return ExecutionResult{
		HookConfig:   config,
		EventName:    event,
		Success:      exitCode == ExitCodeSuccess,
		Output:       output,
		OutputFormat: outputFormat,
		Stdout:       stdoutStr,
		Stderr:       stderrStr,
		ExitCode:     exitCode,
		Duration:     duration,
	}
}

// ExecuteHooksParallel executes multiple hooks concurrently with bounded concurrency.
func (r *Runner) ExecuteHooksParallel(ctx context.Context, configs []HookConfig, event HookEventName, input HookInput) []ExecutionResult {
	results := make([]ExecutionResult, len(configs))
	var g errgroup.Group
	g.SetLimit(MaxParallelHooks)

	for i, cfg := range configs {
		i, cfg := i, cfg
		g.Go(func() error {
			results[i] = r.ExecuteHook(ctx, cfg, event, input)
			return nil
		})
	}

	_ = g.Wait()
	return results
}

// ExecuteHooksSequential executes multiple hooks sequentially, threading output into input.
// A blocking decision (or an explicit continue:false) ends the group: later hooks in a
// sequential chain are written against the earlier ones' output, so running them after a
// block would feed them input for an action that is no longer going to happen.
func (r *Runner) ExecuteHooksSequential(ctx context.Context, configs []HookConfig, event HookEventName, input HookInput) []ExecutionResult {
	results := make([]ExecutionResult, 0, len(configs))
	currInput := input

	for _, cfg := range configs {
		res := r.ExecuteHook(ctx, cfg, event, currInput)
		results = append(results, res)

		if res.Output.IsBlocking() || res.Output.ShouldStop() {
			break
		}
		if res.Success && res.Output != nil {
			currInput = applyHookOutputToInput(currInput, res.Output, event)
		}
	}

	return results
}

// expandCommand replaces environment variable placeholders in command string.
func expandCommand(cmd string, input HookInput, plansDir string) string {
	cmd = strings.ReplaceAll(cmd, "$GEMINI_PROJECT_DIR", input.CWD)
	cmd = strings.ReplaceAll(cmd, "$GEMINI_CWD", input.CWD)
	cmd = strings.ReplaceAll(cmd, "$GEMINI_PLANS_DIR", plansDir)
	cmd = strings.ReplaceAll(cmd, "$GEMINI_SESSION_ID", input.SessionID)
	cmd = strings.ReplaceAll(cmd, "$CLAUDE_PROJECT_DIR", input.CWD)
	cmd = strings.ReplaceAll(cmd, "$SAGITTARIUS_PROJECT_DIR", input.CWD)
	cmd = strings.ReplaceAll(cmd, "$SAGITTARIUS_CWD", input.CWD)
	cmd = strings.ReplaceAll(cmd, "$SAGITTARIUS_SESSION_ID", input.SessionID)
	return cmd
}

// parseHookOutput attempts JSON unmarshaling, degrading to text output on non-JSON.
func parseHookOutput(stdout, stderr string, exitCode int) (*HookOutput, string) {
	textToParse := strings.TrimSpace(stdout)
	if textToParse == "" {
		textToParse = strings.TrimSpace(stderr)
	}

	if textToParse != "" {
		var output HookOutput
		if err := json.Unmarshal([]byte(textToParse), &output); err == nil {
			// Exit code 2 is a hard block and outranks whatever the hook printed:
			// a hook that exits 2 while emitting {"decision":"allow"} still blocks.
			if exitCode == ExitCodeSystemBlock && !output.IsBlocking() {
				output.Decision = DecisionBlock
				if output.Reason == "" {
					output.Reason = blockReason(stderr, stdout)
				}
			}
			return &output, "json"
		}
	}

	// Plain text degradation
	out := convertPlainTextToHookOutput(textToParse, exitCode)
	return out, "text"
}

// blockReason picks the most useful denial reason for an exit-code-2 block.
// stderr is the documented channel for the reason; stdout is a fallback for a
// hook that logged there instead.
func blockReason(stderr, stdout string) string {
	if r := strings.TrimSpace(stderr); r != "" {
		return r
	}
	if r := strings.TrimSpace(stdout); r != "" {
		return r
	}
	return "hook exited with code 2"
}

// convertPlainTextToHookOutput creates structured output from non-JSON stdout/stderr.
func convertPlainTextToHookOutput(text string, exitCode int) *HookOutput {
	if exitCode == ExitCodeSuccess {
		return &HookOutput{
			Decision:      DecisionAllow,
			SystemMessage: text,
		}
	}
	if exitCode == ExitCodeNonBlockingError {
		msg := text
		if msg != "" {
			msg = "Warning: " + msg
		}
		return &HookOutput{
			Decision:      DecisionAllow,
			SystemMessage: msg,
		}
	}
	return &HookOutput{
		Decision: DecisionDeny,
		Reason:   text,
	}
}

// applyHookOutputToInput modifies input for sequential execution.
func applyHookOutputToInput(input HookInput, output *HookOutput, event HookEventName) HookInput {
	if output == nil {
		return input
	}
	modified := input

	switch event {
	case EventBeforeAgent, EventFirstTurn, EventSessionStart:
		if ctx := output.AdditionalContext(); ctx != "" {
			if modified.Prompt != "" {
				modified.Prompt += "\n\n" + ctx
			} else {
				modified.Prompt = ctx
			}
		}
	case EventBeforeTool:
		if modArgs := output.ModifiedToolInput(); modArgs != nil {
			if modified.ToolInput == nil {
				modified.ToolInput = make(map[string]any)
			}
			for k, v := range modArgs {
				modified.ToolInput[k] = v
			}
		}
	}

	return modified
}

// limitedBuffer is a bytes.Buffer that caps written bytes.
type limitedBuffer struct {
	buf bytes.Buffer
	cap int
	mu  sync.Mutex
}

func (b *limitedBuffer) Write(p []byte) (n int, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.cap > 0 && b.buf.Len() >= b.cap {
		return len(p), nil // discard extra
	}
	toWrite := p
	if b.cap > 0 && b.buf.Len()+len(p) > b.cap {
		toWrite = p[:b.cap-b.buf.Len()]
	}
	n, err = b.buf.Write(toWrite)
	if err != nil {
		return n, err
	}
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

var _ io.Writer = (*limitedBuffer)(nil)
