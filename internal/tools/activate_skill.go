package tools

import (
	"context"
	"strings"

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
	var names []string
	if t.manager != nil {
		for _, s := range t.manager.Skills() {
			if name := strings.TrimSpace(s.Name); name != "" {
				names = append(names, name)
			}
		}
	}

	// Match the fork (dynamic-declaration-helpers.ts getActivateSkillDeclaration):
	// only emit an enum when skills exist. An empty enum — or worse, an enum
	// containing an empty string — is rejected by Gemini (e.g. via OpenRouter):
	// "function_declarations[...].parameters.properties[name].enum[0]: cannot be empty".
	nameSchema := map[string]any{"type": "string"}
	description := "Activates a specialized agent skill by name. Returns the skill's instructions. Use this when a task matches a skill's description."
	if len(names) == 0 {
		nameSchema["description"] = "No skills are currently available."
	} else {
		nameSchema["description"] = "The name of the skill to activate."
		nameSchema["enum"] = names
		quoted := make([]string, len(names))
		for i, n := range names {
			quoted[i] = "'" + n + "'"
		}
		description += " (Available: " + strings.Join(quoted, ", ") + ")"
	}

	return provider.ToolDeclaration{
		Name:        activateSkillToolName,
		Description: description,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": nameSchema,
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
