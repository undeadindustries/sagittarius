// Package modelsdialog implements the interactive `/models` picker for the
// Bubble Tea TUI. It is a global picker: it lists every activated model across
// all configured providers as "<provider> - <model>" rows (the activated set is
// curated per provider via the /providers "Manage models" screen) and lets the
// user select one, which switches the active provider and its live model in a
// single step. All side effects go through the Deps interface so the dialog
// never imports the agent or slash packages (preserves AD-004).
package modelsdialog

import "context"

// ModelEntry is one activated model belonging to a provider. The picker renders
// it as "<ProviderLabel> - <Model>".
type ModelEntry struct {
	ProviderID    string
	ProviderLabel string
	Model         string
}

// Deps performs the settings side effects the global models picker needs.
// Implementations live in the agent layer (which owns the runner and loader).
type Deps interface {
	// ListActiveModels returns every activated model across all configured
	// providers, for the global picker. Order is stable (provider then model).
	ListActiveModels() []ModelEntry
	// ActiveProviderID returns the canonical active provider id (may be empty).
	ActiveProviderID() string
	// CurrentModel returns the active provider's currently-configured model.
	CurrentModel() string
	// SelectModel switches the active provider to providerID (if it differs) and
	// sets its live model, rebuilding the runner.
	SelectModel(ctx context.Context, providerID, model string) error
}
