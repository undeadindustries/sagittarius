package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/grill"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/tools"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

// askUserTool is grill mode's structured interrogation primitive: it poses one
// question with 2-4 answer options to the user (via ui.StreamAskUser) and
// records the resolved answer as a grill.Decision. It is read-only and always
// permitted by the grill-mode tool gate regardless of the underlying
// interaction mode (see tools.grillModeAllow).
type askUserTool struct {
	runner *Runner
}

func (t *askUserTool) Name() string               { return tools.AskUserToolName }
func (t *askUserTool) RequiresConfirmation() bool { return false }

func (t *askUserTool) Declaration() provider.ToolDeclaration {
	return provider.ToolDeclaration{
		Name: tools.AskUserToolName,
		Description: "Ask the user ONE structured question with 2-4 short answer options. " +
			"Only usable during grill-me interrogation. Put the recommended option first " +
			"(or set recommended_index); the user can always type a free-form answer instead.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"question": map[string]any{
					"type":        "string",
					"description": "The single highest-uncertainty question to ask next.",
				},
				"options": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"label":       map[string]any{"type": "string"},
							"description": map[string]any{"type": "string"},
						},
						"required": []string{"label"},
					},
					"description": "2 to 4 candidate answers, most-recommended first.",
				},
				"recommended_index": map[string]any{
					"type":        "integer",
					"description": "0-based index into options of the recommended answer (defaults to 0).",
				},
			},
			"required": []string{"question", "options"},
		},
	}
}

// Execute satisfies the plain Tool interface for callers that bypass the
// scheduler's InteractiveTool dispatch (e.g. direct unit tests); the scheduler
// itself always calls ExecuteInteractive for a registered InteractiveTool.
func (t *askUserTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	return t.ExecuteInteractive(ctx, args, false, nil)
}

func (t *askUserTool) ExecuteInteractive(
	ctx context.Context,
	args map[string]any,
	interactive bool,
	emit func(ui.StreamEvent),
) (map[string]any, error) {
	question, ok := args["question"].(string)
	if !ok || strings.TrimSpace(question) == "" {
		return nil, fmt.Errorf("missing required parameter 'question'")
	}
	options, err := parseAskOptions(args["options"])
	if err != nil {
		return nil, err
	}
	recommended := askRecommendedIndex(args, len(options))

	// Refuse to keep interrogating while the session is paused: /grill pause is
	// documented as suspending the interview, so the model must wait for
	// /grill resume before posing another question.
	if g := t.runner.Grill(); g != nil && g.Status == grill.StatusPaused {
		return nil, fmt.Errorf("grill session is paused; the user must run /grill resume before you ask another question")
	}

	answer := ui.AskAnswer{Index: recommended, Text: options[recommended].Label}
	if interactive && emit != nil {
		replyCh := make(chan ui.AskAnswer, 1)
		emit(ui.StreamEvent{
			Type:           ui.StreamAskUser,
			AskQuestion:    question,
			AskOptions:     options,
			AskRecommended: recommended,
			AskReply:       replyCh,
		})
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case answer = <-replyCh:
		}
	}

	t.recordDecision(question, answer.Text)
	return map[string]any{"answer": answer.Text}, nil
}

// parseAskOptions validates and converts the wire "options" argument into
// ui.AskOption values, skipping malformed entries and requiring at least one
// well-formed {label} to remain.
func parseAskOptions(raw any) ([]ui.AskOption, error) {
	rawOptions, ok := raw.([]any)
	if !ok || len(rawOptions) == 0 {
		return nil, fmt.Errorf("missing required parameter 'options'")
	}
	options := make([]ui.AskOption, 0, len(rawOptions))
	for _, entry := range rawOptions {
		m, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		label, _ := m["label"].(string)
		if strings.TrimSpace(label) == "" {
			continue
		}
		desc, _ := m["description"].(string)
		options = append(options, ui.AskOption{Label: label, Description: desc})
	}
	if len(options) == 0 {
		return nil, fmt.Errorf("parameter 'options' must contain at least one entry with a non-empty 'label'")
	}
	return options, nil
}

// askRecommendedIndex extracts recommended_index from args, clamping to a
// valid position (defaulting to 0 when absent or out of range).
func askRecommendedIndex(args map[string]any, optionCount int) int {
	recommended := 0
	switch v := args["recommended_index"].(type) {
	case float64:
		recommended = int(v)
	case int:
		recommended = v
	}
	if recommended < 0 || recommended >= optionCount {
		return 0
	}
	return recommended
}

// recordDecision appends the resolved Q/A pair to the active grill session, a
// no-op when grill mode is not (or no longer) active. The mutation runs under
// the runner's grill lock (UpdateGrill) so it does not race a concurrent
// /grill pause running on a background goroutine.
func (t *askUserTool) recordDecision(question, answer string) {
	_ = t.runner.UpdateGrill(func(g *grill.Session) error {
		g.QuestionCount++
		g.Decisions = append(g.Decisions, grill.Decision{Question: question, Answer: answer})
		return nil
	})
}

func registerGrillTools(runner *Runner, registry *tools.Registry) {
	registry.Register(&askUserTool{runner: runner})
}
