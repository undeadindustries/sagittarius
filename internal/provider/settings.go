package provider

import (
	"encoding/json"
	"fmt"
	"strconv"
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
	providerID = config.NormalizeProviderID(providerID)
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

// SetActiveModels persists the curated active-model set for providerID. Values
// are trimmed, empties dropped, and duplicates removed preserving order. An
// empty result clears the curation (back to the uncurated fallback).
func SetActiveModels(settings *config.Settings, providerID string, models []string) error {
	if settings == nil {
		return fmt.Errorf("set active models: settings are required")
	}
	id := config.NormalizeProviderID(providerID)
	cfg, err := ensureProviderInstance(settings, id)
	if err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(models))
	cleaned := make([]string, 0, len(models))
	for _, m := range models {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		if _, ok := seen[m]; ok {
			continue
		}
		seen[m] = struct{}{}
		cleaned = append(cleaned, m)
	}
	if len(cleaned) == 0 {
		cfg.ActiveModels = nil
	} else {
		cfg.ActiveModels = cleaned
	}
	return setProviderInstance(settings, id, cfg)
}

// CuratedActiveModels returns the explicitly-saved active-model set for
// providerID with no fallback. An empty result means the provider has not been
// curated yet (the activation screen checks every discovered model by default).
func CuratedActiveModels(settings *config.Settings, providerID string) []string {
	inst := providerInstance(settings, config.NormalizeProviderID(providerID))
	if inst == nil || len(inst.ActiveModels) == 0 {
		return nil
	}
	out := make([]string, len(inst.ActiveModels))
	copy(out, inst.ActiveModels)
	return out
}

// ActiveModelsFor returns the curated active-model set for providerID. When the
// provider has not been curated (no activeModels), it falls back to the single
// configured default model so /models is never empty for a usable provider.
func ActiveModelsFor(settings *config.Settings, providerID string) []string {
	id := config.NormalizeProviderID(providerID)
	if inst := providerInstance(settings, id); inst != nil && len(inst.ActiveModels) > 0 {
		out := make([]string, len(inst.ActiveModels))
		copy(out, inst.ActiveModels)
		return out
	}
	endpoint, err := ResolveEndpointForProvider(settings, id)
	if err != nil {
		return nil
	}
	if endpoint.Model == "" || endpoint.Model == "local-model" {
		return nil
	}
	return []string{endpoint.Model}
}

// InstanceSettingValues returns the currently-set provider instance overrides as
// display strings, keyed by setting name. Unset (nil/empty) fields are omitted.
// It is used by the providers wizard edit sheet to show current values.
func InstanceSettingValues(settings *config.Settings, providerID string) map[string]string {
	out := map[string]string{}
	inst := providerInstance(settings, config.NormalizeProviderID(providerID))
	if inst == nil {
		return out
	}
	if inst.Model != "" {
		out["model"] = inst.Model
	}
	if inst.BaseURL != "" {
		out["baseUrl"] = inst.BaseURL
	}
	if inst.PromptMode != "" {
		out["promptMode"] = string(inst.PromptMode)
	}
	if inst.ToolCallParsing != "" {
		out["toolCallParsing"] = string(inst.ToolCallParsing)
	}
	if inst.SystemPromptOverride != "" {
		out["systemPromptOverride"] = inst.SystemPromptOverride
	}
	if inst.ReasoningEffort != "" {
		out["reasoningEffort"] = inst.ReasoningEffort
	}
	if inst.ContextLimit != nil {
		out["contextLimit"] = strconv.Itoa(*inst.ContextLimit)
	}
	if inst.Timeout != nil {
		out["timeout"] = strconv.Itoa(*inst.Timeout)
	}
	if inst.CompressionThreshold != nil {
		out["compressionThreshold"] = strconv.FormatFloat(*inst.CompressionThreshold, 'g', -1, 64)
	}
	if inst.PreserveFraction != nil {
		out["preserveFraction"] = strconv.FormatFloat(*inst.PreserveFraction, 'g', -1, 64)
	}
	if inst.Temperature != nil {
		out["temperature"] = strconv.FormatFloat(*inst.Temperature, 'g', -1, 64)
	}
	if inst.ToolOutputMaskingProtectionFraction != nil {
		out["toolOutputMaskingProtectionFraction"] = strconv.FormatFloat(*inst.ToolOutputMaskingProtectionFraction, 'g', -1, 64)
	}
	if inst.ToolOutputMaskingPrunableFraction != nil {
		out["toolOutputMaskingPrunableFraction"] = strconv.FormatFloat(*inst.ToolOutputMaskingPrunableFraction, 'g', -1, 64)
	}
	if inst.EnableTools != nil {
		out["enableTools"] = strconv.FormatBool(*inst.EnableTools)
	}
	if inst.UseResponseChaining != nil {
		out["useResponseChaining"] = strconv.FormatBool(*inst.UseResponseChaining)
	}
	if inst.ToolOutputMaskingEnabled != nil {
		out["toolOutputMaskingEnabled"] = strconv.FormatBool(*inst.ToolOutputMaskingEnabled)
	}
	if inst.ToolOutputMaskingProtectLatestTurn != nil {
		out["toolOutputMaskingProtectLatestTurn"] = strconv.FormatBool(*inst.ToolOutputMaskingProtectLatestTurn)
	}
	return out
}

// customDefForProvider returns a pointer to the custom definition for providerID,
// or nil if providerID is a built-in or unknown.
func customDefForProvider(settings *config.Settings, providerID string) *config.CustomProviderDefinition {
	if settings == nil || settings.Providers == nil || settings.Providers.Custom == nil {
		return nil
	}
	if def, ok := settings.Providers.Custom[providerID]; ok {
		return &def
	}
	return nil
}

// ApplyProviderSetting validates key against the provider's wire-format allowlist
// (config.ValidSettingKeysForProvider) and applies the parsed value to the
// provider instance overrides. It returns a descriptive error for unknown keys or
// values that fail to parse for their typed field.
func ApplyProviderSetting(settings *config.Settings, providerID, key, value string) error {
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if key == "" {
		return fmt.Errorf("apply provider setting: key is required")
	}
	if settings == nil {
		return fmt.Errorf("apply provider setting: settings are required")
	}

	canonical := config.NormalizeProviderID(providerID)
	allowed := config.ValidSettingKeysForProvider(canonical, customDefForProvider(settings, canonical))
	if len(allowed) == 0 {
		return fmt.Errorf("provider %q exposes no editable settings", providerID)
	}
	if !containsString(allowed, key) {
		return fmt.Errorf("setting %q is not editable for provider %q (allowed: %s)", key, providerID, strings.Join(allowed, ", "))
	}

	cfg, err := ensureProviderInstance(settings, canonical)
	if err != nil {
		return err
	}
	if err := assignInstanceSetting(cfg, key, value); err != nil {
		return err
	}
	return setProviderInstance(settings, canonical, cfg)
}

func assignInstanceSetting(cfg *config.ProviderInstanceConfig, key, value string) error {
	switch key {
	case "model":
		cfg.Model = value
	case "baseUrl":
		cfg.BaseURL = value
	case "promptMode":
		mode := config.PromptMode(value)
		if mode != config.PromptModeLite && mode != config.PromptModeFull {
			return fmt.Errorf("promptMode must be %q or %q", config.PromptModeLite, config.PromptModeFull)
		}
		cfg.PromptMode = mode
	case "toolCallParsing":
		mode := config.ToolCallParsingMode(value)
		switch mode {
		case config.ToolCallParsingStrict, config.ToolCallParsingLenient, config.ToolCallParsingLoose:
			cfg.ToolCallParsing = mode
		default:
			return fmt.Errorf("toolCallParsing must be strict, lenient, or loose")
		}
	case "systemPromptOverride":
		cfg.SystemPromptOverride = value
	case "reasoningEffort":
		if !IsValidReasoningLevel(value) {
			return fmt.Errorf("reasoningEffort %q is not a valid level", value)
		}
		cfg.ReasoningEffort = value
	case "contextLimit":
		n, err := parseInt(key, value)
		if err != nil {
			return err
		}
		cfg.ContextLimit = n
	case "timeout":
		n, err := parseInt(key, value)
		if err != nil {
			return err
		}
		cfg.Timeout = n
	case "compressionThreshold":
		f, err := parseFloat(key, value)
		if err != nil {
			return err
		}
		cfg.CompressionThreshold = f
	case "preserveFraction":
		f, err := parseFloat(key, value)
		if err != nil {
			return err
		}
		cfg.PreserveFraction = f
	case "temperature":
		f, err := parseFloat(key, value)
		if err != nil {
			return err
		}
		cfg.Temperature = f
	case "toolOutputMaskingProtectionFraction":
		f, err := parseFloat(key, value)
		if err != nil {
			return err
		}
		cfg.ToolOutputMaskingProtectionFraction = f
	case "toolOutputMaskingPrunableFraction":
		f, err := parseFloat(key, value)
		if err != nil {
			return err
		}
		cfg.ToolOutputMaskingPrunableFraction = f
	case "enableTools":
		b, err := parseBool(key, value)
		if err != nil {
			return err
		}
		cfg.EnableTools = b
	case "useResponseChaining":
		b, err := parseBool(key, value)
		if err != nil {
			return err
		}
		cfg.UseResponseChaining = b
	case "toolOutputMaskingEnabled":
		b, err := parseBool(key, value)
		if err != nil {
			return err
		}
		cfg.ToolOutputMaskingEnabled = b
	case "toolOutputMaskingProtectLatestTurn":
		b, err := parseBool(key, value)
		if err != nil {
			return err
		}
		cfg.ToolOutputMaskingProtectLatestTurn = b
	default:
		return fmt.Errorf("apply provider setting: unsupported key %q", key)
	}
	return nil
}

// UpdateCustomProviderDefinition edits a definition-level field (displayName,
// baseUrl, wireFormat, apiKeyEnvVar) on providers.custom.<id>. Instance overrides
// (model, temperature, ...) go through ApplyProviderSetting instead.
func UpdateCustomProviderDefinition(settings *config.Settings, id, field, value string) error {
	id = strings.TrimSpace(id)
	field = strings.TrimSpace(field)
	value = strings.TrimSpace(value)
	if settings == nil || settings.Providers == nil || settings.Providers.Custom == nil {
		return fmt.Errorf("update custom provider: %q not found", id)
	}
	def, ok := settings.Providers.Custom[id]
	if !ok {
		return fmt.Errorf("update custom provider: %q not found", id)
	}
	switch field {
	case "displayName":
		if value == "" {
			value = id
		}
		def.DisplayName = value
	case "baseUrl":
		if value == "" {
			return fmt.Errorf("update custom provider: baseUrl is required")
		}
		def.BaseURL = value
	case "wireFormat":
		wf := config.WireFormat(value)
		if wf != config.WireFormatOpenAIChat && wf != config.WireFormatOpenAIResponses {
			return fmt.Errorf("wireFormat must be %q or %q", config.WireFormatOpenAIChat, config.WireFormatOpenAIResponses)
		}
		def.WireFormat = wf
	case "apiKeyEnvVar":
		def.APIKeyEnvVar = value
	case "defaultModel":
		def.DefaultModel = value
	default:
		return fmt.Errorf("update custom provider: unsupported field %q", field)
	}
	settings.Providers.Custom[id] = def
	return nil
}

func parseInt(key, value string) (*int, error) {
	n, err := strconv.Atoi(value)
	if err != nil {
		return nil, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return &n, nil
}

func parseFloat(key, value string) (*float64, error) {
	f, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return nil, fmt.Errorf("%s must be a number: %w", key, err)
	}
	return &f, nil
}

func parseBool(key, value string) (*bool, error) {
	b, err := strconv.ParseBool(value)
	if err != nil {
		return nil, fmt.Errorf("%s must be true or false: %w", key, err)
	}
	return &b, nil
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
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
