package provider

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/undeadindustries/sagittarius/internal/config"
)

const defaultOpenAITimeout = 120 * time.Second

// EndpointConfig holds resolved connection parameters for a provider endpoint.
type EndpointConfig struct {
	ProviderID           string
	BaseURL              string
	Model                string
	WireFormat           config.WireFormat
	Timeout              time.Duration
	Bearer               string
	RequiresAPIKey       bool
	ToolCallParsing      config.ToolCallParsingMode
	ReasoningEffort      string
	UseResponseChaining  bool
	Temperature          *float64
	SystemPromptOverride string
	ToolsEnabled         bool
}

// ExtractServerRoot normalizes a provider URL to a server root suitable for
// appending /v1/models, /v1/chat/completions, or /v1/responses.
func ExtractServerRoot(rawURL string) string {
	s := strings.TrimRight(strings.TrimSpace(rawURL), "/")
	s = strings.TrimSuffix(s, "/v1/chat/completions")
	s = strings.TrimSuffix(s, "/v1/completions")
	s = strings.TrimSuffix(s, "/v1/responses")
	s = strings.TrimSuffix(s, "/v1")
	return strings.TrimRight(s, "/")
}

// ChatCompletionsURL returns the POST target for OpenAI chat completions.
// baseURL may be a full /v1/chat/completions path or a prefix ending in /v1.
func ChatCompletionsURL(baseURL string) string {
	s := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(s, "/v1/chat/completions") {
		return s
	}
	return ExtractServerRoot(s) + "/v1/chat/completions"
}

// ResponsesURL returns the POST target for OpenAI Responses API.
// baseURL may be a full /v1/responses path or a prefix ending in /v1.
func ResponsesURL(baseURL string) string {
	s := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(s, "/v1/responses") {
		return s
	}
	return ExtractServerRoot(s) + "/v1/responses"
}

// ResolveEndpointConfig merges registry defaults, custom definitions, and
// per-instance overrides for the active provider in settings.
func ResolveEndpointConfig(settings *config.Settings) (EndpointConfig, error) {
	if settings == nil {
		return EndpointConfig{}, fmt.Errorf("resolve endpoint: settings are required")
	}

	providerID := settings.ActiveProvider()
	if providerID == "" {
		return EndpointConfig{}, fmt.Errorf("resolve endpoint: no active provider configured")
	}

	if def, ok := config.LookupBuiltInProvider(providerID); ok {
		if def.WireFormat == config.WireFormatGemini {
			return geminiEndpointConfig(settings, providerID, def)
		}
		return resolveBuiltInEndpoint(settings, providerID, def)
	}

	if settings.Providers != nil {
		if custom, ok := settings.Providers.Custom[providerID]; ok {
			return resolveCustomEndpoint(settings, providerID, custom)
		}
	}

	return EndpointConfig{}, fmt.Errorf("resolve endpoint: unknown provider %q", providerID)
}

// ResolveEndpointForProvider resolves the endpoint config for an arbitrary
// provider id, regardless of which provider is currently active. It clones the
// settings with providerID forced active so model discovery and edit previews can
// target any configured provider.
func ResolveEndpointForProvider(settings *config.Settings, providerID string) (EndpointConfig, error) {
	if settings == nil {
		return EndpointConfig{}, fmt.Errorf("resolve endpoint: settings are required")
	}
	providerID = config.NormalizeProviderID(providerID)
	if providerID == "" {
		return EndpointConfig{}, fmt.Errorf("resolve endpoint: provider id is required")
	}
	clone := *settings
	prov := config.ProvidersSettings{}
	if clone.Providers != nil {
		prov = *clone.Providers
	}
	prov.Active = providerID
	clone.Providers = &prov
	return ResolveEndpointConfig(&clone)
}

func resolveBuiltInEndpoint(
	settings *config.Settings,
	providerID string,
	def config.BuiltInProvider,
) (EndpointConfig, error) {
	inst := providerInstance(settings, providerID)
	wireFormat := def.WireFormat
	if inst != nil && inst.WireFormat != "" {
		wireFormat = inst.WireFormat
	}

	baseURL := def.DefaultBaseURL
	if inst != nil && inst.BaseURL != "" {
		baseURL = inst.BaseURL
	}
	if baseURL == "" && wireFormat != config.WireFormatGemini {
		return EndpointConfig{}, fmt.Errorf("resolve endpoint: provider %q has no baseUrl configured", providerID)
	}

	model := def.DefaultModel
	if inst != nil && inst.Model != "" {
		model = inst.Model
	}
	if model == "" {
		model = "local-model"
	}

	normalizedURL := normalizeEndpointURL(baseURL, wireFormat)

	return EndpointConfig{
		ProviderID:           providerID,
		BaseURL:              normalizedURL,
		Model:                model,
		WireFormat:           wireFormat,
		Timeout:              resolveTimeout(inst, defaultOpenAITimeout),
		RequiresAPIKey:       def.RequiresAPIKey,
		ToolCallParsing:      resolveToolCallParsing(inst),
		ReasoningEffort:      resolveReasoningEffort(inst),
		UseResponseChaining:  resolveUseResponseChaining(inst),
		Temperature:          config.ResolveEffectiveTemperature(settings, providerID, model),
		SystemPromptOverride: resolveSystemPromptOverride(inst),
		ToolsEnabled:         resolveToolsEnabled(inst),
	}, nil
}

func geminiEndpointConfig(
	settings *config.Settings,
	providerID string,
	def config.BuiltInProvider,
) (EndpointConfig, error) {
	inst := providerInstance(settings, providerID)
	wireFormat := def.WireFormat
	if inst != nil && inst.WireFormat != "" {
		wireFormat = inst.WireFormat
	}
	model := def.DefaultModel
	if inst != nil && inst.Model != "" {
		model = inst.Model
	}
	return EndpointConfig{
		ProviderID:      providerID,
		Model:           model,
		WireFormat:      wireFormat,
		Timeout:         resolveTimeout(inst, 0),
		RequiresAPIKey:  def.RequiresAPIKey,
		ToolCallParsing: resolveToolCallParsing(inst),
		ToolsEnabled:    resolveToolsEnabled(inst),
	}, nil
}

func resolveCustomEndpoint(
	settings *config.Settings,
	providerID string,
	custom config.CustomProviderDefinition,
) (EndpointConfig, error) {
	if strings.TrimSpace(custom.BaseURL) == "" {
		return EndpointConfig{}, fmt.Errorf("resolve endpoint: custom provider %q has no baseUrl", providerID)
	}

	inst := providerInstance(settings, providerID)
	wireFormat := config.WireFormatOpenAIChat
	if custom.WireFormat != "" {
		wireFormat = custom.WireFormat
	}
	if inst != nil && inst.WireFormat != "" {
		wireFormat = inst.WireFormat
	}

	baseURL := custom.BaseURL
	if inst != nil && inst.BaseURL != "" {
		baseURL = inst.BaseURL
	}

	model := custom.DefaultModel
	if inst != nil && inst.Model != "" {
		model = inst.Model
	}
	if model == "" {
		model = "local-model"
	}

	requiresKey := custom.APIKeyEnvVar != ""
	return EndpointConfig{
		ProviderID:           providerID,
		BaseURL:              normalizeEndpointURL(baseURL, wireFormat),
		Model:                model,
		WireFormat:           wireFormat,
		Timeout:              resolveTimeout(inst, defaultOpenAITimeout),
		RequiresAPIKey:       requiresKey,
		ToolCallParsing:      resolveToolCallParsing(inst),
		ReasoningEffort:      resolveReasoningEffort(inst),
		UseResponseChaining:  resolveUseResponseChaining(inst),
		Temperature:          config.ResolveEffectiveTemperature(settings, providerID, model),
		SystemPromptOverride: resolveSystemPromptOverride(inst),
		ToolsEnabled:         resolveToolsEnabled(inst),
	}, nil
}

func normalizeEndpointURL(baseURL string, wireFormat config.WireFormat) string {
	if wireFormat == config.WireFormatOpenAIResponses {
		return ResponsesURL(baseURL)
	}
	return ChatCompletionsURL(baseURL)
}

func providerInstance(settings *config.Settings, providerID string) *config.ProviderInstanceConfig {
	if settings == nil || settings.Providers == nil {
		return nil
	}
	switch providerID {
	case string(config.BuiltInOpenAI):
		return settings.Providers.OpenAI
	case string(config.BuiltInGeminiAPIKey):
		return settings.Providers.GeminiAPIKey
	case string(config.BuiltInOpenAIResponses):
		return settings.Providers.OpenAIResponses
	default:
		if raw, ok := settings.Providers.Extra[providerID]; ok {
			var cfg config.ProviderInstanceConfig
			if err := json.Unmarshal(raw, &cfg); err == nil {
				return &cfg
			}
		}
		return nil
	}
}

func resolveTimeout(inst *config.ProviderInstanceConfig, fallback time.Duration) time.Duration {
	if inst != nil && inst.Timeout != nil && *inst.Timeout > 0 {
		return time.Duration(*inst.Timeout) * time.Second
	}
	return fallback
}

func resolveToolCallParsing(inst *config.ProviderInstanceConfig) config.ToolCallParsingMode {
	if inst != nil && inst.ToolCallParsing != "" {
		return inst.ToolCallParsing
	}
	return config.ToolCallParsingLenient
}

func resolveReasoningEffort(inst *config.ProviderInstanceConfig) string {
	if inst == nil {
		return ""
	}
	return strings.TrimSpace(inst.ReasoningEffort)
}

func resolveUseResponseChaining(inst *config.ProviderInstanceConfig) bool {
	if inst == nil || inst.UseResponseChaining == nil {
		return false
	}
	return *inst.UseResponseChaining
}

func resolveSystemPromptOverride(inst *config.ProviderInstanceConfig) string {
	if inst == nil {
		return ""
	}
	return strings.TrimSpace(inst.SystemPromptOverride)
}

func resolveToolsEnabled(inst *config.ProviderInstanceConfig) bool {
	if inst == nil || inst.EnableTools == nil {
		return true
	}
	return *inst.EnableTools
}
