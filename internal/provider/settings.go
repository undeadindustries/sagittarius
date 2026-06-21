package provider

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/config"
)

// IsOpenAIChatMode reports whether the active provider uses openai-chat wire format.
// Phase 11 context layers key off this hook (fork isLocalMode semantics).
func IsOpenAIChatMode(settings *config.Settings) bool {
	endpoint, err := ResolveEndpointConfig(settings)
	if err != nil {
		return false
	}
	return endpoint.WireFormat == config.WireFormatOpenAIChat
}

// IsOpenAIResponsesMode reports whether the active provider uses openai-responses wire format.
func IsOpenAIResponsesMode(settings *config.Settings) bool {
	endpoint, err := ResolveEndpointConfig(settings)
	if err != nil {
		return false
	}
	return endpoint.WireFormat == config.WireFormatOpenAIResponses
}

// SetActiveProvider updates providers.active to providerID.
func SetActiveProvider(settings *config.Settings, providerID string) error {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return fmt.Errorf("set active provider: id is required")
	}
	if settings == nil {
		return fmt.Errorf("set active provider: settings are required")
	}
	if settings.Providers == nil {
		settings.Providers = &config.ProvidersSettings{}
	}
	if _, ok := config.LookupBuiltInProvider(providerID); !ok {
		if settings.Providers.Custom == nil {
			return fmt.Errorf("set active provider: unknown provider %q", providerID)
		}
		if _, ok := settings.Providers.Custom[providerID]; !ok {
			return fmt.Errorf("set active provider: unknown provider %q", providerID)
		}
	}
	settings.Providers.Active = providerID
	return nil
}

// SaveActiveProvider sets the active provider and persists settings via loader.
//
// Switching providers invalidates session state scoped to the previous backend:
// the Responses API previous_response_id (a chained id is meaningless to another
// endpoint) and the session-only reasoning override. Both are cleared on a
// successful switch, matching the fork.
func SaveActiveProvider(loader *config.Loader, settings *config.Settings, providerID string) error {
	if loader == nil {
		return fmt.Errorf("save active provider: loader is required")
	}
	if err := SetActiveProvider(settings, providerID); err != nil {
		return err
	}
	if err := loader.Save(settings); err != nil {
		return err
	}
	ClearLastResponseID()
	ClearSessionReasoningOverride()
	return nil
}

// SetProviderModel updates the model override for providerID.
func SetProviderModel(settings *config.Settings, providerID, model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return fmt.Errorf("set provider model: model is required")
	}
	if settings == nil {
		return fmt.Errorf("set provider model: settings are required")
	}
	cfg, err := ensureProviderInstance(settings, providerID)
	if err != nil {
		return err
	}
	cfg.Model = model
	return setProviderInstance(settings, providerID, cfg)
}

// SetProviderField updates a single provider instance field (model or baseUrl).
func SetProviderField(settings *config.Settings, providerID, field, value string) error {
	field = strings.TrimSpace(field)
	value = strings.TrimSpace(value)
	if field == "" {
		return fmt.Errorf("set provider field: field is required")
	}
	if settings == nil {
		return fmt.Errorf("set provider field: settings are required")
	}
	cfg, err := ensureProviderInstance(settings, providerID)
	if err != nil {
		return err
	}
	switch field {
	case "model":
		cfg.Model = value
	case "baseUrl":
		cfg.BaseURL = value
	default:
		return fmt.Errorf("set provider field: unsupported field %q", field)
	}
	return setProviderInstance(settings, providerID, cfg)
}

// AddCustomProvider registers a user-defined OpenAI-compatible provider.
func AddCustomProvider(settings *config.Settings, id string, def config.CustomProviderDefinition) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("add custom provider: id is required")
	}
	if strings.TrimSpace(def.BaseURL) == "" {
		return fmt.Errorf("add custom provider: baseUrl is required")
	}
	if settings == nil {
		return fmt.Errorf("add custom provider: settings are required")
	}
	if settings.Providers == nil {
		settings.Providers = &config.ProvidersSettings{}
	}
	if _, ok := config.LookupBuiltInProvider(id); ok {
		return fmt.Errorf("add custom provider: id %q conflicts with built-in provider", id)
	}
	if settings.Providers.Custom == nil {
		settings.Providers.Custom = make(map[string]config.CustomProviderDefinition)
	}
	if _, exists := settings.Providers.Custom[id]; exists {
		return fmt.Errorf("add custom provider: %q already exists", id)
	}
	if def.DisplayName == "" {
		def.DisplayName = id
	}
	if def.WireFormat == "" {
		def.WireFormat = config.WireFormatOpenAIChat
	}
	settings.Providers.Custom[id] = def
	return nil
}

// RemoveCustomProvider deletes a custom provider entry.
func RemoveCustomProvider(settings *config.Settings, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("remove custom provider: id is required")
	}
	if settings == nil || settings.Providers == nil || settings.Providers.Custom == nil {
		return fmt.Errorf("remove custom provider: %q not found", id)
	}
	if _, ok := settings.Providers.Custom[id]; !ok {
		return fmt.Errorf("remove custom provider: %q not found", id)
	}
	delete(settings.Providers.Custom, id)
	if settings.Providers.Active == id {
		settings.Providers.Active = ""
	}
	return nil
}

func ensureProviderInstance(settings *config.Settings, providerID string) (*config.ProviderInstanceConfig, error) {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return nil, fmt.Errorf("provider instance: id is required")
	}
	if _, ok := config.LookupBuiltInProvider(providerID); !ok {
		if settings.Providers == nil || settings.Providers.Custom == nil {
			return nil, fmt.Errorf("provider instance: unknown provider %q", providerID)
		}
		if _, ok := settings.Providers.Custom[providerID]; !ok {
			return nil, fmt.Errorf("provider instance: unknown provider %q", providerID)
		}
	}
	if settings.Providers == nil {
		settings.Providers = &config.ProvidersSettings{}
	}
	inst := providerInstance(settings, providerID)
	if inst != nil {
		copy := *inst
		return &copy, nil
	}
	return &config.ProviderInstanceConfig{}, nil
}

func setProviderInstance(settings *config.Settings, providerID string, cfg *config.ProviderInstanceConfig) error {
	if settings.Providers == nil {
		settings.Providers = &config.ProvidersSettings{}
	}
	switch providerID {
	case string(config.BuiltInOpenAI):
		settings.Providers.OpenAI = cfg
	case string(config.BuiltInGeminiAPIKey):
		settings.Providers.GeminiAPIKey = cfg
	case string(config.BuiltInOpenAIResponses):
		settings.Providers.OpenAIResponses = cfg
	default:
		if settings.Providers.Extra == nil {
			settings.Providers.Extra = make(map[string]json.RawMessage)
		}
		raw, err := json.Marshal(cfg)
		if err != nil {
			return fmt.Errorf("marshal provider instance: %w", err)
		}
		settings.Providers.Extra[providerID] = raw
	}
	return nil
}

// SetProviderReasoningEffort persists reasoningEffort for providerID.
func SetProviderReasoningEffort(settings *config.Settings, providerID, level string) error {
	level = strings.TrimSpace(level)
	if level == "" {
		return fmt.Errorf("set provider reasoning effort: level is required")
	}
	if !IsValidReasoningLevel(level) {
		return fmt.Errorf("set provider reasoning effort: unknown level %q", level)
	}
	if settings == nil {
		return fmt.Errorf("set provider reasoning effort: settings are required")
	}
	cfg, err := ensureProviderInstance(settings, providerID)
	if err != nil {
		return err
	}
	cfg.ReasoningEffort = level
	return setProviderInstance(settings, providerID, cfg)
}

// EffectiveProviderSummary describes the active provider for slash commands.
type EffectiveProviderSummary struct {
	ProviderID      string
	DisplayName     string
	WireFormat      config.WireFormat
	ReasoningEffort string
}

// EffectiveProvider returns metadata for the active provider.
func EffectiveProvider(settings *config.Settings) (EffectiveProviderSummary, error) {
	if settings == nil {
		return EffectiveProviderSummary{}, fmt.Errorf("effective provider: settings are required")
	}
	endpoint, err := ResolveEndpointConfig(settings)
	if err != nil {
		return EffectiveProviderSummary{}, err
	}
	summary := EffectiveProviderSummary{
		ProviderID:      endpoint.ProviderID,
		WireFormat:      endpoint.WireFormat,
		ReasoningEffort: endpoint.ReasoningEffort,
	}
	if def, ok := config.LookupBuiltInProvider(endpoint.ProviderID); ok {
		summary.DisplayName = def.DisplayName
	} else if settings.Providers != nil {
		if custom, ok := settings.Providers.Custom[endpoint.ProviderID]; ok {
			summary.DisplayName = custom.DisplayName
			if summary.DisplayName == "" {
				summary.DisplayName = endpoint.ProviderID
			}
		}
	}
	if summary.DisplayName == "" {
		summary.DisplayName = endpoint.ProviderID
	}
	return summary, nil
}
