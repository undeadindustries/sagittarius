package tools

import (
	"context"

	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/skills"
)

const activateSkillToolName = "activate_skill"

// ActivateSkillTool loads specialized procedural expertise from discovered skills.
type ActivateSkillTool struct {
	manager *skills.Manager
}

// NewActivateSkillTool constructs the activate_skill built-in tool.
func NewActivateSkillTool(manager *skills.Manager) *ActivateSkillTool {
	return &ActivateSkillTool{manager: manager}
}

func (t *ActivateSkillTool) Name() string { return activateSkillToolName }

func (t *ActivateSkillTool) RequiresConfirmation() bool { return false }

func (t *ActivateSkillTool) Declaration() provider.ToolDeclaration {
	names := []string{""}
	if t.manager != nil {
		sk := t.manager.Skills()
		names = make([]string, 0, len(sk))
		for _, s := range sk {
			names = append(names, s.Name)
		}
		if len(names) == 0 {
			names = []string{""}
		}
	}
	return provider.ToolDeclaration{
		Name:        activateSkillToolName,
		Description: "Activate a discovered agent skill and load its instructions.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "The skill name to activate.",
					"enum":        names,
				},
			},
			"required": []string{"name"},
		},
	}
}

func (t *ActivateSkillTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	if t.manager == nil {
		return map[string]any{"error": "skill manager unavailable"}, nil
	}
	raw, ok := args["name"]
	if !ok {
		return map[string]any{"error": `missing required parameter "name"`}, nil
	}
	name, ok := raw.(string)
	if !ok || name == "" {
		return map[string]any{"error": `parameter "name" must be a non-empty string`}, nil
	}
	content, err := t.manager.ActivateContent(name)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	return map[string]any{"result": content}, nil
}

var _ Tool = (*ActivateSkillTool)(nil)
