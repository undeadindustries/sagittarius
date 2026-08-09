package slash

import (
	"fmt"
	"strings"
)

// constraintsCommand manages standing session scope limits — user-stated
// restrictions (e.g. "just discuss this, don't change anything yet") that
// are injected into the system prompt with the highest precedence so they
// survive context compression and outlast the tool-invocation mandate's
// act-now bias. See internal/agent/constraints.go.
func constraintsCommand() Command {
	return Command{
		Name:        "constraints",
		Description: "Manage standing session scope limits (add/list/clear)",
		SubCommands: []Command{
			{
				Name:        "add",
				Description: "Add a standing constraint: /constraints add <text>",
				Handler:     handleConstraintsAdd,
			},
			{
				Name:        "list",
				Description: "List active standing constraints",
				Handler:     handleConstraintsList,
			},
			{
				Name:        "clear",
				Description: "Remove every standing constraint",
				Handler:     handleConstraintsClear,
			},
		},
	}
}

func handleConstraintsAdd(ctx *Context) Result {
	if ctx.Deps.Hooks == nil {
		return InfoResult("Constraints unavailable.")
	}
	text := strings.TrimSpace(ctx.Args)
	if text == "" {
		return InfoResult("Usage: /constraints add <text>")
	}
	if err := ctx.Deps.Hooks.AddConstraint(text); err != nil {
		return ErrorResult(fmt.Errorf("add constraint: %w", err))
	}
	return InfoResult(fmt.Sprintf("Added constraint: %s", text))
}

func handleConstraintsList(ctx *Context) Result {
	if ctx.Deps.Hooks == nil {
		return InfoResult("Constraints unavailable.")
	}
	constraints := ctx.Deps.Hooks.ListConstraints()
	if len(constraints) == 0 {
		return InfoResult("No standing constraints. Add one with /constraints add <text>.")
	}
	lines := make([]string, 0, len(constraints))
	for i, c := range constraints {
		lines = append(lines, fmt.Sprintf("%d. %s", i+1, c))
	}
	return InfoResult(strings.Join(lines, "\n"))
}

func handleConstraintsClear(ctx *Context) Result {
	if ctx.Deps.Hooks == nil {
		return InfoResult("Constraints unavailable.")
	}
	if err := ctx.Deps.Hooks.ClearConstraints(); err != nil {
		return ErrorResult(fmt.Errorf("clear constraints: %w", err))
	}
	return InfoResult("Cleared all standing constraints.")
}
