// Package modelsdialog implements the /models per-model settings editor for the
// Bubble Tea TUI. It lists every activated (Provider/Model) pair globally; the
// user selects one and edits per-model overrides (temperature, context limit,
// reasoning effort) in a submenu. System prompt is project-scoped via
// /system-prompt, not per-model. All side effects go through Deps so the
// dialog never imports the agent or slash packages (preserves AD-004).
package modelsdialog

import "context"

// ModelEntry is one row in the global active-model list.
type ModelEntry struct {
	ProviderID    string
	ProviderLabel string // short display id (e.g. "gemini")
	Model         string
}

// Deps performs the settings side effects the per-model settings editor needs.
type Deps interface {
	// ListAllActiveModels returns every (provider, model) pair across all providers.
	ListAllActiveModels() []ModelEntry
	// GetModelSettings returns the current per-model override values for display
	// (keyed by "temperature", "contextLimit", "reasoningEffort"). Unset fields
	// are omitted.
	GetModelSettings(providerID, model string) map[string]string
	// SetModelSetting applies a per-model setting override.
	SetModelSetting(ctx context.Context, providerID, model, key, value string) error
	// ClearModelSetting removes a per-model override so it inherits the provider default.
	ClearModelSetting(ctx context.Context, providerID, model, key string) error
}
