package slash

import (
	"fmt"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/modes"
)

func modeCommand() Command {
	return Command{
		Name:        "mode",
		Description: "Show or switch Sagittarius interaction mode (model routing)",
		SubCommands: []Command{
			{
				Name:        "show",
				Description: "Show the active interaction mode and resolved model",
				Handler:     handleModeShow,
			},
			{
				Name:        "agent",
				Description: "Switch to normal agent mode (default model routing)",
				Handler:     handleModeSetAgent,
			},
			{
				Name:        "plan",
				Description: "Switch to plan mode (uses modes.plan.model when configured)",
				Handler:     handleModeSetPlan,
			},
			{
				Name:        "ask",
				Description: "Switch to ask mode (uses modes.ask.model when configured)",
				Handler:     handleModeSetAsk,
			},
			{
				Name:        "debug",
				Description: "Switch to debug mode (verbose logging; optional model override)",
				Handler:     handleModeSetDebug,
			},
		},
		Handler: handleModeRoot,
	}
}

func handleModeShow(ctx *Context) Result {
	if ctx.Deps.Hooks == nil {
		return InfoResult("Mode commands unavailable.")
	}
	mode, model := ctx.Deps.Hooks.InteractionMode()
	return InfoResult(modes.DescribeMode(mode, model))
}

func handleModeSetAgent(ctx *Context) Result {
	return setInteractionMode(ctx, modes.ModeAgent)
}

func handleModeSetPlan(ctx *Context) Result {
	return setInteractionMode(ctx, modes.ModePlan)
}

func handleModeSetAsk(ctx *Context) Result {
	return setInteractionMode(ctx, modes.ModeAsk)
}

func handleModeSetDebug(ctx *Context) Result {
	return setInteractionMode(ctx, modes.ModeDebug)
}

func handleModeRoot(ctx *Context) Result {
	args := strings.TrimSpace(ctx.Args)
	if args == "" || strings.EqualFold(args, "show") {
		return handleModeShow(ctx)
	}
	parts := strings.Fields(args)
	head := strings.ToLower(parts[0])
	switch head {
	case "set":
		if len(parts) < 2 {
			return InfoResult("Usage: /mode set <agent|plan|ask|debug>")
		}
		mode, err := modes.ParseMode(parts[1])
		if err != nil {
			return InfoResult(err.Error())
		}
		return setInteractionMode(ctx, mode)
	case "agent", "plan", "ask", "debug":
		mode, err := modes.ParseMode(head)
		if err != nil {
			return InfoResult(err.Error())
		}
		return setInteractionMode(ctx, mode)
	default:
		mode, err := modes.ParseMode(head)
		if err != nil {
			return InfoResult("Unknown sub-command '" + head + "'. Expected: show, set <mode>, or agent | plan | ask | debug.")
		}
		return setInteractionMode(ctx, mode)
	}
}

func setInteractionMode(ctx *Context, mode modes.Mode) Result {
	if ctx.Deps.Hooks == nil {
		return InfoResult("Mode commands unavailable.")
	}
	model, err := ctx.Deps.Hooks.SetInteractionMode(ctx.Ctx, mode)
	if err != nil {
		return ErrorResult(err)
	}
	return InfoResult(fmt.Sprintf("Switched to %s. %s", mode.String(), modes.DescribeMode(mode, model)))
}
