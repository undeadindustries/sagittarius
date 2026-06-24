package config

import "strings"

// floatPtr returns a pointer to v (helper for the small default tables).
func floatPtr(v float64) *float64 { return &v }

// ModelTemperatureRule reports a model-family sampling opinion for a model id.
//
//   - omit=true: the family rejects or ignores custom temperature (send none).
//     Examples: Gemini 3 / 2.5 (Google recommends the default 1.0; do not send a
//     lower value), GPT-5 / o3 / o4 reasoning models, Anthropic Opus 4.7+.
//   - temp!=nil with omit=false: the family has a recommended fixed value
//     (e.g. Qwen3-Coder -> 1.0).
//   - matched=false: the family has no opinion; the caller should fall through
//     to the personality preset default.
func ModelTemperatureRule(model string) (temp *float64, omit bool, matched bool) {
	m := strings.ToLower(strings.TrimSpace(model))
	if m == "" {
		return nil, false, false
	}
	// Normalize provider-prefixed ids ("openai/gpt-5", "google/gemini-3-pro").
	if i := strings.LastIndex(m, "/"); i >= 0 {
		m = m[i+1:]
	}

	switch {
	case strings.Contains(m, "gemini-3"), strings.Contains(m, "gemini-2.5"):
		return nil, true, true
	case strings.HasPrefix(m, "gpt-5"), strings.HasPrefix(m, "o3"), strings.HasPrefix(m, "o4"):
		return nil, true, true
	case strings.Contains(m, "opus-4.7"), strings.Contains(m, "opus-4.8"),
		strings.Contains(m, "opus-4-7"), strings.Contains(m, "opus-4-8"):
		return nil, true, true
	case strings.Contains(m, "qwen3-coder"):
		return floatPtr(1.0), false, true
	default:
		return nil, false, false
	}
}

// PersonalityDefaultTemperature returns the recommended generic temperature for
// a canonical personality, used when no model-family rule and no user pin apply.
func PersonalityDefaultTemperature(personality string) *float64 {
	switch personality {
	case PersonalitySysadmin:
		return floatPtr(0.25)
	case PersonalityPersonalAssistant:
		return floatPtr(0.55)
	case PersonalityCreativeAssistant:
		return floatPtr(0.85)
	case PersonalityProgrammer:
		return floatPtr(0.35)
	default:
		return floatPtr(0.35)
	}
}

// VariantCompressionThreshold returns the recommended context-compression
// threshold for a variant. Lite variants compress earlier to protect the
// smaller context windows they target.
func VariantCompressionThreshold(variant string) float64 {
	if CanonicalVariant(variant) == VariantLite {
		return 0.38
	}
	return 0.45
}

// ResolveEffectiveTemperature computes the temperature to send for a model,
// applying the resolution order:
//  1. per-model override (providers.<id>.models.<model>.temperature)
//  2. provider instance override (providers.<id>.temperature)
//  3. model-family rule (families that reject custom values return nil)
//  4. personality preset default
//
// A nil result means "send no temperature" (let the server decide), which is the
// correct behavior for families that reject custom values.
func ResolveEffectiveTemperature(settings *Settings, providerID, model string) *float64 {
	if inst := settings.ProviderInstance(providerID); inst != nil {
		if mc, ok := lookupModelConfig(inst, model); ok && mc.Temperature != nil {
			return mc.Temperature
		}
		if inst.Temperature != nil {
			return inst.Temperature
		}
	}
	if temp, omit, matched := ModelTemperatureRule(model); matched {
		if omit {
			return nil
		}
		return temp
	}
	return PersonalityDefaultTemperature(ResolvePersonality(settings, providerID, model))
}

// ResolveShowThinking reports whether the model reasoning ("thinking") box
// should be shown for a (provider, model), applying the resolution order:
//  1. per-model override (providers.<id>.models.<model>.showThinking)
//  2. provider instance override (providers.<id>.showThinking)
//  3. global ui.showThinking
//  4. false (hidden)
func ResolveShowThinking(settings *Settings, providerID, model string) bool {
	if settings == nil {
		return false
	}
	if inst := settings.ProviderInstance(providerID); inst != nil {
		if mc, ok := lookupModelConfig(inst, model); ok && mc.ShowThinking != nil {
			return *mc.ShowThinking
		}
		if inst.ShowThinking != nil {
			return *inst.ShowThinking
		}
	}
	return settings.UI().ShowThinking
}
