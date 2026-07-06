package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/goal"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/tools"
)

type getGoalTool struct {
	runner *Runner
}

func (t *getGoalTool) Name() string { return "get_goal" }
func (t *getGoalTool) RequiresConfirmation() bool { return false }
func (t *getGoalTool) Declaration() provider.ToolDeclaration {
	return provider.ToolDeclaration{
		Name:        "get_goal",
		Description: "Returns the current autonomous goal objective, state, and notes.",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}
}
func (t *getGoalTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	g := t.runner.Goal()
	if g == nil {
		return map[string]any{"result": "No active goal."}, nil
	}
	res := fmt.Sprintf("Objective: %s\nStatus: %s\nTurns: %d/%d\nLast Reason: %s\nNote: %s",
		g.Objective, string(g.Status), g.TurnCount, g.MaxTurns, g.LastReason, g.Note)
	return map[string]any{"result": res}, nil
}

type updateGoalTool struct {
	runner *Runner
}

func (t *updateGoalTool) Name() string { return "update_goal" }
func (t *updateGoalTool) RequiresConfirmation() bool { return true }
func (t *updateGoalTool) Declaration() provider.ToolDeclaration {
	return provider.ToolDeclaration{
		Name:        "update_goal",
		Description: "Update the status of the current autonomous goal. Use this to mark a goal as complete once all requirements are met, or blocked if unable to proceed. Must provide detailed evidence or blocker description.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"status": map[string]any{
					"type":        "string",
					"enum":        []string{"complete", "blocked", "active", "paused"},
					"description": "The new status of the goal.",
				},
				"note": map[string]any{
					"type":        "string",
					"description": "Detailed evidence that the goal is complete, or a description of why it is blocked.",
				},
			},
			"required": []string{"status", "note"},
		},
	}
}

func (t *updateGoalTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	statusRaw, ok := args["status"]
	if !ok {
		return nil, fmt.Errorf("missing required parameter 'status'")
	}
	statusStr, ok := statusRaw.(string)
	if !ok {
		return nil, fmt.Errorf("parameter 'status' must be a string")
	}

	noteRaw, ok := args["note"]
	if !ok {
		return nil, fmt.Errorf("missing required parameter 'note'")
	}
	noteStr, ok := noteRaw.(string)
	if !ok || strings.TrimSpace(noteStr) == "" {
		return nil, fmt.Errorf("parameter 'note' must be a non-empty string explaining evidence or blocker")
	}

	g := t.runner.Goal()
	if g == nil {
		return map[string]any{"result": "No active goal to update."}, nil
	}

	newStatus := goal.Status(statusStr)
	switch newStatus {
	case goal.StatusComplete, goal.StatusBlocked, goal.StatusActive, goal.StatusPaused:
		// Valid
	default:
		return nil, fmt.Errorf("invalid status: %q", statusStr)
	}

	g.Status = newStatus
	g.Note = noteStr
	t.runner.SetGoal(g)

	return map[string]any{"result": "Goal status updated successfully."}, nil
}

func registerGoalTools(runner *Runner, registry *tools.Registry) {
	registry.Register(&getGoalTool{runner: runner})
	registry.Register(&updateGoalTool{runner: runner})
}
