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
//
// Reads return the merged (global + project) view. Preference writes take the
// scope the user picked; API keys, custom provider definitions, and the
// connection fields (baseUrl, wireFormat) are always global, because a project
// file must not redirect where requests and keys go.
type Deps interface {
	// ListProviders returns every configured provider (built-in + custom).
	ListProviders() []ProviderEntry
	// ActiveProviderID returns the canonical active provider id (may be empty).
	ActiveProviderID() string
	// ProjectAvailable reports whether the session has a project settings file
	// to save to; when false the dialog hides the scope switch.
	ProjectAvailable() bool
	// SwitchProvider sets the active provider in scope and rebuilds the runner.
	SwitchProvider(ctx context.Context, id string, scope config.SettingScope) error
	// SetAPIKey stores an API key for a provider in the OS keychain / fallback.
	SetAPIKey(ctx context.Context, id, key string) error
	// AddCustomProvider registers a custom provider; apiKey is optional.
	AddCustomProvider(ctx context.Context, id string, def config.CustomProviderDefinition, apiKey string) error
	// RemoveCustomProvider deletes a custom provider definition.
	RemoveCustomProvider(ctx context.Context, id string) error
	// DiscoverModels lists chat models from a provider endpoint (network call).
	DiscoverModels(ctx context.Context, id string) ([]string, error)
	// SetModel sets the active model for a provider in scope.
	SetModel(ctx context.Context, id, model string, scope config.SettingScope) error
	// CurrentModel returns the provider's resolved live/default model id (empty
	// when it cannot be resolved). Used to keep the live model inside the curated
	// active set when saving the activation screen.
	CurrentModel(id string) string
	// ApplySetting validates and applies a provider instance setting in scope.
	// Connection keys are written globally whatever scope is passed.
	ApplySetting(ctx context.Context, id, key, value string, scope config.SettingScope) error
	// UpdateCustomDefinition edits a custom provider definition field (global).
	UpdateCustomDefinition(ctx context.Context, id, field, value string) error
	// ProviderSettings returns the instance values written in scope's file, so
	// the edit sheet can mark what is set there. Custom definition fields and
	// connection keys always reflect the global file.
	ProviderSettings(id string, scope config.SettingScope) map[string]string
	// ValidSettingKeys returns the editable instance keys for a provider.
	ValidSettingKeys(id string) []string
	// ActiveModels returns the curated active-model set for a provider (the raw
	// saved set, no fallback). Empty means the provider is not yet curated, in
	// which case the activation screen checks every discovered model by default.
	ActiveModels(id string) []string
	// SetActiveModels persists the curated active-model set in scope.
	SetActiveModels(ctx context.Context, id string, models []string, scope config.SettingScope) error
	// EffectiveProviderSettings returns resolved display values (defaults +
	// overrides) for the edit sheet, keyed by setting name. Keys without a
	// computed default are omitted.
	EffectiveProviderSettings(id string) map[string]string
	// SystemPromptPresetID returns the preset id matching the provider's current
	// personality + variant ("" when it matches no preset).
	SystemPromptPresetID(id string) string
	// ApplySystemPromptPreset sets the provider's personality + variant from a
	// preset in scope and returns an info line describing the suggested
	// sampling defaults.
	ApplySystemPromptPreset(ctx context.Context, id, presetID string, scope config.SettingScope) (string, error)
	// ClearSetting removes a single instance override from scope.
	ClearSetting(ctx context.Context, id, key string, scope config.SettingScope) error
	// ResetSettings clears all behavioral instance overrides for a provider in
	// scope, preserving model/baseUrl/wireFormat and the curated active set.
	ResetSettings(ctx context.Context, id string, scope config.SettingScope) error
	// GenerateProviderID returns a collision-free provider id auto-derived from
	// the given base URL. Used by the add wizard to suggest an id.
	GenerateProviderID(baseURL string) string
}
