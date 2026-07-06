package config

import "strings"

// ProviderPreset is a ready-made template for a well-known OpenAI-compatible (or
// OpenAI-Responses) provider. Presets seed the custom-provider add wizard and
// the first-run onboarding flow: selecting one pre-fills the display name, base
// URL, wire format, and API-key env var so the user only needs to paste a key.
//
// A preset is not a built-in provider — the only native built-in is Gemini
// (config.BuiltInGeminiAPIKey). Choosing a preset produces an ordinary
// providers.custom.<id> definition reusing the preset ID.
type ProviderPreset struct {
	// ID is the canonical settings.json provider id created from this preset
	// (e.g. "openai", "openrouter"). It doubles as the default custom id.
	ID string
	// DisplayName is the human-readable label shown in pickers.
	DisplayName string
	// BaseURL is the full POST endpoint (…/chat/completions or …/responses).
	// It is stored verbatim as the custom definition's baseUrl; the endpoint
	// normalizer honors a URL already ending in /chat/completions or /responses.
	BaseURL string
	// APIKeyEnvVar is the environment variable checked for this provider's key.
	APIKeyEnvVar string
	// WireFormat is the HTTP dialect (openai-chat for all but openai-responses).
	WireFormat WireFormat
	// DefaultModel is a fallback model id used when live discovery returns
	// nothing (e.g. non-/v1 hosts) or as the initial selection. May be empty.
	DefaultModel string
	// DefaultContextLimit is the fallback context window when the provider does
	// not report one. Zero means "unset" (fall back to the global default).
	DefaultContextLimit int
	// Note is an optional one-line caveat surfaced in the UI (e.g. Anthropic's
	// OpenAI-compat layer being eval-grade, or z.ai lacking model discovery).
	Note string
}

// ProviderPresets is the ordered set of provider templates offered when adding a
// custom provider. Base URLs and env vars are the vendors' documented
// OpenAI-compatible endpoints (verified Jul 2026). Everything is openai-chat
// except OpenAI-Responses. AWS Bedrock is intentionally excluded: SigV4 signing
// and regional hosts do not fit the paste-URL + API-key model.
var ProviderPresets = []ProviderPreset{
	{
		ID:                  "openai",
		DisplayName:         "OpenAI",
		BaseURL:             "https://api.openai.com/v1/chat/completions",
		APIKeyEnvVar:        "OPENAI_API_KEY",
		WireFormat:          WireFormatOpenAIChat,
		DefaultModel:        "gpt-4o-mini",
		DefaultContextLimit: 128_000,
	},
	{
		ID:                  "openai-responses",
		DisplayName:         "OpenAI Responses",
		BaseURL:             "https://api.openai.com/v1/responses",
		APIKeyEnvVar:        "OPENAI_API_KEY",
		WireFormat:          WireFormatOpenAIResponses,
		DefaultModel:        "gpt-5-codex",
		DefaultContextLimit: 400_000,
		Note:                "Responses API for GPT-5 / reasoning models (reasoning.effort, response chaining).",
	},
	{
		ID:           "openrouter",
		DisplayName:  "OpenRouter",
		BaseURL:      "https://openrouter.ai/api/v1/chat/completions",
		APIKeyEnvVar: "OPENROUTER_API_KEY",
		WireFormat:   WireFormatOpenAIChat,
	},
	{
		ID:                  "anthropic",
		DisplayName:         "Anthropic",
		BaseURL:             "https://api.anthropic.com/v1/chat/completions",
		APIKeyEnvVar:        "ANTHROPIC_API_KEY",
		WireFormat:          WireFormatOpenAIChat,
		DefaultContextLimit: 200_000,
		Note:                "OpenAI-compat layer (eval-grade); use OpenRouter for full Claude features.",
	},
	{
		ID:           "deepseek",
		DisplayName:  "DeepSeek",
		BaseURL:      "https://api.deepseek.com/v1/chat/completions",
		APIKeyEnvVar: "DEEPSEEK_API_KEY",
		WireFormat:   WireFormatOpenAIChat,
		DefaultModel: "deepseek-chat",
	},
	{
		ID:           "xai",
		DisplayName:  "xAI (Grok)",
		BaseURL:      "https://api.x.ai/v1/chat/completions",
		APIKeyEnvVar: "XAI_API_KEY",
		WireFormat:   WireFormatOpenAIChat,
		DefaultModel: "grok-4",
	},
	{
		ID:           "zai",
		DisplayName:  "z.ai (GLM)",
		BaseURL:      "https://api.z.ai/api/coding/paas/v4/chat/completions",
		APIKeyEnvVar: "ZAI_API_KEY",
		WireFormat:   WireFormatOpenAIChat,
		DefaultModel: "glm-4.6",
		Note:         "Model discovery unavailable (non-/v1 path); pick the default or type a model id.",
	},
	{
		ID:           "groq",
		DisplayName:  "Groq",
		BaseURL:      "https://api.groq.com/openai/v1/chat/completions",
		APIKeyEnvVar: "GROQ_API_KEY",
		WireFormat:   WireFormatOpenAIChat,
	},
	{
		ID:           "together",
		DisplayName:  "Together AI",
		BaseURL:      "https://api.together.xyz/v1/chat/completions",
		APIKeyEnvVar: "TOGETHER_API_KEY",
		WireFormat:   WireFormatOpenAIChat,
	},
	{
		ID:           "mistral",
		DisplayName:  "Mistral",
		BaseURL:      "https://api.mistral.ai/v1/chat/completions",
		APIKeyEnvVar: "MISTRAL_API_KEY",
		WireFormat:   WireFormatOpenAIChat,
	},
	{
		ID:           "fireworks",
		DisplayName:  "Fireworks AI",
		BaseURL:      "https://api.fireworks.ai/inference/v1/chat/completions",
		APIKeyEnvVar: "FIREWORKS_API_KEY",
		WireFormat:   WireFormatOpenAIChat,
	},
}

// LookupProviderPreset returns the preset with the given id (case-insensitive,
// trimmed), or false when none matches. It is named to avoid colliding with the
// system-prompt LookupPreset in systemprompt.go.
func LookupProviderPreset(id string) (ProviderPreset, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, p := range ProviderPresets {
		if strings.ToLower(p.ID) == id {
			return p, true
		}
	}
	return ProviderPreset{}, false
}

// ToCustomProviderDefinition builds the providers.custom.<id> definition seeded
// from this preset. Instance overrides (model, temperature, …) are layered on
// top later via the normal per-instance settings path.
func (p ProviderPreset) ToCustomProviderDefinition() CustomProviderDefinition {
	def := CustomProviderDefinition{
		DisplayName:  p.DisplayName,
		BaseURL:      p.BaseURL,
		APIKeyEnvVar: p.APIKeyEnvVar,
		WireFormat:   p.WireFormat,
		DefaultModel: p.DefaultModel,
	}
	if p.DefaultContextLimit > 0 {
		limit := p.DefaultContextLimit
		def.DefaultContextLimit = &limit
	}
	return def
}
