package agent

import (
	"context"
	"log/slog"

	"github.com/undeadindustries/sagittarius/internal/hooks"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

// FireHookEvent builds a HookInput and executes matching hooks in r.hooksRegistry.
func (r *Runner) FireHookEvent(ctx context.Context, event hooks.HookEventName, target string, customizeInput func(*hooks.HookInput), streamOut chan<- ui.StreamEvent) ([]hooks.ExecutionResult, error) {
	if r == nil || r.hooksRegistry == nil {
		return nil, nil
	}

	sessionID := r.CurrentSessionID()
	transcriptPath := r.SessionFilePath()
	turnIndex := r.TurnCounter()

	input := hooks.NewHookInput(sessionID, transcriptPath, r.workDir, event, turnIndex)
	if customizeInput != nil {
		customizeInput(&input)
	}

	results, err := r.hooksRegistry.FireEvent(ctx, event, target, input)
	if err != nil {
		slog.Warn("hook execution error", "event", event, "error", err)
	}

	for _, res := range results {
		if res.Stderr != "" {
			slog.Debug("hook stderr", "event", event, "hook", res.HookConfig.Key(), "stderr", res.Stderr)
		}
		if res.Output != nil && res.Output.SystemMessage != "" && streamOut != nil {
			streamOut <- ui.StreamEvent{
				Type: ui.StreamInfo,
				Text: res.Output.SystemMessage,
			}
		}
		if res.Error != nil {
			slog.Warn("hook execution failed", "event", event, "hook", res.HookConfig.Key(), "error", res.Error)
		}
	}

	return results, err
}

// fireAfterAgentHooks executes FirstTurn (once per session) and AfterAgent hooks.
func (r *Runner) fireAfterAgentHooks(ctx context.Context, userInput, assistantReply string, out chan<- ui.StreamEvent) {
	if r == nil || r.hooksRegistry == nil {
		return
	}

	r.firstTurnOnce.Do(func() {
		_, _ = r.FireHookEvent(ctx, hooks.EventFirstTurn, "", func(inp *hooks.HookInput) {
			inp.Prompt = userInput
			inp.PromptResponse = assistantReply
		}, out)
	})

	_, _ = r.FireHookEvent(ctx, hooks.EventAfterAgent, "", func(inp *hooks.HookInput) {
		inp.Prompt = userInput
		inp.PromptResponse = assistantReply
	}, out)
}

// OnWillCompress fires PreCompress hooks before compression runs.
func (r *Runner) OnWillCompress(ctx context.Context, trigger string) {
	if r == nil || r.hooksRegistry == nil {
		return
	}
	_, _ = r.FireHookEvent(ctx, hooks.EventPreCompress, trigger, func(inp *hooks.HookInput) {
		inp.PreCompressTrigger = trigger
	}, nil)
}

func (r *Runner) beforeToolHook(ctx context.Context, toolName string, args map[string]any) (map[string]any, bool, string, error) {
	if r == nil || r.hooksRegistry == nil {
		return nil, false, "", nil
	}
	results, err := r.FireHookEvent(ctx, hooks.EventBeforeTool, toolName, func(inp *hooks.HookInput) {
		inp.ToolName = toolName
		inp.ToolInput = args
	}, nil)

	var modArgs map[string]any
	for _, res := range results {
		if res.Output != nil {
			if res.Output.IsBlocking() || res.Output.ShouldStop() {
				reason := res.Output.EffectiveReason("Tool execution denied by hook")
				return nil, true, reason, err
			}
			if m := res.Output.ModifiedToolInput(); m != nil {
				modArgs = m
			}
		}
	}

	return modArgs, false, "", err
}

func (r *Runner) afterToolHook(ctx context.Context, toolName string, args map[string]any, result map[string]any) {
	if r == nil || r.hooksRegistry == nil {
		return
	}
	_, _ = r.FireHookEvent(ctx, hooks.EventAfterTool, toolName, func(inp *hooks.HookInput) {
		inp.ToolName = toolName
		inp.ToolInput = args
		inp.ToolResponse = result
	}, nil)
}

// HooksRegistry returns the active hooks registry (or nil).
func (r *Runner) HooksRegistry() *hooks.Registry {
	if r == nil {
		return nil
	}
	return r.hooksRegistry
}

// SetHooksRegistry sets the active hooks registry.
func (r *Runner) SetHooksRegistry(reg *hooks.Registry) {
	if r == nil {
		return
	}
	r.hooksRegistry = reg
}
