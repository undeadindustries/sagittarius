package agent

import (
	"context"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

// App adapts Runner to ui.App for interactive TUI sessions.
type App struct {
	runner *Runner
	status ui.StatusBar
}

// NewApp wraps runner for interactive use and exposes footer metadata.
func NewApp(runner *Runner, providerLabel, model string) *App {
	return &App{
		runner: runner,
		status: ui.StatusBar{
			Left:  providerLabel,
			Right: model,
		},
	}
}

// HandleInput implements ui.App by delegating to the agent runner.
func (a *App) HandleInput(ctx context.Context, input string) (<-chan ui.StreamEvent, error) {
	return a.runner.RunTurn(ctx, input)
}

// Status returns footer metadata for the TUI status bar.
func (a *App) Status() ui.StatusBar {
	return a.status
}
