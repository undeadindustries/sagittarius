package config

import (
	"encoding/json"
	"os"
	"strings"
)

// PromptMode controls system prompt size for OpenAI-compat providers.
type PromptMode string

const (
	PromptModeLite PromptMode = "lite"
	PromptModeFull PromptMode = "full"
)

// ToolCallParsingMode controls tool-call recovery aggressiveness.
type ToolCallParsingMode string

const (
	ToolCallParsingStrict  ToolCallParsingMode = "strict"
	ToolCallParsingLenient ToolCallParsingMode = "lenient"
	ToolCallParsingLoose   ToolCallParsingMode = "loose"
)

// ProviderInstanceConfig holds per-instance overrides under providers.<id>.*
// Field names match fork settingsSchema.ts (openai / openai-compat sheet).
type ProviderInstanceConfig struct {
	Model                string                     `json:"model,omitempty"`
	BaseURL              string                     `json:"baseUrl,omitempty"`
	ContextLimit         *int                       `json:"contextLimit,omitempty"`
	CompressionThreshold *float64                   `json:"compressionThreshold,omitempty"`
	PreserveFraction     *float64                   `json:"preserveFraction,omitempty"`
	PromptMode           PromptMode                 `json:"promptMode,omitempty"`
	EnableTools          *bool                      `json:"enableTools,omitempty"`
	Timeout              *int                       `json:"timeout,omitempty"`
	Temperature          *float64                   `json:"temperature,omitempty"`
	ToolCallParsing      ToolCallParsingMode        `json:"toolCallParsing,omitempty"`
	SystemPromptOverride string                     `json:"systemPromptOverride,omitempty"`
	ReasoningEffort      string                     `json:"reasoningEffort,omitempty"`
	UseResponseChaining  *bool                      `json:"useResponseChaining,omitempty"`
	WireFormat           WireFormat                 `json:"wireFormat,omitempty"`
	Extra                map[string]json.RawMessage `json:"-"`
}

// CustomProviderDefinition is a user-defined OpenAI-compatible provider under
// providers.custom.<id> (fork CustomProviderDefinition).
type CustomProviderDefinition struct {
	DisplayName         string                     `json:"displayName"`
	BaseURL             string                     `json:"baseUrl"`
	DefaultModel        string                     `json:"defaultModel,omitempty"`
	DefaultContextLimit *int                       `json:"defaultContextLimit,omitempty"`
	APIKeyEnvVar        string                     `json:"apiKeyEnvVar,omitempty"`
	WireFormat          WireFormat                 `json:"wireFormat,omitempty"`
	Extra               map[string]json.RawMessage `json:"-"`
}

// ProvidersSettings is the typed providers.* subset from settings.json.
type ProvidersSettings struct {
	Active       string                              `json:"active,omitempty"`
	OpenAI       *ProviderInstanceConfig             `json:"openai,omitempty"`
	GeminiAPIKey *ProviderInstanceConfig             `json:"gemini-apikey,omitempty"`
	Custom       map[string]CustomProviderDefinition `json:"custom,omitempty"`
	// Extra holds other provider instance blocks (e.g. openai-responses, local-vllm)
	// as raw JSON so round-trip does not drop keys Sagittarius does not model yet.
	Extra map[string]json.RawMessage `json:"-"`
}

// Settings is the in-memory view of settings.json with typed providers and
// passthrough for all other top-level sections.
type Settings struct {
	Providers *ProvidersSettings
	// Raw preserves untouched top-level JSON keys (ui, general, mcp, …).
	Raw map[string]json.RawMessage
}

// ActiveProvider returns the effective active provider id.
// GEMINI_PROVIDER env var overrides providers.active (fork config.ts / settingsSchema).
func (s *Settings) ActiveProvider() string {
	if v := strings.TrimSpace(os.Getenv("GEMINI_PROVIDER")); v != "" {
		return v
	}
	if s != nil && s.Providers != nil {
		return s.Providers.Active
	}
	return ""
}
