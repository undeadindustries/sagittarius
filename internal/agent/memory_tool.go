package agent

import (
	"context"
	"fmt"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/tools"
)

// saveMemoryTool is the model-callable counterpart to /memory add: a thin,
// append-only, confirmation-gated wrapper over AddMemory. It deliberately
// cannot list or remove entries — the user owns deletion, and the model
// already sees every memory file's content in its system prompt.
type saveMemoryTool struct {
	runner *Runner
}

func newSaveMemoryTool(r *Runner) tools.Tool {
	return &saveMemoryTool{runner: r}
}

func (t *saveMemoryTool) Name() string { return tools.SaveMemoryToolName }

func (t *saveMemoryTool) Description() string {
	return "Save a durable fact, preference, or instruction for future sessions. " +
		"Appends one line to AGENTS.md (global by default, or the current project). " +
		"Use sparingly, for things worth remembering across the whole conversation history, not one-off task details."
}

func (t *saveMemoryTool) Declaration() provider.ToolDeclaration {
	return provider.ToolDeclaration{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				tools.SaveMemoryParamText: map[string]any{
					"type":        "string",
					"description": "The fact to remember, as a single self-contained sentence.",
				},
				tools.SaveMemoryParamScope: map[string]any{
					"type":        "string",
					"enum":        []string{"global", "project"},
					"description": "Where to save it: \"global\" (default, applies to every project) or \"project\" (this repository only).",
				},
			},
			"required": []string{tools.SaveMemoryParamText},
		},
	}
}

func (t *saveMemoryTool) RequiresConfirmation() bool { return true }

func (t *saveMemoryTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	text, err := stringArg(args, tools.SaveMemoryParamText)
	if err != nil {
		return nil, err
	}
	scope := parseMemoryScopeArg(args)

	path, err := AddMemory(scope, t.runner.workDir, text)
	if err != nil {
		return nil, fmt.Errorf("save memory: %w", err)
	}
	if err := t.runner.ReloadSystemInstruction(); err != nil {
		return nil, fmt.Errorf("memory saved to %s, but reload failed: %w", path, err)
	}

	return map[string]any{
		"status": "saved",
		"path":   path,
		"scope":  scope.String(),
		"text":   text,
	}, nil
}

// parseMemoryScopeArg reads the optional "scope" argument, defaulting to
// global (matching gemini-cli's save_memory target) for any missing or
// unrecognized value.
func parseMemoryScopeArg(args map[string]any) config.SettingScope {
	if s, _ := stringArg(args, tools.SaveMemoryParamScope); s == "project" {
		return config.ScopeProject
	}
	return config.ScopeGlobal
}
