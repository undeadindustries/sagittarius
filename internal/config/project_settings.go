package config

import (
	"strings"
)

// ProjectSystemPromptPresetID returns the selection in an explicitly
// configured sagittarius.systemPrompt: the custom prompt name when one is set,
// otherwise the matching built-in preset id, or "" when unset. Custom names
// never collide with preset ids, so callers can tell them apart with
// LookupPreset.
func ProjectSystemPromptPresetID(settings *Settings) string {
	gp := globalSystemPrompt(settings)
	if gp.IsEmpty() {
		return ""
	}
	if c := strings.TrimSpace(gp.Custom); c != "" {
		return c
	}
	personality := gp.Personality
	variant := gp.Variant
	if strings.TrimSpace(personality) == "" {
		personality = PersonalityProgrammer
	}
	if strings.TrimSpace(variant) == "" {
		variant = VariantFull
	}
	if p, ok := PresetForPersonalityVariant(personality, variant); ok {
		return p.ID
	}
	return ""
}

// CanonicalPersonalityID returns the canonical personality or programmer default.
func CanonicalPersonalityID(id string) string {
	if canon, ok := CanonicalPersonality(id); ok {
		return canon
	}
	return PersonalityProgrammer
}
