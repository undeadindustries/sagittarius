package slash

import (
	"fmt"

	"github.com/undeadindustries/sagittarius/internal/goal"
)

func goalCommand() Command {
	return Command{
		Name:        "goal",
		Description: "Autonomous run-until-done mode",
		Handler:     handleGoal,
		SubCommands: []Command{
			{Name: "start", Description: "Start a new goal", Handler: handleGoalStart},
			{Name: "set", Description: "Alias for start", Handler: handleGoalStart},
			{Name: "create", Description: "Alias for start", Handler: handleGoalStart},
			{Name: "status", Description: "Show the current goal", Handler: handleGoalStatus},
			{Name: "pause", Description: "Pause the active goal", Handler: handleGoalPause},
			{Name: "resume", Description: "Resume a paused or blocked goal", Handler: handleGoalResume},
			{Name: "complete", Description: "Mark the goal achieved", Handler: handleGoalComplete},
			{Name: "done", Description: "Alias for complete", Handler: handleGoalComplete},
			{Name: "block", Description: "Mark the goal blocked", Handler: handleGoalBlock},
			{Name: "blocked", Description: "Alias for block", Handler: handleGoalBlock},
			{Name: "clear", Description: "Remove the goal", Handler: handleGoalClear},
			{Name: "cancel", Description: "Alias for clear", Handler: handleGoalClear},
			{Name: "stop", Description: "Alias for clear", Handler: handleGoalClear},
		},
	}
}

func handleGoal(ctx *Context) Result {
	if ctx.Args == "" {
		return handleGoalStatus(ctx)
	}
	return handleGoalStart(ctx)
}

func handleGoalStart(ctx *Context) Result {
	g := ctx.Deps.Hooks.GoalStatus()
	if g != nil && g.Status != goal.StatusComplete {
		return ErrorResult(fmt.Errorf("Goal error: goal already exists. Clear it before starting a new one."))
	}

	mode, _ := ctx.Deps.Hooks.InteractionMode()
	if mode.String() == "plan" || mode.String() == "ask" {
		return ErrorResult(fmt.Errorf("Goal error: agent mode is required for goals (current mode is %s)", mode.String()))
	}

	if ctx.Args == "" {
		return ErrorResult(fmt.Errorf("Usage: /goal <objective>"))
	}

	if err := ctx.Deps.Hooks.SetGoal(ctx.Args, nil); err != nil {
		return ErrorResult(err)
	}

	return Result{
		Handled:      true,
		Messages:     []string{fmt.Sprintf("Goal created. Starting autonomous loop...")},
		SubmitPrompt: ctx.Args,
	}
}

func handleGoalStatus(ctx *Context) Result {
	g := ctx.Deps.Hooks.GoalStatus()
	if g == nil {
		return InfoResult("No active goal.")
	}

	statusStr := string(g.Status)
	msg := fmt.Sprintf("Goal\nStatus: %s\nObjective: %s\nTurns: %d/%d\n", statusStr, g.Objective, g.TurnCount, g.MaxTurns)
	if g.LastReason != "" {
		msg += fmt.Sprintf("Last evaluator reason: %s\n", g.LastReason)
	}
	if g.Note != "" {
		msg += fmt.Sprintf("Note: %s\n", g.Note)
	}

	return InfoResult(msg)
}

func handleGoalPause(ctx *Context) Result {
	if err := ctx.Deps.Hooks.PauseGoal(ctx.Args); err != nil {
		return ErrorResult(err)
	}
	return InfoResult("Goal paused.")
}

func handleGoalResume(ctx *Context) Result {
	if err := ctx.Deps.Hooks.ResumeGoal(ctx.Args); err != nil {
		return ErrorResult(err)
	}
	return InfoResult("Goal resumed.")
}

func handleGoalComplete(ctx *Context) Result {
	if err := ctx.Deps.Hooks.CompleteGoal(ctx.Args); err != nil {
		return ErrorResult(err)
	}
	return InfoResult("Goal marked complete.")
}

func handleGoalBlock(ctx *Context) Result {
	if err := ctx.Deps.Hooks.BlockGoal(ctx.Args); err != nil {
		return ErrorResult(err)
	}
	return InfoResult("Goal marked blocked.")
}

func handleGoalClear(ctx *Context) Result {
	if err := ctx.Deps.Hooks.ClearGoal(ctx.Args); err != nil {
		return ErrorResult(err)
	}
	return InfoResult("Goal cleared.")
}
