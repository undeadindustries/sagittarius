package agent

import (
	"context"
	"fmt"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/tools"
)

// updateScratchpadTool lets the model maintain a short working-memory note that
// lives outside the conversation history, so it survives masking, compression,
// and truncation. It is the within-task counterpart to save_memory: that tool
// writes durable facts to AGENTS.md for future sessions, this one holds state
// for the task currently in progress.
type updateScratchpadTool struct {
	runner *Runner
}

func newUpdateScratchpadTool(r *Runner) tools.Tool {
	return &updateScratchpadTool{runner: r}
}

// registerWorkingMemoryTools registers the within-task memory tools. The
// scratchpad is gated because it costs tokens on every request once populated;
// search_session is not, because it costs one tool declaration and reads a file
// the session is already writing.
// Subagents get neither tool: a child is a context firewall with its own
// recorder, so the parent's scratchpad would leak context.
func registerWorkingMemoryTools(r *Runner, registry *tools.Registry, settings *config.Settings) {
	if r == nil || registry == nil {
		return
	}
	if r.isSubagent() {
		return
	}
	if config.ScratchpadEnabled(settings, nil) {
		registry.Register(newUpdateScratchpadTool(r))
	}
	registry.Register(newSearchSessionTool(r))
}

func (t *updateScratchpadTool) Name() string { return tools.UpdateScratchpadToolName }

func (t *updateScratchpadTool) Description() string {
	return fmt.Sprintf(
		"Replace your scratchpad: a short working note that survives context compression, "+
			"unlike anything said in the conversation. Use it for state you must not lose on a long task — "+
			"the plan and which step you are on, exact paths, ports, ids, and decisions already made. "+
			"You always see the current note in your system prompt, so send the full replacement text "+
			"(there is no append mode); send an empty string to clear it. Capped at %d characters.",
		ScratchpadMaxRunes,
	)
}

func (t *updateScratchpadTool) Declaration() provider.ToolDeclaration {
	return provider.ToolDeclaration{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				tools.ScratchpadParamContent: map[string]any{
					"type":        "string",
					"description": "The complete new scratchpad text. Replaces the previous note entirely. Empty string clears it.",
				},
			},
			"required": []string{tools.ScratchpadParamContent},
		},
	}
}

// RequiresConfirmation is false by design. save_memory is gated because it
// rewrites AGENTS.md on disk; the scratchpad only changes session state the user
// can read with /scratchpad and wipe with /scratchpad clear. Prompting here
// would make the tool unusable on the unattended long tasks it exists for.
func (t *updateScratchpadTool) RequiresConfirmation() bool { return false }

func (t *updateScratchpadTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	// presentStringArg, not stringArg: clearing the note is a legitimate call and
	// sends an empty string, which stringArg rejects as missing.
	content, err := presentStringArg(args, tools.ScratchpadParamContent)
	if err != nil {
		return nil, err
	}

	requested := len([]rune(content))
	if err := t.runner.SetScratchpad(content); err != nil {
		return nil, fmt.Errorf("update scratchpad: %w", err)
	}
	stored := t.runner.Scratchpad()

	return map[string]any{
		"status":    "updated",
		"content":   stored,
		"runes":     len([]rune(stored)),
		"truncated": requested > ScratchpadMaxRunes,
	}, nil
}
