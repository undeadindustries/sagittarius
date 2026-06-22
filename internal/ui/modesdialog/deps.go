// Package modesdialog implements the /modes mode-override editor for the Bubble
// Tea TUI. It lists the four interaction modes and lets the user assign a
// {Provider}/{Model} override (from the global active list) or clear the override
// to fall back to the default resolution. All side effects go through Deps so
// the dialog never imports the agent or slash packages (preserves AD-004).
package modesdialog

import "context"

// ModelEntry is one row in the global active-model picker.
type ModelEntry struct {
	ProviderID  string
	DisplayID   string
	Model       string
}

// ModeEntry describes one interaction mode and its current override.
type ModeEntry struct {
	Mode     string // "agent", "plan", "ask", "debug"
	Provider string // "" = no override
	Model    string // "" = no override
}

// Deps performs the settings side effects the modes-override editor needs.
type Deps interface {
	// ListModes returns the four interaction modes with their current overrides.
	ListModes() []ModeEntry
	// AllActiveModels returns all (provider, model) pairs for the picker.
	AllActiveModels() []ModelEntry
	// SetModeOverride persists a (provider, model) override for the given mode name.
	SetModeOverride(ctx context.Context, mode, providerID, model string) error
	// ClearModeOverride removes the override for the given mode name.
	ClearModeOverride(ctx context.Context, mode string) error
}
