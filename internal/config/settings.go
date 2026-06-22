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
	Model                string              `json:"model,omitempty"`
	BaseURL              string              `json:"baseUrl,omitempty"`
	ContextLimit         *int                `json:"contextLimit,omitempty"`
	// ContextLimitUserSet marks contextLimit as an explicit user pin so the
	// model-switch auto-discovery (provider.MaybeSetContextLimit) leaves it
	// alone. Auto-discovered limits set ContextLimit without this flag.
	ContextLimitUserSet *bool `json:"contextLimitUserSet,omitempty"`
	CompressionThreshold *float64            `json:"compressionThreshold,omitempty"`
	PreserveFraction     *float64            `json:"preserveFraction,omitempty"`
	PromptMode           PromptMode          `json:"promptMode,omitempty"`
	EnableTools          *bool               `json:"enableTools,omitempty"`
	Timeout              *int                `json:"timeout,omitempty"`
	Temperature          *float64            `json:"temperature,omitempty"`
	ToolCallParsing      ToolCallParsingMode `json:"toolCallParsing,omitempty"`
	SystemPromptOverride string              `json:"systemPromptOverride,omitempty"`
	ReasoningEffort      string              `json:"reasoningEffort,omitempty"`
	UseResponseChaining  *bool               `json:"useResponseChaining,omitempty"`
	WireFormat           WireFormat          `json:"wireFormat,omitempty"`

	// Personality selects the system-prompt personality for this provider
	// (e.g. "programmer"). Empty falls back to sagittarius.systemPrompt.personality.
	Personality string `json:"personality,omitempty"`

	// Models holds optional per-model overrides keyed by model id (minimal
	// AD-024 slice: personality + promptMode only). A model entry beats the
	// provider-level Personality/PromptMode for system-prompt resolution.
	Models map[string]ProviderModelConfig `json:"models,omitempty"`

	// ActiveModels is the curated set of models the user has activated for this
	// provider (the /providers model-activation screen writes it; /models reads
	// it). Empty/absent means "not yet curated" -- /models falls back to the
	// configured default model. Models are active by default at browse time.
	ActiveModels []string `json:"activeModels,omitempty"`

	// Phase 11 context-management knobs (active only for openai-chat). Names
	// mirror the fork local.* leaf keys; they live per-provider here because
	// Sagittarius providers differ by config rather than a single local block
	// (AD-003 / AD-015). Unset (nil) means "use the built-in default".
	ToolOutputMaskingEnabled            *bool    `json:"toolOutputMaskingEnabled,omitempty"`
	ToolOutputMaskingProtectionFraction *float64 `json:"toolOutputMaskingProtectionFraction,omitempty"`
	ToolOutputMaskingPrunableFraction   *float64 `json:"toolOutputMaskingPrunableFraction,omitempty"`
	ToolOutputMaskingProtectLatestTurn  *bool    `json:"toolOutputMaskingProtectLatestTurn,omitempty"`

	Extra map[string]json.RawMessage `json:"-"`
}

// ProviderModelConfig holds per-model overrides under providers.<id>.models.<model>.
// Unknown keys round-trip via Extra.
type ProviderModelConfig struct {
	Personality string     `json:"personality,omitempty"`
	PromptMode  PromptMode `json:"promptMode,omitempty"`
	// Temperature overrides the provider-level temperature for this model only.
	// A nil value inherits the provider instance override (or the model-family rule).
	Temperature *float64 `json:"temperature,omitempty"`
	// ContextLimit overrides the provider-level context window size for this
	// model only. Nil inherits the provider instance value.
	ContextLimit *int `json:"contextLimit,omitempty"`
	// ReasoningEffort overrides the provider-level reasoning effort for this
	// model only (openai-responses wire only; empty inherits provider value).
	ReasoningEffort string                     `json:"reasoningEffort,omitempty"`
	Extra           map[string]json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the known per-model fields and preserves unknown keys.
func (c *ProviderModelConfig) UnmarshalJSON(data []byte) error {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	c.Extra = make(map[string]json.RawMessage)
	for key, val := range obj {
		switch key {
		case "personality":
			if err := json.Unmarshal(val, &c.Personality); err != nil {
				return err
			}
		case "promptMode":
			if err := json.Unmarshal(val, &c.PromptMode); err != nil {
				return err
			}
		case "temperature":
			var f float64
			if err := json.Unmarshal(val, &f); err != nil {
				return err
			}
			c.Temperature = &f
		case "contextLimit":
			var n int
			if err := json.Unmarshal(val, &n); err != nil {
				return err
			}
			c.ContextLimit = &n
		case "reasoningEffort":
			if err := json.Unmarshal(val, &c.ReasoningEffort); err != nil {
				return err
			}
		default:
			c.Extra[key] = val
		}
	}
	if len(c.Extra) == 0 {
		c.Extra = nil
	}
	return nil
}

// MarshalJSON emits the known per-model fields plus any preserved unknown keys.
func (c ProviderModelConfig) MarshalJSON() ([]byte, error) {
	obj := make(map[string]json.RawMessage)
	if c.Personality != "" {
		b, err := json.Marshal(c.Personality)
		if err != nil {
			return nil, err
		}
		obj["personality"] = b
	}
	if c.PromptMode != "" {
		b, err := json.Marshal(c.PromptMode)
		if err != nil {
			return nil, err
		}
		obj["promptMode"] = b
	}
	if c.Temperature != nil {
		b, err := json.Marshal(*c.Temperature)
		if err != nil {
			return nil, err
		}
		obj["temperature"] = b
	}
	if c.ContextLimit != nil {
		b, err := json.Marshal(*c.ContextLimit)
		if err != nil {
			return nil, err
		}
		obj["contextLimit"] = b
	}
	if c.ReasoningEffort != "" {
		b, err := json.Marshal(c.ReasoningEffort)
		if err != nil {
			return nil, err
		}
		obj["reasoningEffort"] = b
	}
	for key, val := range c.Extra {
		obj[key] = val
	}
	return json.Marshal(obj)
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
	Active          string                              `json:"active,omitempty"`
	OpenAI          *ProviderInstanceConfig             `json:"openai,omitempty"`
	GeminiAPIKey    *ProviderInstanceConfig             `json:"gemini-apikey,omitempty"`
	OpenAIResponses *ProviderInstanceConfig             `json:"openai-responses,omitempty"`
	Custom          map[string]CustomProviderDefinition `json:"custom,omitempty"`
	// Extra holds other provider instance blocks (e.g. openai-responses, local-vllm)
	// as raw JSON so round-trip does not drop keys Sagittarius does not model yet.
	Extra map[string]json.RawMessage `json:"-"`
}

// Settings is the in-memory view of settings.json with typed providers and
// passthrough for all other top-level sections.
type Settings struct {
	Providers   *ProvidersSettings
	Sagittarius *SagittariusSettings
	Security    *SecuritySettings
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

// ProviderInstance returns the provider instance config for id (after alias
// normalization), or nil when none is configured. Custom providers stored under
// providers.<id> round-trip through Extra.
func (s *Settings) ProviderInstance(id string) *ProviderInstanceConfig {
	if s == nil || s.Providers == nil {
		return nil
	}
	switch NormalizeProviderID(id) {
	case string(BuiltInOpenAI):
		return s.Providers.OpenAI
	case string(BuiltInGeminiAPIKey):
		return s.Providers.GeminiAPIKey
	case string(BuiltInOpenAIResponses):
		return s.Providers.OpenAIResponses
	default:
		if raw, ok := s.Providers.Extra[NormalizeProviderID(id)]; ok {
			var cfg ProviderInstanceConfig
			if err := json.Unmarshal(raw, &cfg); err == nil {
				return &cfg
			}
		}
		return nil
	}
}
