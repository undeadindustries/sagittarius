package slash

import (
	"fmt"

	"github.com/undeadindustries/sagittarius/internal/grill"
)

func grillCommand() Command {
	return Command{
		Name:        "grill",
		Description: "Socratic interrogation mode: the agent interviews you about a topic",
		Handler:     handleGrill,
		SubCommands: []Command{
			{Name: "start", Description: "Start a new grill session", Handler: handleGrillStart},
			{Name: "status", Description: "Show the current grill session", Handler: handleGrillStatus},
			{Name: "pause", Description: "Pause the active grill session", Handler: handleGrillPause},
			{Name: "resume", Description: "Resume a paused grill session", Handler: handleGrillResume},
			{Name: "done", Description: "Finish interrogation and generate the spec", Handler: handleGrillDone},
			{Name: "finish", Description: "Alias for done", Handler: handleGrillDone},
			{Name: "clear", Description: "Cancel the grill session", Handler: handleGrillClear},
			{Name: "stop", Description: "Alias for clear", Handler: handleGrillClear},
		},
	}
}

func handleGrill(ctx *Context) Result {
	if ctx.Args == "" {
		return handleGrillStatus(ctx)
	}
	return handleGrillStart(ctx)
}

func handleGrillStart(ctx *Context) Result {
	g := ctx.Deps.Hooks.GrillStatus()
	if g != nil && g.Status != grill.StatusComplete {
		return ErrorResult(fmt.Errorf("Grill error: a session on %q is already active; finish (/grill done) or clear it before starting a new one", g.Topic))
	}

	mode, _ := ctx.Deps.Hooks.InteractionMode()
	if mode.String() == "plan" || mode.String() == "ask" {
		return ErrorResult(fmt.Errorf("Grill error: agent mode is required for grill sessions (current mode is %s)", mode.String()))
	}

	if ctx.Args == "" {
		return ErrorResult(fmt.Errorf("Usage: /grill <topic>"))
	}

	if err := ctx.Deps.Hooks.SetGrill(ctx.Args); err != nil {
		return ErrorResult(err)
	}

	return Result{
		Handled:      true,
		Messages:     []string{"Grill session started. Interrogating..."},
		SubmitPrompt: fmt.Sprintf("Begin the grill-me interrogation for: %s", ctx.Args),
	}
}

func handleGrillStatus(ctx *Context) Result {
	g := ctx.Deps.Hooks.GrillStatus()
	if g == nil {
		return InfoResult("No active grill session.")
	}

	msg := fmt.Sprintf("Grill\nStatus: %s\nTopic: %s\nQuestions asked: %d\n", g.Status, g.Topic, g.QuestionCount)
	if g.Note != "" {
		msg += fmt.Sprintf("Note: %s\n", g.Note)
	}
	return InfoResult(msg)
}

func handleGrillPause(ctx *Context) Result {
	if err := ctx.Deps.Hooks.PauseGrill(ctx.Args); err != nil {
		return ErrorResult(err)
	}
	return InfoResult("Grill session paused.")
}

func handleGrillResume(ctx *Context) Result {
	if err := ctx.Deps.Hooks.ResumeGrill(ctx.Args); err != nil {
		return ErrorResult(err)
	}
	return InfoResult("Grill session resumed.")
}

func handleGrillDone(ctx *Context) Result {
	specPrompt, err := ctx.Deps.Hooks.EndGrill(ctx.Args)
	if err != nil {
		return ErrorResult(err)
	}
	return Result{
		Handled:            true,
		Messages:           []string{"Wrapping up the interrogation and writing the spec..."},
		SubmitPrompt:       specPrompt,
		CompleteGrillAfter: true,
	}
}

func handleGrillClear(ctx *Context) Result {
	if err := ctx.Deps.Hooks.ClearGrill(ctx.Args); err != nil {
		return ErrorResult(err)
	}
	return InfoResult("Grill session cleared.")
}
