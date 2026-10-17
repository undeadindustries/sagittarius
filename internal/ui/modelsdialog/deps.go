// Package modelsdialog implements the /models per-model settings editor for the
// Bubble Tea TUI. It lists every activated (Provider/Model) pair globally; the
// user selects one and edits per-model overrides (temperature, context limit,
// reasoning effort) in a submenu. System prompt is project-scoped via
// /system-prompt, not per-model. All side effects go through Deps so the
// dialog never imports the agent or slash packages (preserves AD-004).
package modelsdialog

import (
	"context"

	"github.com/undeadindustries/sagittarius/internal/config"
)

// ModelEntry is one row in the global active-model list.
type ModelEntry struct {
	ProviderID    string
	ProviderLabel string // short display id (e.g. "gemini")
	Model         string
}

// Deps performs the settings side effects the per-model settings editor needs.
// Writes take the scope the user picked so a per-project tweak never leaks into
// every other project.
type Deps interface {
	// ListAllActiveModels returns every (provider, model) pair across all providers.
	ListAllActiveModels() []ModelEntry
	// ProjectAvailable reports whether the session has a project settings file
	// to save to; when false the dialog hides the scope switch.
	ProjectAvailable() bool
	// GetModelSettings returns the per-model override values written in
	// scope's file (keyed by "temperature", "contextLimit", ...). Unset fields
	// are omitted, so the dialog can tell "set here" from inherited.
	GetModelSettings(providerID, model string, scope config.SettingScope) map[string]string
	// SetModelSetting applies a per-model setting override in scope.
	SetModelSetting(ctx context.Context, providerID, model, key, value string, scope config.SettingScope) error
	// ClearModelSetting removes a per-model override from scope so it inherits.
	ClearModelSetting(ctx context.Context, providerID, model, key string, scope config.SettingScope) error
	// ReasoningCapabilityHint returns a one-line, read-only description of the
	// model's resolved reasoning mechanism (e.g. "Adaptive (dynamic thinking)
	// — Gemini decides depth per turn.", "Fixed effort — none, low, medium,
	// high (default: none)", "Not supported for this model."), sourced from
	// the same resolver /reasoning show uses. Empty means no hint to display.
	ReasoningCapabilityHint(providerID, model string) string
	// ReasoningOptions returns the effort levels this model is known to accept,
	// its advertised default, and whether capability is known. When known is
	// false the picker offers only "default (inherit)" plus an unverified
	// custom entry.
	ReasoningOptions(providerID, model string) (efforts []string, defaultEffort string, known bool)
}
