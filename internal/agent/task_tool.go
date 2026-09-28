package agent

import (
	"context"
	"errors"
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

// StartBadge implements tools.StartBadger: the card border shows the child's
// resolved provider/model when routing pins send it somewhere other than the
// parent's live pair.
func (t *taskTool) StartBadge() string { return subagentBadge(t.runner, config.SubagentResearch) }

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

	// The attempt budget stops a delegation loop: a task that keeps coming
	// back is refused before the child starts, with the refusal telling the
	// parent to finish it directly.
	attempt, maxAttempts, err := t.runner.claimSubagentAttempt(config.SubagentResearch, desc, nil)
	if err != nil {
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
		t.runner.releaseSubagentAttempt(config.SubagentResearch, desc, nil)
		return nil, err
	}

	// The child's report is read back from its runner inside
	// buildSubagentResult; run's text return is superseded by the schema.
	_, runErr := child.run(ctx, promptText, sink)
	if runErr != nil && ctx.Err() != nil {
		// A user cancel of this one child (AD-154) is a hand-off, not a tool
		// error: the parent needs the partial state, and the batch goes on.
		if errors.Is(context.Cause(ctx), tools.ErrSubagentCanceledByUser) {
			result := canceledSubagentResult(child, config.SubagentResearch, runErr, attempt, maxAttempts)
			if via := subagentViaLabel(t.runner, child); via != "" {
				result["via"] = via
			}
			return result, nil
		}
		return nil, runErr
	}
	result := buildSubagentResult(child, config.SubagentResearch, runErr, attempt, maxAttempts)
	if via := subagentViaLabel(t.runner, child); via != "" {
		result["via"] = via
	}
	return result, nil
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

// presentStringArg requires the key to be present and a string but allows the
// empty value, mirroring the tools package helper of the same name. Use it where
// an empty string is a meaningful instruction (clearing the scratchpad) rather
// than a missing argument.
func presentStringArg(args map[string]any, key string) (string, error) {
	raw, ok := args[key]
	if !ok {
		return "", fmt.Errorf("missing required parameter %q", key)
	}
	s, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("parameter %q must be a string", key)
	}
	return s, nil
}
