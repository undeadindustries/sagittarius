package slash

import "fmt"

func toolkitCommand() Command {
	return Command{
		Name:        "toolkit",
		Description: "Check host toolkit (compilers, linters, MCPs) and web capability",
		Handler: func(ctx *Context) Result {
			if len(ctx.Args) > 0 {
				return ErrorResult(fmt.Errorf("usage: /toolkit [dismiss]"))
			}
			report := ctx.Deps.Hooks.ToolkitReport()
			return Result{Handled: true, Scrollback: []ScrollbackEntry{{Text: report, Role: ScrollInfo}}}
		},
		SubCommands: []Command{
			{
				Name:        "dismiss",
				Description: "Permanently dismiss the automatic toolkit checklist on startup",
				Handler: func(ctx *Context) Result {
					if err := ctx.Deps.Hooks.ToolkitDismiss(); err != nil {
						return ErrorResult(err)
					}
					return Result{Handled: true, Scrollback: []ScrollbackEntry{{Text: "Toolkit checklist permanently dismissed.", Role: ScrollInfo}}}
				},
			},
		},
	}
}
