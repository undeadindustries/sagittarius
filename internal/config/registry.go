package config

import "strings"

// WireFormat identifies the HTTP wire protocol spoken by a provider endpoint.
type WireFormat string

const (
	WireFormatOpenAIChat      WireFormat = "openai-chat"
	WireFormatOpenAIResponses WireFormat = "openai-responses"
	WireFormatGemini          WireFormat = "gemini"
)

// BuiltInProviderID identifies a built-in provider registry entry.
type BuiltInProviderID string

const (
	BuiltInGeminiAPIKey BuiltInProviderID = "gemini-apikey"
	// BuiltInOpenAI and BuiltInOpenAIResponses are no longer native built-ins
	// (they were collapsed into the ProviderPresets templates, AD-072). The ids
	// are retained because they remain the canonical settings.json ids for the
	// migrated custom providers, the typed instance-override store
	// (ProvidersSettings.OpenAI / OpenAIResponses), and legacy references. They
	// are intentionally absent from BuiltInProviders — only Gemini is built-in.
	BuiltInOpenAI          BuiltInProviderID = "openai"
	BuiltInOpenAIResponses BuiltInProviderID = "openai-responses"
)

// BuiltInProvider describes a frozen registry entry shipped with the binary.
// Shape mirrors fork providerRegistry.ts BUILT_IN_PROVIDERS (subset for Phase 02).
type BuiltInProvider struct {
	ID                  BuiltInProviderID
	DisplayName         string
	DefaultBaseURL      string
	APIKeyEnvVar        string
	WireFormat          WireFormat
	DefaultModel        string
	DefaultContextLimit int
	RequiresAPIKey      bool
}

// BuiltInProviders maps built-in provider ids to their registry defaults.
// Gemini is the only native built-in: it needs the genai SDK, has no base URL,
// and uses GEMINI_API_KEY/GOOGLE_API_KEY. Every other known provider (OpenAI,
// OpenRouter, Anthropic, …) is a ProviderPreset template that materializes an
// ordinary providers.custom.<id> definition (AD-072).
var BuiltInProviders = map[BuiltInProviderID]BuiltInProvider{
	BuiltInGeminiAPIKey: {
		ID:                  BuiltInGeminiAPIKey,
		DisplayName:         "Gemini",
		DefaultBaseURL:      "",
		APIKeyEnvVar:        "GEMINI_API_KEY",
		WireFormat:          WireFormatGemini,
		DefaultModel:        "gemini-2.5-pro",
		DefaultContextLimit: 1_048_576,
		RequiresAPIKey:      true,
	},
}

// LookupBuiltInProvider returns the registry entry for id, or false if unknown.
func LookupBuiltInProvider(id string) (BuiltInProvider, bool) {
	def, ok := BuiltInProviders[BuiltInProviderID(NormalizeProviderID(id))]
	return def, ok
}

// ProviderDefaults returns the registry-style defaults for a known provider id:
// a native built-in (Gemini) or a preset template converted to a BuiltInProvider
// shape. It lets endpoint resolution and validation treat presets as "known"
// providers even before a providers.custom.<id> definition has been persisted,
// so existing openai/openai-responses settings keep resolving after the
// built-in collapse (AD-072).
func ProviderDefaults(id string) (BuiltInProvider, bool) {
	if def, ok := LookupBuiltInProvider(id); ok {
		return def, true
	}
	if p, ok := LookupProviderPreset(NormalizeProviderID(id)); ok {
		return BuiltInProvider{
			ID:                  BuiltInProviderID(p.ID),
			DisplayName:         p.DisplayName,
			DefaultBaseURL:      p.BaseURL,
			APIKeyEnvVar:        p.APIKeyEnvVar,
			WireFormat:          p.WireFormat,
			DefaultModel:        p.DefaultModel,
			DefaultContextLimit: p.DefaultContextLimit,
			RequiresAPIKey:      p.APIKeyEnvVar != "",
		}, true
	}
	return BuiltInProvider{}, false
}

// NormalizeProviderID maps user-facing aliases to canonical settings.json ids.
// The canonical id for Gemini remains "gemini-apikey" (fork-compatible); users
// may type "gemini" in /provider use and completion.
func NormalizeProviderID(id string) string {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "gemini":
		return string(BuiltInGeminiAPIKey)
	default:
		return strings.TrimSpace(id)
	}
}

// ProviderDisplayID returns the short id shown in /providers list and completion.
func ProviderDisplayID(canonicalID string) string {
	if canonicalID == string(BuiltInGeminiAPIKey) {
		return "gemini"
	}
	return canonicalID
}

// openAIChatSettingKeys are the per-instance keys editable for an openai-chat
// provider. Mirrors fork providerRegistry.ts OPENAI_COMPAT_SETTING_KEYS plus the
// Sagittarius per-provider tool-output masking knobs (AD-015), which are modelled
// per provider here rather than in a single global local.* block.
var openAIChatSettingKeys = []string{
	"model",
	"baseUrl",
	"contextLimit",
	"promptMode",
	"personality",
	"enableTools",
	"timeout",
	"compressionThreshold",
	"preserveFraction",
	"temperature",
	"toolCallParsing",
	"systemPromptOverride",
	"showThinking",
	"toolOutputMaskingEnabled",
	"toolOutputMaskingProtectionFraction",
	"toolOutputMaskingPrunableFraction",
	"toolOutputMaskingProtectLatestTurn",
}

// openAIResponsesSettingKeys are the per-instance keys editable for an
// openai-responses provider. Mirrors fork OPENAI_RESPONSES_SETTING_KEYS: the
// openai-chat set minus toolCallParsing (Responses returns structured
// function_call items) plus reasoningEffort and useResponseChaining.
var openAIResponsesSettingKeys = []string{
	"model",
	"baseUrl",
	"contextLimit",
	"promptMode",
	"personality",
	"enableTools",
	"timeout",
	"compressionThreshold",
	"preserveFraction",
	"temperature",
	"reasoningEffort",
	"useResponseChaining",
	"systemPromptOverride",
	"showThinking",
}

// ValidSettingKeys returns the editable providers.<id>.* leaf keys for a wire
// format. Gemini providers expose none — upstream owns those defaults (fork
// GEMINI_SETTING_KEYS = []).
func ValidSettingKeys(wf WireFormat) []string {
	switch wf {
	case WireFormatOpenAIChat:
		return append([]string(nil), openAIChatSettingKeys...)
	case WireFormatOpenAIResponses:
		return append([]string(nil), openAIResponsesSettingKeys...)
	default:
		return nil
	}
}

// ProviderWireFormat resolves the wire format for a provider id. For custom
// providers the caller passes the definition (its WireFormat wins, defaulting to
// openai-chat). Unknown ids return an empty wire format.
func ProviderWireFormat(id string, custom *CustomProviderDefinition) WireFormat {
	if def, ok := LookupBuiltInProvider(id); ok {
		return def.WireFormat
	}
	if custom != nil {
		if custom.WireFormat != "" {
			return custom.WireFormat
		}
		return WireFormatOpenAIChat
	}
	// No custom definition yet: fall back to a preset template so a legacy
	// openai/openai-responses id (or any preset) still exposes the right
	// editable settings before migration materializes a custom block.
	if p, ok := LookupProviderPreset(NormalizeProviderID(id)); ok {
		if p.WireFormat != "" {
			return p.WireFormat
		}
		return WireFormatOpenAIChat
	}
	return ""
}

// ValidSettingKeysForProvider returns the editable leaf keys for a provider id,
// resolving its wire format from the registry or custom definition.
func ValidSettingKeysForProvider(id string, custom *CustomProviderDefinition) []string {
	return ValidSettingKeys(ProviderWireFormat(id, custom))
}
