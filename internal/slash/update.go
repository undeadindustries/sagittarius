package slash

import (
	"fmt"
	"strings"
)

func updateCommand() Command {
	return Command{
		Name:        "update",
		Description: "Check for Sagittarius updates",
		SubCommands: []Command{
			{
				Name:        "install",
				Description: "Download and install the latest update",
				Handler: func(ctx *Context) Result {
					res, err := ctx.Deps.Hooks.InstallUpdate(ctx.Ctx)
					if err != nil {
						return Result{Handled: true, Err: fmt.Errorf("update failed: %w", err)}
					}
					if res == nil {
						return Result{Handled: true, Err: fmt.Errorf("update not available or skipped")}
					}
					return Result{
						Handled:  true,
						Messages: []string{fmt.Sprintf("Installed %s. Restart sagittarius to use it.", res.Version)},
					}
				},
			},
		},
		Handler: func(ctx *Context) Result {
			args := strings.TrimSpace(ctx.Args)
			if args != "" {
				return Result{Handled: true, Err: fmt.Errorf("unknown subcommand: %s. Use /update or /update install", args)}
			}

			res, err := ctx.Deps.Hooks.CheckForUpdate(ctx.Ctx, true)
			if err != nil {
				return Result{Handled: true, Err: fmt.Errorf("update check failed: %w", err)}
			}
			if res == nil {
				return Result{Handled: true, Messages: []string{"Update check skipped (running dev build)"}}
			}
			if !res.Available {
				return Result{Handled: true, Messages: []string{fmt.Sprintf("Sagittarius is up to date (current: %s)", res.Current)}}
			}

			return Result{
				Handled:  true,
				Messages: []string{fmt.Sprintf("Update available: %s -> %s. Run /update install to upgrade.", res.Current, res.Latest)},
			}
		},
	}
}
