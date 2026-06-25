package config

import (
	"strings"
)

// ProjectSystemPromptPresetID returns the preset id matching an explicitly
// configured sagittarius.systemPrompt, or "" when unset.
func ProjectSystemPromptPresetID(settings *Settings) string {
	gp := globalSystemPrompt(settings)
	if gp == nil || (strings.TrimSpace(gp.Personality) == "" && strings.TrimSpace(gp.Variant) == "") {
		return ""
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
