package agent

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/undeadindustries/sagittarius/internal/slash"
	"github.com/undeadindustries/sagittarius/internal/tools"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

const bangUsage = "Usage: !<command>  or  /run <command>"

type userShellExecutor interface {
	ExecuteUser(ctx context.Context, command string, sink tools.ToolOutputSink, stdin *tools.PTYStdin) (map[string]any, error)
}

// HandleBang runs a user-typed `!` / `/run` command in the PTY and streams
// tool-card events. The command and its output are not written to history,
// the session recorder, or metrics.
func (a *App) HandleBang(ctx context.Context, input string) (<-chan ui.StreamEvent, error) {
	out := make(chan ui.StreamEvent, 16)
	go func() {
		defer close(out)
		a.runBang(ctx, input, out)
		out <- ui.StreamEvent{Type: ui.StreamDone}
	}()
	return out, nil
}

// WriteBangInput implements ui.BangInputWriter.
func (a *App) WriteBangInput(p []byte) error {
	a.bangMu.Lock()
	stdin := a.bangStdin
	a.bangMu.Unlock()
	if stdin == nil {
		return tools.ErrPTYClosed
	}
	_, err := stdin.Write(p)
	return err
}

func (a *App) setBangStdin(stdin *tools.PTYStdin) {
	a.bangMu.Lock()
	a.bangStdin = stdin
	a.bangMu.Unlock()
}

func (a *App) runBang(ctx context.Context, input string, out chan<- ui.StreamEvent) {
	command, ok := slash.ParseBangCommand(input)
	if !ok || command == "" {
		out <- ui.StreamEvent{Type: ui.StreamInfo, Text: bangUsage + "\n"}
		return
	}

	exec, err := a.lookupUserShell()
	if err != nil {
		out <- ui.StreamEvent{Type: ui.StreamError, Err: err}
		return
	}

	args := map[string]any{tools.ShellParamCommand: command}
	id := ui.BangCallIDPrefix + strconv.FormatInt(time.Now().UnixNano(), 10)
	out <- ui.StreamEvent{
		Type:       ui.StreamToolStart,
		ToolName:   tools.ShellToolName,
		ToolCallID: id,
		Text:       command,
	}

	if reason, blocked := a.bangBoundaryDenied(args); blocked {
		out <- ui.StreamEvent{
			Type:       ui.StreamToolResult,
			ToolName:   tools.ShellToolName,
			ToolCallID: id,
			Text:       reason,
			IsError:    true,
		}
		return
	}

	stdin := &tools.PTYStdin{}
	a.setBangStdin(stdin)
	defer a.setBangStdin(nil)

	sink := func(text string) {
		select {
		case <-ctx.Done():
		case out <- ui.StreamEvent{
			Type:       ui.StreamToolOutput,
			ToolName:   tools.ShellToolName,
			ToolCallID: id,
			Text:       text,
		}:
		default:
		}
	}

	result, execErr := exec.ExecuteUser(ctx, command, sink, stdin)
	if execErr != nil {
		out <- ui.StreamEvent{
			Type:       ui.StreamToolResult,
			ToolName:   tools.ShellToolName,
			ToolCallID: id,
			Text:       execErr.Error(),
			IsError:    true,
		}
		return
	}

	text, code, isErr := tools.FormatShellResult(result)
	out <- ui.StreamEvent{
		Type:       ui.StreamToolResult,
		ToolName:   tools.ShellToolName,
		ToolCallID: id,
		Text:       text,
		ExitCode:   code,
		IsError:    isErr,
	}
}

func (a *App) lookupUserShell() (userShellExecutor, error) {
	if a == nil || a.runtime == nil {
		return nil, fmt.Errorf("shell is not available")
	}
	reg := a.runtime.Registry()
	if reg == nil {
		return nil, fmt.Errorf("shell is not available")
	}
	tool, ok := reg.Lookup(tools.ShellToolName)
	if !ok {
		return nil, fmt.Errorf("shell is not available")
	}
	exec, ok := tool.(userShellExecutor)
	if !ok {
		return nil, fmt.Errorf("shell is not available")
	}
	return exec, nil
}

func (a *App) bangBoundaryDenied(args map[string]any) (reason string, denied bool) {
	if a.runner == nil {
		return "", false
	}
	allowed, reason := tools.ProjectBoundaryAllow(
		a.runner.projectBoundary, tools.ShellToolName, args, a.runner.Workspace())
	if allowed {
		return "", false
	}
	return reason, true
}
