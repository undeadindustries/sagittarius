package slash

import (
	"fmt"
	"strings"
)

func subagentsCommand() Command {
	return Command{
		Name:        "subagents",
		Description: "Route subagents to a {Provider}/{Model} pair per slot (default, research, coding, reviewer, utility) — opens an interactive menu",
		SubCommands: []Command{
			{
				Name:        "show",
				Description: "List the five routing slots with their current pins",
				Handler:     handleSubagentsShow,
			},
			{
				Name:        "set",
				Hidden:      true, // headless/scripting; bare /subagents opens the interactive editor
				Description: "Pin a routing slot headlessly (scope: global|project; default project)",
				Handler:     handleSubagentsSet,
			},
			{
				Name:        "clear",
				Hidden:      true,
				Description: "Clear a routing slot pin headlessly",
				Handler:     handleSubagentsClear,
			},
			{
				Name:        "reset",
				Hidden:      true,
				Description: "Clear every routing pin in scope headlessly",
				Handler:     handleSubagentsReset,
			},
		},
		Handler: handleSubagents,
	}
}

// handleSubagents opens the subagent routing editor dialog.
func handleSubagents(_ *Context) Result {
	return DialogResult(DialogSubagents)
}

func handleSubagentsShow(ctx *Context) Result {
	if ctx.Deps.Hooks == nil {
		return InfoResult("Subagent routing unavailable.")
	}
	return InfoResult(ctx.Deps.Hooks.SubagentRoutingText())
}

// handleSubagentsSet pins a routing slot headlessly:
//
//	/subagents set <default|research|coding|reviewer|utility> <Provider/Model> [global|project]
func handleSubagentsSet(ctx *Context) Result {
	if ctx.Deps.Hooks == nil {
		return InfoResult("Subagent routing unavailable.")
	}
	parts := strings.Fields(strings.TrimSpace(ctx.Args))
	if len(parts) < 2 {
		// Incomplete headless invocation; open the interactive editor instead
		// of printing usage.
		return DialogResult(DialogSubagents)
	}
	slot := strings.ToLower(parts[0])
	pair := parts[1]
	scope, err := trailingScope(parts[2:])
	if err != nil {
		return InfoResult(err.Error())
	}
	slash := strings.SplitN(pair, "/", 2)
	if len(slash) != 2 || slash[0] == "" || slash[1] == "" {
		return InfoResult("Model must be in Provider/Model format, e.g. openrouter/qwen/qwen3-235b-a22b")
	}
	if err := ctx.Deps.Hooks.SetSubagentOverride(ctx.Ctx, slot, slash[0], slash[1], scope); err != nil {
		return ErrorResult(err)
	}
	return InfoResult(fmt.Sprintf("Subagent slot %s → %s (saved to %s settings)", slot, pair, scope))
}

// handleSubagentsClear clears a routing slot pin headlessly:
//
//	/subagents clear <default|research|coding|reviewer|utility> [global|project]
func handleSubagentsClear(ctx *Context) Result {
	if ctx.Deps.Hooks == nil {
		return InfoResult("Subagent routing unavailable.")
	}
	parts := strings.Fields(strings.TrimSpace(ctx.Args))
	if len(parts) < 1 {
		return DialogResult(DialogSubagents)
	}
	slot := strings.ToLower(parts[0])
	scope, err := trailingScope(parts[1:])
	if err != nil {
		return InfoResult(err.Error())
	}
	if err := ctx.Deps.Hooks.SetSubagentOverride(ctx.Ctx, slot, "", "", scope); err != nil {
		return ErrorResult(err)
	}
	return InfoResult(fmt.Sprintf("Subagent slot %s cleared from %s settings", slot, scope))
}

// handleSubagentsReset clears every routing pin headlessly:
//
//	/subagents reset [global|project]
func handleSubagentsReset(ctx *Context) Result {
	if ctx.Deps.Hooks == nil {
		return InfoResult("Subagent routing unavailable.")
	}
	parts := strings.Fields(strings.TrimSpace(ctx.Args))
	scope, err := trailingScope(parts)
	if err != nil {
		return InfoResult(err.Error())
	}
	msg, err := ctx.Deps.Hooks.ResetSubagentOverrides(ctx.Ctx, scope)
	if err != nil {
		return ErrorResult(err)
	}
	return InfoResult(msg)
}
