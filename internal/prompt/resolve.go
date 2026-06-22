package prompt

import (
	"github.com/undeadindustries/sagittarius/internal/config"
)

// ResolvePersonality picks the system-prompt personality for (providerID, model)
// using the first non-empty tier: per-model override -> provider override ->
// global default -> built-in (programmer). Resolution lives in config so
// internal/provider can share it without importing prompt.
func ResolvePersonality(settings *config.Settings, providerID, model string) Personality {
	return Personality(config.ResolvePersonality(settings, providerID, model))
}

// ResolveVariant picks the prompt variant for (providerID, model) using the same
// tier order, falling back to the built-in (full).
func ResolveVariant(settings *config.Settings, providerID, model string) Variant {
	return Variant(config.ResolveVariant(settings, providerID, model))
}
