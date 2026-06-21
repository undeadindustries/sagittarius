// Package modelsdialog implements the interactive `/models` picker for the
// Bubble Tea TUI. It is scoped to the active provider: it lists that provider's
// activated models (curated via the /providers "Manage models" screen, with a
// fallback to the configured default model when uncurated) and lets the user
// pick which one is live. All side effects go through the Deps interface so the
// dialog never imports the agent or slash packages (preserves AD-004).
package modelsdialog

import "context"

// Deps performs the settings side effects the models picker needs.
// Implementations live in the agent layer (which owns the runner and loader).
type Deps interface {
	// ActiveProviderID returns the canonical active provider id (may be empty).
	ActiveProviderID() string
	// ActiveProviderLabel returns a short display label for the active provider.
	ActiveProviderLabel() string
	// ActiveModels returns the active provider's activated models, resolved with
	// the uncurated fallback (the configured default model). Empty means the
	// provider exposes no usable model yet.
	ActiveModels(id string) []string
	// CurrentModel returns the provider's currently-configured model.
	CurrentModel(id string) string
	// SetModel sets the active model for the provider and rebuilds the runner.
	SetModel(ctx context.Context, id, model string) error
}
