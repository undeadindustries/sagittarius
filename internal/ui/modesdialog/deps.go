// Package modesdialog implements the /modes mode-override editor for the Bubble
// Tea TUI. It lists the four interaction modes and lets the user assign a
// {Provider}/{Model} override (from the global active list) or clear the override
// to fall back to the default resolution. All side effects go through Deps so
// the dialog never imports the agent or slash packages (preserves AD-004).
package modesdialog

import (
	"context"

	"github.com/undeadindustries/sagittarius/internal/config"
)

// ModelEntry is one row in the global active-model picker.
// When IsClear is true the entry represents "(use default)" and carries no
// provider/model — selecting it clears the mode override instead of setting one.
type ModelEntry struct {
	ProviderID string
	DisplayID  string
	Model      string
	IsClear    bool
}

// ModeEntry describes one interaction mode and its current override.
type ModeEntry struct {
	Mode     string // "agent", "plan", "ask", "debug"
	Provider string // "" = no override
	Model    string // "" = no override
}

// Deps performs the settings side effects the modes-override editor needs.
type Deps interface {
	// ListModes returns the four interaction modes with their current overrides
	// from the merged (effective) settings.
	ListModes() []ModeEntry
	// AllActiveModels returns all (provider, model) pairs for the picker.
	AllActiveModels() []ModelEntry
	// SetModeOverride persists a (provider, model) override for the given mode
	// name into the specified scope (Global or Project).
	SetModeOverride(ctx context.Context, mode, providerID, model string, scope config.SettingScope) error
	// ClearModeOverride removes the override for the given mode name from the
	// specified scope.
	ClearModeOverride(ctx context.Context, mode string, scope config.SettingScope) error
	// ProjectAvailable reports whether a project scope is writable (workDir
	// is set and is not the home directory). When false, the scope selector is
	// hidden and saves always target Global.
	ProjectAvailable() bool
}

// Resetter is an optional Deps extension. When the deps implement it, the
// dialog offers R (reset every row in the selected scope, two-step confirm).
// The /modes editor does not implement it; /subagents does.
type Resetter interface {
	ResetAllOverrides(ctx context.Context, scope config.SettingScope) (string, error)
}

// HeaderProvider is an optional Deps extension. When implemented, Header is
// rendered above the row list (e.g. current enablement state for /subagents).
type HeaderProvider interface {
	Header() string
}

// Options customizes the slot-override editor. The zero value reproduces the
// /modes editor exactly; /subagents passes its own title and noun.
type Options struct {
	// Title is the overlay title. Default "Mode Overrides".
	Title string
	// ItemNoun names one row in status lines ("mode" or "slot").
	// Default "mode".
	ItemNoun string
}

// DefaultOptions returns the /modes editor configuration.
func DefaultOptions() Options {
	return Options{Title: "Mode Overrides", ItemNoun: "mode"}
}

// SubagentsOptions returns the /subagents routing editor configuration.
func SubagentsOptions() Options {
	return Options{Title: "Subagent Routing", ItemNoun: "slot"}
}
