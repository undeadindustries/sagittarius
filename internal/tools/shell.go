package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/undeadindustries/sagittarius/internal/provider"
)

const shellTimeout = 5 * time.Minute

type shellTool struct {
	ws *Workspace
}

func newShellTool(ws *Workspace) Tool {
	return &shellTool{ws: ws}
}

func (t *shellTool) Name() string { return ShellToolName }

func (t *shellTool) RequiresConfirmation() bool { return true }

func (t *shellTool) Declaration() provider.ToolDeclaration {
	return provider.ToolDeclaration{
		Name: ShellToolName,
		Description: "Executes a given shell command as `bash -c <command>`. " +
			"Command is executed as a subprocess that leads its own process group.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				ShellParamCommand: map[string]any{
					"type":        "string",
					"description": "Exact bash command to execute as `bash -c <command>`",
				},
				ShellParamIsBackground: map[string]any{
					"type":        "boolean",
					"description": "Optional: run command in background (not supported in Phase 08; runs synchronously).",
				},
			},
			"required": []string{ShellParamCommand},
		},
	}
}

func (t *shellTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
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

	runCtx, cancel := context.WithTimeout(ctx, shellTimeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "bash", "-c", command)
	cmd.Dir = t.ws.Root()
	cmd.Env = os.Environ()
	// Put the child in its own process group so that background subprocesses
	// spawned via `cmd &` can be killed as a group when context is canceled.
	// Without this, the child inherits our pipe FDs and cmd.Run() blocks until
	// the long-lived background process eventually exits.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			// Kill the entire process group to reap background children.
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		if cmd.Process != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
	// After SIGKILL, wait at most 5 s for I/O goroutines to drain before
	// forcibly closing the pipes and returning.
	cmd.WaitDelay = 5 * time.Second

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	out := strings.TrimSpace(stdout.String())
	errOut := strings.TrimSpace(stderr.String())
	combined := out
	if errOut != "" {
		if combined != "" {
			combined += "\n"
		}
		combined += errOut
	}
	if combined == "" {
		combined = "(empty)"
	}

	result := map[string]any{
		"output": combined,
	}
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result["exit_code"] = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("shell execution failed: %w", err)
		}
	}
	return result, nil
}
