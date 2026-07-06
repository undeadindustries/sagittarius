package provider

import (
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
)

func TestPruneModeOverridesUnqualified(t *testing.T) {
	// Setup settings with gemini-apikey as active
	settings := &config.Settings{
		Providers: &config.ProvidersSettings{
			Active: "gemini-apikey",
			GeminiAPIKey: &config.ProviderInstanceConfig{
				ActiveModels: []string{"gemini-pro-latest"},
			},
		},
		Sagittarius: &config.SagittariusSettings{
			Modes: &config.SagittariusModes{
				Agent: &config.SagittariusModeConfig{
					Model: "qwen/qwen3.5-122b-a10b",
					// Provider is intentionally empty
				},
				Plan: &config.SagittariusModeConfig{
					Model: "gemini-pro-latest",
					// Provider is intentionally empty
				},
			},
		},
	}

	PruneModeOverrides(settings)

	if settings.Sagittarius.Modes.Agent.Model != "" {
		t.Errorf("Agent mode override should be pruned, got %q", settings.Sagittarius.Modes.Agent.Model)
	}
	if settings.Sagittarius.Modes.Plan.Model != "gemini-pro-latest" {
		t.Errorf("Plan mode override should survive, got %q", settings.Sagittarius.Modes.Plan.Model)
	}
}

func TestPruneModeOverridesQualified(t *testing.T) {
	settings := &config.Settings{
		Providers: &config.ProvidersSettings{
			Active: "gemini-apikey",
			GeminiAPIKey: &config.ProviderInstanceConfig{
				ActiveModels: []string{"gemini-pro-latest"},
			},
		},
		Sagittarius: &config.SagittariusSettings{
			Modes: &config.SagittariusModes{
				Agent: &config.SagittariusModeConfig{
					Provider: "openrouter",
					Model:    "qwen/qwen3.5-122b-a10b",
				},
			},
		},
	}
	// openrouter is a preset, but it has no active models explicitly set.
	// Its fallback default model is "gpt-4o-mini", not "qwen...".
	// So this override should be pruned.
	PruneModeOverrides(settings)

	if settings.Sagittarius.Modes.Agent.Model != "" {
		t.Errorf("Agent mode override should be pruned, got %q", settings.Sagittarius.Modes.Agent.Model)
	}
	if settings.Sagittarius.Modes.Agent.Provider != "" {
		t.Errorf("Agent mode override provider should be cleared, got %q", settings.Sagittarius.Modes.Agent.Provider)
	}
}
