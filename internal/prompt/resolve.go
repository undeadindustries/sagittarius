package prompt

import (
	"strings"

	"github.com/undeadindustries/sagittarius/internal/config"
)

// ResolvePersonality picks the system-prompt personality for (providerID, model)
// using the first non-empty tier: per-model override -> provider override ->
// global default -> built-in (programmer).
func ResolvePersonality(settings *config.Settings, providerID, model string) Personality {
	inst := instanceFor(settings, providerID)
	if inst != nil {
		if mc, ok := lookupModel(inst, model); ok {
			if p := strings.TrimSpace(mc.Personality); p != "" {
				return Personality(p)
			}
		}
		if p := strings.TrimSpace(inst.Personality); p != "" {
			return Personality(p)
		}
	}
	if gp := globalSystemPrompt(settings); gp != nil {
		if p := strings.TrimSpace(gp.Personality); p != "" {
			return Personality(p)
		}
	}
	return DefaultPersonality
}

// ResolveVariant picks the prompt variant for (providerID, model) using the same
// tier order, falling back to the built-in (full). The provider/per-model tier
// reuses the existing promptMode (lite/full) setting.
func ResolveVariant(settings *config.Settings, providerID, model string) Variant {
	inst := instanceFor(settings, providerID)
	if inst != nil {
		if mc, ok := lookupModel(inst, model); ok {
			if v := strings.TrimSpace(string(mc.PromptMode)); v != "" {
				return Variant(v)
			}
		}
		if v := strings.TrimSpace(string(inst.PromptMode)); v != "" {
			return Variant(v)
		}
	}
	if gp := globalSystemPrompt(settings); gp != nil {
		if v := strings.TrimSpace(gp.Variant); v != "" {
			return Variant(v)
		}
	}
	return DefaultVariant
}

func instanceFor(settings *config.Settings, providerID string) *config.ProviderInstanceConfig {
	if settings == nil {
		return nil
	}
	return settings.ProviderInstance(providerID)
}

func lookupModel(inst *config.ProviderInstanceConfig, model string) (config.ProviderModelConfig, bool) {
	model = strings.TrimSpace(model)
	if inst == nil || len(inst.Models) == 0 || model == "" {
		return config.ProviderModelConfig{}, false
	}
	mc, ok := inst.Models[model]
	return mc, ok
}

func globalSystemPrompt(settings *config.Settings) *config.SagittariusSystemPromptConfig {
	if settings == nil || settings.Sagittarius == nil {
		return nil
	}
	return settings.Sagittarius.SystemPrompt
}
