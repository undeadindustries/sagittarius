// Package modelpickdialog implements the /model picker overlay: a global
// {Provider}/{Model} list spanning all providers' activated models. Selecting
// an entry calls SelectCurrentModel which atomically switches the active
// provider and model and rebuilds the runner. All side effects go through Deps
// so the dialog never imports the agent or slash packages (preserves AD-004).
package modelpickdialog

import (
	"context"

	"github.com/undeadindustries/sagittarius/internal/config"
)

// ModelEntry is one row in the global active-model list.
type ModelEntry struct {
	ProviderID  string
	DisplayID   string // short provider label shown in the list
	DisplayName string // full provider display name
	Model       string
}

// Deps performs the side effects the global model picker needs.
type Deps interface {
	// AllActiveModels returns every (provider, model) pair across all providers.
	AllActiveModels() []ModelEntry
	// CurrentProviderID returns the canonical id of the currently-active provider.
	CurrentProviderID() string
	// CurrentModel returns the currently-configured model for the active provider.
	CurrentModel() string
	// SelectCurrentModel switches to the given (provider, model), saves to the
	// specified scope, and rebuilds the runner.
	SelectCurrentModel(ctx context.Context, providerID, model string, scope config.SettingScope) error
	// ProjectAvailable reports whether the project scope is writable.
	ProjectAvailable() bool
}
