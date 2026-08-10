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
			{
				Name:        "settings",
				Description: "Edit mode overrides (alias for /modes)",
				Handler:     func(_ *Context) Result { return DialogResult(DialogModes) },
			},
		},
		Handler: handleModeRoot,
	}
}

// agentCommand, planCommand, askCommand, and debugCommand are top-level
// shortcuts for "/mode <name>" — the primary, high-frequency action (switch
// now) gets a one-word command; "/mode" remains for discovery/compat and
// "/modes" remains the per-mode override editor. They take no arguments and
// no subcommands: the mode name is the entire command.
func agentCommand() Command {
	return Command{
		Name:        "agent",
		Description: "Switch to agent mode (shortcut for /mode agent)",
		Handler:     noArgsModeHandler("agent", handleModeSetAgent),
	}
}

func planCommand() Command {
	return Command{
		Name:        "plan",
		Description: "Switch to plan mode (shortcut for /mode plan)",
		Handler:     noArgsModeHandler("plan", handleModeSetPlan),
	}
}

func askCommand() Command {
	return Command{
		Name:        "ask",
		Description: "Switch to ask mode (shortcut for /mode ask)",
		Handler:     noArgsModeHandler("ask", handleModeSetAsk),
	}
}

func debugCommand() Command {
	return Command{
		Name:        "debug",
		Description: "Switch to debug mode (shortcut for /mode debug)",
		Handler:     noArgsModeHandler("debug", handleModeSetDebug),
	}
}

// noArgsModeHandler wraps a mode-switch handler so the top-level shortcuts
// reject trailing arguments instead of silently ignoring them (e.g.
// "/agent reload" must not switch mode and drop "reload").
func noArgsModeHandler(name string, h func(*Context) Result) func(*Context) Result {
	return func(ctx *Context) Result {
		if strings.TrimSpace(ctx.Args) != "" {
			return InfoResult("Usage: /" + name)
		}
		return h(ctx)
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
