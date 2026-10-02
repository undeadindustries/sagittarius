package slash

import (
	"errors"
	"fmt"
	"strings"
)

func exportCommand() Command {
	return Command{
		Name:        "export",
		Description: "Export session data (e.g. /export atif)",
		SubCommands: []Command{
			{
				Name:        "atif",
				Description: "Export the current session as a Harbor ATIF v1.7 trajectory JSON (/export atif [path])",
				Handler:     handleExportATIF,
			},
		},
		Handler: func(ctx *Context) Result {
			args := strings.TrimSpace(ctx.Args)
			if args == "" {
				return ErrorResult(errors.New("usage: /export atif [path]"))
			}
			parts := strings.Fields(args)
			if parts[0] == "atif" {
				path := ""
				if len(parts) > 1 {
					path = parts[1]
				}
				ctx.Args = path
				return handleExportATIF(ctx)
			}
			return ErrorResult(fmt.Errorf("unknown export format %q (available: atif)", parts[0]))
		},
	}
}

func handleExportATIF(ctx *Context) Result {
	if ctx.Deps.Hooks == nil {
		return ErrorResult(errors.New("hooks unavailable"))
	}
	outPath := strings.TrimSpace(ctx.Args)
	dest, err := ctx.Deps.Hooks.ExportATIF(outPath)
	if err != nil {
		return ErrorResult(err)
	}
	return InfoResult(fmt.Sprintf("Exported ATIF trajectory to %s", dest))
}
