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
	// Curate openrouter's active set to a different model, so the pin's model
	// is known-absent (not merely unknown) and the override should be pruned.
	if err := SetActiveModels(settings, "openrouter", []string{"gpt-4o-mini"}); err != nil {
		t.Fatalf("SetActiveModels: %v", err)
	}
	PruneModeOverrides(settings)

	if settings.Sagittarius.Modes.Agent.Model != "" {
		t.Errorf("Agent mode override should be pruned, got %q", settings.Sagittarius.Modes.Agent.Model)
	}
	if settings.Sagittarius.Modes.Agent.Provider != "" {
		t.Errorf("Agent mode override provider should be cleared, got %q", settings.Sagittarius.Modes.Agent.Provider)
	}
}

func TestPruneModelOverridesSubagentSlots(t *testing.T) {
	settings := &config.Settings{
		Providers: &config.ProvidersSettings{
			Active: "gemini-apikey",
			GeminiAPIKey: &config.ProviderInstanceConfig{
				ActiveModels: []string{"gemini-pro-latest"},
			},
		},
		Sagittarius: &config.SagittariusSettings{
			Subagents: &config.SagittariusSubagents{
				Default:  config.SagittariusSubagentConfig{Provider: "gemini-apikey", Model: "gemini-pro-latest"},
				Research: &config.SagittariusSubagentClass{Provider: "gemini-apikey", Model: "stale-model"},
				Coding:   &config.SagittariusSubagentClass{Model: "also-stale"},
				Reviewer: &config.SagittariusSubagentClass{Provider: "removed-provider", Model: "x"},
			},
			Goal: &config.SagittariusGoalConfig{
				EvaluatorProvider: "gemini-apikey",
				EvaluatorModel:    "gemini-pro-latest",
			},
		},
	}

	if !PruneModelOverrides(settings) {
		t.Fatal("expected prune to report a change")
	}
	sub := settings.Sagittarius.Subagents
	if sub.Default.Model != "gemini-pro-latest" {
		t.Errorf("default pin should survive, got %+v", sub.Default)
	}
	if sub.Research.Model != "" || sub.Research.Provider != "" {
		t.Errorf("research pin should be cleared, got %+v", sub.Research)
	}
	if sub.Coding.Model != "" || sub.Coding.Provider != "" {
		t.Errorf("unqualified coding pin should be cleared, got %+v", sub.Coding)
	}
	if sub.Reviewer.Model != "" || sub.Reviewer.Provider != "" {
		t.Errorf("reviewer pin should be cleared, got %+v", sub.Reviewer)
	}
	if settings.Sagittarius.Goal.EvaluatorModel != "gemini-pro-latest" {
		t.Errorf("goal evaluator pin should survive, got %+v", settings.Sagittarius.Goal)
	}
}

func TestPruneModelOverridesGoalEvaluatorStale(t *testing.T) {
	settings := &config.Settings{
		Providers: &config.ProvidersSettings{
			Active: "gemini-apikey",
			GeminiAPIKey: &config.ProviderInstanceConfig{
				ActiveModels: []string{"gemini-pro-latest"},
			},
		},
		Sagittarius: &config.SagittariusSettings{
			Goal: &config.SagittariusGoalConfig{
				EvaluatorProvider: "openrouter",
				EvaluatorModel:    "qwen/qwen3.5-122b-a10b",
			},
		},
	}
	// Curate openrouter's active set to a different model, so the evaluator
	// pin's model is known-absent (not merely unknown) and should be pruned.
	// SetActiveModels prunes internally, so the pin is cleared by the set.
	if err := SetActiveModels(settings, "openrouter", []string{"gpt-4o-mini"}); err != nil {
		t.Fatalf("SetActiveModels: %v", err)
	}

	if settings.Sagittarius.Goal.EvaluatorModel != "" || settings.Sagittarius.Goal.EvaluatorProvider != "" {
		t.Errorf("stale goal evaluator pin should be cleared, got %+v", settings.Sagittarius.Goal)
	}
	// A second prune is a no-op now that the pin is gone.
	if PruneModelOverrides(settings) {
		t.Error("prune after the pin is gone should report no change")
	}
}

func TestPruneModelOverridesNoSagittarius(t *testing.T) {
	if PruneModelOverrides(nil) {
		t.Error("nil settings should not report a change")
	}
	if PruneModelOverrides(&config.Settings{}) {
		t.Error("settings without a sagittarius block should not report a change")
	}
}

// TestPruneModelOverridesKeepsPinWhenModelSetUnknown is the benchmark failure:
// a pin to a custom local provider with no curated active-model set and no
// configured default model was pruned at startup because ActiveModelsFor
// returned empty. An empty set means "unknown," not "gone."
func TestPruneModelOverridesKeepsPinWhenModelSetUnknown(t *testing.T) {
	settings := &config.Settings{
		Providers: &config.ProvidersSettings{
			Active: "gemini-apikey",
			Custom: map[string]config.CustomProviderDefinition{
				"GX10-01": {BaseURL: "http://127.0.0.1:8000/v1"},
			},
		},
		Sagittarius: &config.SagittariusSettings{
			Subagents: &config.SagittariusSubagents{
				Coding: &config.SagittariusSubagentClass{Provider: "GX10-01", Model: "qwen3.8-27b"},
			},
		},
	}

	if PruneModelOverrides(settings) {
		t.Fatal("pin to an uncurated provider must survive pruning")
	}
	if settings.Sagittarius.Subagents.Coding.Model != "qwen3.8-27b" {
		t.Errorf("coding pin = %+v, want qwen3.8-27b kept", settings.Sagittarius.Subagents.Coding)
	}
}
