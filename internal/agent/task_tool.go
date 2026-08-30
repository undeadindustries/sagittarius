package agent

import (
	"context"
	"fmt"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/modes"
	"github.com/undeadindustries/sagittarius/internal/prompt"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/tools"
)

type taskTool struct {
	runner *Runner
}

func newTaskTool(r *Runner) tools.Tool {
	return &taskTool{runner: r}
}

func (t *taskTool) Name() string { return tools.TaskToolName }

func (t *taskTool) Description() string {
	return "Launch a new read-only agent to handle research, analysis, or codebase exploration autonomously. Use this to isolate context and prevent large searches or file reads from polluting your own context window. When run_script is available, prefer it for multi-step read-only research (grep, read, list) that does not need a nested agent — it collapses those calls into one turn."
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
	return t.ExecuteStream(ctx, args, func(string) {})
}

func (t *taskTool) ExecuteStream(ctx context.Context, args map[string]any, sink tools.ToolOutputSink) (map[string]any, error) {
	promptText, err := stringArg(args, tools.TaskParamPrompt)
	if err != nil {
		return nil, err
	}
	desc, _ := stringArg(args, tools.TaskParamDescription)
	if desc == "" {
		desc = "Research subagent"
	}
	if err := t.runner.checkSubagentDepth(); err != nil {
		return nil, err
	}

	// ModeAsk is what makes the child read-only: the scheduler's ask gate
	// denies everything outside readOnlyBuiltinTools, so no lease is needed.
	child, err := t.runner.newSubagent(ctx, subagentSpec{
		description: desc,
		mode:        modes.ModeAsk,
		class:       config.SubagentResearch,
		charter:     prompt.ResearchSubagentCharter(),
		approval:    t.runner.approval,
	})
	if err != nil {
		return nil, err
	}

	text, err := child.run(ctx, promptText, sink)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"status": "completed",
		"result": text,
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
