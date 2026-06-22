// Package providersdialog implements the interactive `/providers` management
// wizard for the Bubble Tea TUI. It is a self-contained overlay model that the
// main TUI embeds; all settings/credential side effects go through the Deps
// interface so the dialog never imports the agent or slash packages directly
// (preserves the AD-004 UI-library boundary in the other direction).
package providersdialog

import (
	"context"

	"github.com/undeadindustries/sagittarius/internal/config"
)

// ProviderEntry is a row in the provider lists (switch / edit-pick / list).
type ProviderEntry struct {
	ID          string // canonical settings.json id (e.g. gemini-apikey)
	DisplayID   string // short id shown to the user (e.g. gemini)
	DisplayName string
	WireFormat  config.WireFormat
	IsCustom    bool
	IsActive    bool
	Model       string
}

// Deps performs the settings and credential side effects the dialog needs.
// Implementations live in the agent layer (which owns the runner, loader, and
// credential store). All mutating methods persist settings and rebuild the
// active runner when the change affects the active provider.
type Deps interface {
	// ListProviders returns every configured provider (built-in + custom).
	ListProviders() []ProviderEntry
	// ActiveProviderID returns the canonical active provider id (may be empty).
	ActiveProviderID() string
	// SwitchProvider sets the active provider and rebuilds the runner.
	SwitchProvider(ctx context.Context, id string) error
	// SetAPIKey stores an API key for a provider in the OS keychain / fallback.
	SetAPIKey(ctx context.Context, id, key string) error
	// AddCustomProvider registers a custom provider; apiKey is optional.
	AddCustomProvider(ctx context.Context, id string, def config.CustomProviderDefinition, apiKey string) error
	// RemoveCustomProvider deletes a custom provider definition.
	RemoveCustomProvider(ctx context.Context, id string) error
	// DiscoverModels lists chat models from a provider endpoint (network call).
	DiscoverModels(ctx context.Context, id string) ([]string, error)
	// SetModel sets the active model for a provider (allowlist-free, like /model).
	SetModel(ctx context.Context, id, model string) error
	// CurrentModel returns the provider's resolved live/default model id (empty
	// when it cannot be resolved). Used to keep the live model inside the curated
	// active set when saving the activation screen.
	CurrentModel(id string) string
	// ApplySetting validates and applies a provider instance setting.
	ApplySetting(ctx context.Context, id, key, value string) error
	// UpdateCustomDefinition edits a custom provider definition field.
	UpdateCustomDefinition(ctx context.Context, id, field, value string) error
	// ProviderSettings returns the current instance setting values for display.
	ProviderSettings(id string) map[string]string
	// ValidSettingKeys returns the editable instance keys for a provider.
	ValidSettingKeys(id string) []string
	// ActiveModels returns the curated active-model set for a provider (the raw
	// saved set, no fallback). Empty means the provider is not yet curated, in
	// which case the activation screen checks every discovered model by default.
	ActiveModels(id string) []string
	// SetActiveModels persists the curated active-model set for a provider.
	SetActiveModels(ctx context.Context, id string, models []string) error
	// EffectiveProviderSettings returns resolved display values (defaults +
	// overrides) for the edit sheet, keyed by setting name. Keys without a
	// computed default are omitted.
	EffectiveProviderSettings(id string) map[string]string
	// SystemPromptPresetID returns the preset id matching the provider's current
	// personality + variant ("" when it matches no preset).
	SystemPromptPresetID(id string) string
	// ApplySystemPromptPreset sets the provider's personality + variant from a
	// preset and returns an info line describing the suggested sampling defaults.
	ApplySystemPromptPreset(ctx context.Context, id, presetID string) (string, error)
	// ClearSetting removes a single instance override so it falls back to default.
	ClearSetting(ctx context.Context, id, key string) error
	// ResetSettings clears all behavioral instance overrides for a provider,
	// preserving model/baseUrl/wireFormat and the curated active-model set.
	ResetSettings(ctx context.Context, id string) error
}
