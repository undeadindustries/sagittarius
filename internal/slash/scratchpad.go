package slash

import (
	"fmt"
)

// scratchpadCommand inspects and wipes the model's working-memory note. There is
// deliberately no `set` subcommand: user-authored standing text is
// /constraints. The scratchpad block is framed to the model as its own notes, so
// letting the user write into it would blur the one distinction that keeps the
// model from reading it as a directive. See internal/agent/scratchpad.go.
func scratchpadCommand() Command {
	return Command{
		Name:        "scratchpad",
		Description: "Show or clear the model's working-memory note (show/clear)",
		SubCommands: []Command{
			{
				Name:        "show",
				Description: "Show the current working-memory note",
				Handler:     handleScratchpadShow,
			},
			{
				Name:        "clear",
				Description: "Discard the working-memory note",
				Handler:     handleScratchpadClear,
			},
		},
		Handler: handleScratchpadShow,
	}
}

func handleScratchpadShow(ctx *Context) Result {
	if ctx.Deps.Hooks == nil {
		return InfoResult("Scratchpad unavailable.")
	}
	note := ctx.Deps.Hooks.Scratchpad()
	if note == "" {
		return InfoResult("The scratchpad is empty. The model writes to it with update_scratchpad.")
	}
	return InfoResult(fmt.Sprintf("Scratchpad (%d characters):\n%s", len([]rune(note)), note))
}

func handleScratchpadClear(ctx *Context) Result {
	if ctx.Deps.Hooks == nil {
		return InfoResult("Scratchpad unavailable.")
	}
	if ctx.Deps.Hooks.Scratchpad() == "" {
		return InfoResult("The scratchpad is already empty.")
	}
	if err := ctx.Deps.Hooks.ClearScratchpad(); err != nil {
		return ErrorResult(fmt.Errorf("clear scratchpad: %w", err))
	}
	return InfoResult("Scratchpad cleared. It is gone from the next request's system prompt.")
}
