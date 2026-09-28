package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/skills"
)

const activateSkillToolName = "activate_skill"

// maxSkillDescriptionRunes caps each skill's description in the declaration
// catalog (AD-155), following the mcp.pruneToolSchemas precedent.
const maxSkillDescriptionRunes = 200

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
	var enabled []skills.Definition
	if t.manager != nil {
		for _, s := range t.manager.Skills() {
			if name := strings.TrimSpace(s.Name); name != "" {
				s.Name = name
				enabled = append(enabled, s)
			}
		}
	}
	sort.Slice(enabled, func(i, j int) bool {
		return enabled[i].Name < enabled[j].Name
	})

	var names []string
	for _, s := range enabled {
		names = append(names, s.Name)
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

		var b strings.Builder
		b.WriteString(description)
		b.WriteString("\n\nAvailable skills:\n")
		for _, s := range enabled {
			desc := strings.Join(strings.Fields(s.Description), " ")
			if desc != "" {
				desc = truncateSkillDescription(desc, maxSkillDescriptionRunes)
				fmt.Fprintf(&b, "- %s: %s\n", s.Name, desc)
			} else {
				fmt.Fprintf(&b, "- %s\n", s.Name)
			}
		}
		description = strings.TrimRight(b.String(), "\n")
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

// truncateSkillDescription cuts s on a rune boundary at limit, appending "..."
// when truncated.
func truncateSkillDescription(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	if limit <= 3 {
		return string(runes[:limit])
	}
	return string(runes[:limit-3]) + "..."
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
