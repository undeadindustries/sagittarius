package config

import (
	"sort"
	"strings"
)

// Variant ids for the system prompt size. Canonical home is config so provider
// and prompt can both reference them without an import cycle.
const (
	VariantFull = "full"
	VariantLite = "lite"
)

// CanonicalVariant normalizes v to "full" or "lite". Anything other than a
// case-insensitive "lite" resolves to "full".
func CanonicalVariant(v string) string {
	if strings.ToLower(strings.TrimSpace(v)) == VariantLite {
		return VariantLite
	}
	return VariantFull
}

// SystemPromptPreset is a single user-facing choice in the providers dialog that
// collapses the personality and variant axes into one pick. Selecting a preset
// writes both personality and promptMode on the provider instance.
type SystemPromptPreset struct {
	ID          string
	Label       string
	Personality string
	Variant     string
}

// SystemPromptPresets is the ordered list shown in the system-prompt picker.
var SystemPromptPresets = []SystemPromptPreset{
	{ID: "programmer", Label: "Programmer", Personality: PersonalityProgrammer, Variant: VariantFull},
	{ID: "programmer-lite", Label: "Programmer (low context)", Personality: PersonalityProgrammer, Variant: VariantLite},
	{ID: "sysadmin", Label: "System administrator", Personality: PersonalitySysadmin, Variant: VariantFull},
	{ID: "sysadmin-lite", Label: "System administrator (low context)", Personality: PersonalitySysadmin, Variant: VariantLite},
	{ID: "personal-assistant", Label: "Personal assistant", Personality: PersonalityPersonalAssistant, Variant: VariantFull},
	{ID: "personal-assistant-lite", Label: "Personal assistant (low context)", Personality: PersonalityPersonalAssistant, Variant: VariantLite},
	{ID: "creative-assistant", Label: "Creative assistant", Personality: PersonalityCreativeAssistant, Variant: VariantFull},
	{ID: "creative-assistant-lite", Label: "Creative assistant (low context)", Personality: PersonalityCreativeAssistant, Variant: VariantLite},
}

// SortedSystemPromptPresets returns a copy of SystemPromptPresets sorted alphabetically by ID,
// keeping full/lite variants together while presenting them in a predictable order.
func SortedSystemPromptPresets() []SystemPromptPreset {
	out := make([]SystemPromptPreset, len(SystemPromptPresets))
	copy(out, SystemPromptPresets)
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

// LookupPreset returns the preset with the given id (case-insensitive).
func LookupPreset(id string) (SystemPromptPreset, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, p := range SystemPromptPresets {
		if p.ID == id {
			return p, true
		}
	}
	return SystemPromptPreset{}, false
}

// PresetForPersonalityVariant finds the preset matching a (personality, variant)
// pair after canonicalization. It is used to pre-select the picker from stored
// settings.
func PresetForPersonalityVariant(personality, variant string) (SystemPromptPreset, bool) {
	canon, _ := CanonicalPersonality(personality)
	v := CanonicalVariant(variant)
	for _, p := range SystemPromptPresets {
		if p.Personality == canon && p.Variant == v {
			return p, true
		}
	}
	return SystemPromptPreset{}, false
}

// ResolvePersonality picks the canonical system-prompt personality for
// (providerID, model) using the first non-empty tier: per-model override ->
// provider override -> global default -> built-in (programmer).
func ResolvePersonality(settings *Settings, providerID, model string) string {
	if inst := settings.ProviderInstance(providerID); inst != nil {
		if mc, ok := lookupModelConfig(inst, model); ok {
			if canon, ok := CanonicalPersonality(mc.Personality); ok {
				return canon
			}
		}
		if canon, ok := CanonicalPersonality(inst.Personality); ok {
			return canon
		}
	}
	if gp := globalSystemPrompt(settings); gp != nil {
		if canon, ok := CanonicalPersonality(gp.Personality); ok {
			return canon
		}
	}
	return PersonalityProgrammer
}

// ResolveVariant picks the prompt variant ("full"/"lite") for (providerID,
// model) using the same tier order, reusing the promptMode setting.
func ResolveVariant(settings *Settings, providerID, model string) string {
	if inst := settings.ProviderInstance(providerID); inst != nil {
		if mc, ok := lookupModelConfig(inst, model); ok {
			if v := strings.TrimSpace(string(mc.PromptMode)); v != "" {
				return CanonicalVariant(v)
			}
		}
		if v := strings.TrimSpace(string(inst.PromptMode)); v != "" {
			return CanonicalVariant(v)
		}
	}
	if gp := globalSystemPrompt(settings); gp != nil {
		if v := strings.TrimSpace(gp.Variant); v != "" {
			return CanonicalVariant(v)
		}
	}
	return VariantFull
}

func lookupModelConfig(inst *ProviderInstanceConfig, model string) (ProviderModelConfig, bool) {
	model = strings.TrimSpace(model)
	if inst == nil || len(inst.Models) == 0 || model == "" {
		return ProviderModelConfig{}, false
	}
	mc, ok := inst.Models[model]
	return mc, ok
}

// LookupModelConfig is the exported form of lookupModelConfig, for use by
// packages that cannot call the unexported helper (e.g. internal/provider).
func LookupModelConfig(inst *ProviderInstanceConfig, model string) (ProviderModelConfig, bool) {
	return lookupModelConfig(inst, model)
}

func globalSystemPrompt(settings *Settings) *SagittariusSystemPromptConfig {
	if settings == nil || settings.Sagittarius == nil {
		return nil
	}
	return settings.Sagittarius.SystemPrompt
}
