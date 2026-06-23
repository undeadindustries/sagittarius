package slash

// statsCommand returns the /stats command: live session usage statistics.
// It mirrors the app exit screen without quitting, with session/model/tools
// subcommands. The plain-text rendering is produced by the agent layer via
// Hooks.SessionStatsText so this package stays free of UI imports.
func statsCommand() Command {
	return Command{
		Name:        "stats",
		Description: "Show session usage statistics. Usage: /stats [session|model|tools]",
		Handler:     statsHandler("session"),
		SubCommands: []Command{
			{
				Name:        "session",
				Description: "Show overall session statistics",
				Handler:     statsHandler("session"),
			},
			{
				Name:        "model",
				Description: "Show per-model token usage",
				Handler:     statsHandler("model"),
			},
			{
				Name:        "tools",
				Description: "Show tool-call statistics",
				Handler:     statsHandler("tools"),
			},
		},
	}
}

// statsHandler returns a handler that renders the given stats section, degrading
// gracefully when no Hooks are wired (e.g. tests without an agent).
func statsHandler(section string) func(*Context) Result {
	return func(ctx *Context) Result {
		if ctx.Deps.Hooks == nil {
			return InfoResult("Session statistics are not available.")
		}
		return InfoResult(ctx.Deps.Hooks.SessionStatsText(section))
	}
}
