package agent

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/undeadindustries/sagittarius/internal/contextmgmt"
	"github.com/undeadindustries/sagittarius/internal/modes"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/session"
	"github.com/undeadindustries/sagittarius/internal/tools"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

type taskTool struct {
	runner *Runner
}

func newTaskTool(r *Runner) tools.Tool {
	return &taskTool{runner: r}
}

func (t *taskTool) Name() string { return tools.TaskToolName }

func (t *taskTool) Description() string {
	return "Launch a new read-only agent to handle research, analysis, or codebase exploration autonomously. Use this to isolate context and prevent large searches or file reads from polluting your own context window."
}

func (t *taskTool) Declaration() provider.ToolDeclaration {
	return provider.ToolDeclaration{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				tools.TaskParamDescription: map[string]any{
					"type":        "string",
					"description": "A short, user-friendly title for the subagent task (e.g. 'Research database schema').",
				},
				tools.TaskParamPrompt: map[string]any{
					"type":        "string",
					"description": "The detailed instructions for the subagent.",
				},
			},
			"required": []string{tools.TaskParamDescription, tools.TaskParamPrompt},
		},
	}
}

func (t *taskTool) RequiresConfirmation() bool { return false }

func (t *taskTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	return t.ExecuteStream(ctx, args, func(s string) {})
}

func (t *taskTool) ExecuteStream(ctx context.Context, args map[string]any, sink tools.ToolOutputSink) (map[string]any, error) {
	prompt, err := stringArg(args, tools.TaskParamPrompt)
	if err != nil {
		return nil, err
	}
	desc, _ := stringArg(args, tools.TaskParamDescription)
	if desc == "" {
		desc = "Subagent task"
	}

	// 1. Check if depth 1 is respected (parent is not already a subagent)
	if t.runner.sessionRecorder != nil && t.runner.sessionRecorder.Kind() == "subagent" {
		return nil, fmt.Errorf("subagents cannot launch further subagents (depth limit 1)")
	}

	subID := uuid.New().String()

	// Create context manager
	outputDir, _ := session.ChatsDir(t.runner.workspace.Root())
	ctxMgr := contextmgmt.NewManager(contextmgmt.ManagerConfig{
		Enabled:   true,
		SessionID: subID,
		OutputDir: outputDir,
	})

	// Create session recorder
	var rec *session.Recorder
	if t.runner.sessionRecorder != nil {
		hash := session.ProjectHash(t.runner.workspace.Root())
		chatsDir, cdErr := session.ChatsDir(t.runner.workspace.Root())
		if cdErr == nil {
			rec = session.NewRecorder(chatsDir, subID, hash, "subagent")
		}
	}

	// Use parent's settings
	settings := t.runner.settingsSnapshot()

	// Child is strictly read-only: ModeAsk.
	// But ask mode also changes the prompt. So we just use ask mode,
	// which automatically enforces readOnlyBuiltinTools in the scheduler gate.

	// Use parent's model. BUT, to avoid OpenAIResponses generator chaining hazard,
	// we shouldn't share the exact generator instance if it holds state.
	// Actually, RebuildRunner builds a new generator from cache, so if they use the same
	// provider/model, they get the SAME cached generator.
	// This is the chaining hazard.
	// In the next step, I'll fix the cache to return a new generator or disable chaining.

	cfg := RunnerConfig{
		Model:           t.runner.model,
		ModelPinned:     t.runner.modelPinned,
		WorkDir:         t.runner.workspace.Root(),
		ApprovalMode:    t.runner.approval,
		Interactive:     false, // headless execution
		ContextManager:  ctxMgr,
		SessionRecorder: rec,
		Settings:        settings,
		ProjectBoundary: t.runner.projectBoundary,
		Snapshotter:     nil,              // No writes allowed anyway
		InitialMode:     modes.ModeAsk,    // ModeAsk ensures read-only enforcement
		Runtime:         t.runner.runtime, // Share MCP and bgproc
	}

	// We need a generator. We must get it from provider layer, but bypass cache or fix cache.

	// Let's call a helper to get generator.
	// For now, let's use the standard flow: build generator from provider settings.
	// We'll update the generator cache to not cache responses API if chaining is enabled, or something.

	gen, err := provider.NewContentGenerator(ctx, settings)
	if err != nil {
		return nil, fmt.Errorf("failed to build subagent generator: %w", err)
	}
	cfg.Generator = gen

	subRunner, err := NewRunner(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create subagent runner: %w", err)
	}

	// Submit prompt to subagent
	slog.Info("launching subagent", "description", desc, "sessionID", subID)

	// Since we are inside Execute, which runs concurrently (later), we can block here until it finishes.
	// RunTurn handles the agent loop.
	stream, err := subRunner.RunTurn(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("subagent failed: %w", err)
	}

	// Drain stream
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case ev, ok := <-stream:
			if !ok {
				goto Done
			}
			switch ev.Type {
			case ui.StreamToolStart:
				sink("Running " + ev.ToolName + "...")
			case ui.StreamToolResult:
				sink("Finished " + ev.ToolName)
			case ui.StreamTextDelta, ui.StreamReasoningDelta:
				if ev.Text != "" {
					sink("Thinking...")
				}
			}
		}
	}
Done:

	// Result is the last assistant text.
	res := subRunner.LastAssistantText()
	if res == "" {
		res = "(Subagent finished without producing text)"
	}

	return map[string]any{
		"status": "completed",
		"result": res,
	}, nil
}

func stringArg(args map[string]any, key string) (string, error) {
	raw, ok := args[key]
	if !ok {
		return "", fmt.Errorf("missing required parameter %q", key)
	}
	s, ok := raw.(string)
	if !ok || s == "" {
		return "", fmt.Errorf("parameter %q must be a non-empty string", key)
	}
	return s, nil
}
