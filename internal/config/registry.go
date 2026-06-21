package config

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
	BuiltInGeminiAPIKey    BuiltInProviderID = "gemini-apikey"
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
var BuiltInProviders = map[BuiltInProviderID]BuiltInProvider{
	BuiltInGeminiAPIKey: {
		ID:                  BuiltInGeminiAPIKey,
		DisplayName:         "Gemini (API key)",
		DefaultBaseURL:      "",
		APIKeyEnvVar:        "GEMINI_API_KEY",
		WireFormat:          WireFormatGemini,
		DefaultModel:        "gemini-2.5-pro",
		DefaultContextLimit: 1_048_576,
		RequiresAPIKey:      true,
	},
	BuiltInOpenAI: {
		ID:                  BuiltInOpenAI,
		DisplayName:         "OpenAI",
		DefaultBaseURL:      "https://api.openai.com/v1/chat/completions",
		APIKeyEnvVar:        "OPENAI_API_KEY",
		WireFormat:          WireFormatOpenAIChat,
		DefaultModel:        "gpt-4o-mini",
		DefaultContextLimit: 128_000,
		RequiresAPIKey:      true,
	},
	BuiltInOpenAIResponses: {
		ID:                  BuiltInOpenAIResponses,
		DisplayName:         "OpenAI Responses",
		DefaultBaseURL:      "https://api.openai.com/v1/responses",
		APIKeyEnvVar:        "OPENAI_API_KEY",
		WireFormat:          WireFormatOpenAIResponses,
		DefaultModel:        "gpt-5-codex",
		DefaultContextLimit: 400_000,
		RequiresAPIKey:      true,
	},
}

// LookupBuiltInProvider returns the registry entry for id, or false if unknown.
func LookupBuiltInProvider(id string) (BuiltInProvider, bool) {
	def, ok := BuiltInProviders[BuiltInProviderID(id)]
	return def, ok
}
