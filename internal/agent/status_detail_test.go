package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/modes"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/session"
)

func TestSystemPromptStatusDetailPresetLabel(t *testing.T) {
	t.Parallel()
	settings := &config.Settings{
		Providers: &config.ProvidersSettings{
			Active: string(config.BuiltInOpenAI),
			OpenAI: &config.ProviderInstanceConfig{
				Personality: config.PersonalityProgrammer,
				PromptMode:  config.PromptMode(config.VariantLite),
			},
		},
	}
	got := systemPromptStatusDetail(nil, settings)
	if got != "System Prompt: Programmer (low context)" {
		t.Fatalf("got %q, want System Prompt: Programmer (low context)", got)
	}
}

func TestResolveSystemPromptLabelPrecedence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		settings *config.Settings
		model    string
		want     string
	}{
		{
			name: "provider override beats sagittarius.systemPrompt",
			settings: &config.Settings{
				Providers: &config.ProvidersSettings{
					Active: "anthropic",
					Custom: map[string]config.CustomProviderDefinition{
						"anthropic": {BaseURL: "https://api.anthropic.com/v1/messages", WireFormat: config.WireFormatAnthropic, DefaultModel: "claude-opus-5-5"},
					},
					Extra: map[string]json.RawMessage{
						"anthropic": json.RawMessage(`{"personality":"personal-assistant"}`),
					},
				},
				Sagittarius: &config.SagittariusSettings{
					SystemPrompt: &config.SagittariusSystemPromptConfig{
						Personality: config.PersonalityProgrammer,
						Variant:     config.VariantFull,
					},
				},
			},
			model: "claude-opus-5-5",
			want:  "Personal assistant",
		},
		{
			name: "per-model override beats provider and sagittarius.systemPrompt",
			settings: &config.Settings{
				Providers: &config.ProvidersSettings{
					Active: string(config.BuiltInOpenAI),
					OpenAI: &config.ProviderInstanceConfig{
						Personality: config.PersonalityPersonalAssistant,
						Models: map[string]config.ProviderModelConfig{
							"gpt-5": {Personality: config.PersonalitySysadmin, PromptMode: config.PromptModeLite},
						},
					},
				},
				Sagittarius: &config.SagittariusSettings{
					SystemPrompt: &config.SagittariusSystemPromptConfig{
						Personality: config.PersonalityProgrammer,
					},
				},
			},
			model: "gpt-5",
			want:  "System administrator (low context)",
		},
		{
			name: "sagittarius.systemPrompt applies when no provider/model override",
			settings: &config.Settings{
				Providers: &config.ProvidersSettings{
					Active: string(config.BuiltInGeminiAPIKey),
				},
				Sagittarius: &config.SagittariusSettings{
					SystemPrompt: &config.SagittariusSystemPromptConfig{
						Personality: config.PersonalitySysadmin,
						Variant:     config.VariantFull,
					},
				},
			},
			model: "gemini-4-carbon",
			want:  "System administrator",
		},
		{
			name: "custom prompt yields to provider personality override",
			settings: &config.Settings{
				Providers: &config.ProvidersSettings{
					Active: string(config.BuiltInOpenAI),
					OpenAI: &config.ProviderInstanceConfig{
						Personality: config.PersonalitySysadmin,
					},
				},
				Sagittarius: &config.SagittariusSettings{
					SystemPrompt: &config.SagittariusSystemPromptConfig{
						Custom: "reviewer",
					},
				},
			},
			model: "gpt-4o",
			want:  "System administrator",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r, err := NewRunner(RunnerConfig{
				Generator:   &fakeGenerator{},
				Model:       tc.model,
				WorkDir:     t.TempDir(),
				Settings:    tc.settings,
				ModelPinned: true,
			})
			if err != nil {
				t.Fatalf("NewRunner: %v", err)
			}
			got := resolveSystemPromptLabel(r, tc.settings)
			if got != tc.want {
				t.Fatalf("resolveSystemPromptLabel = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestNewAppInitialStatusUsesMergedAndRuntimeProvider guards Bug A: NewApp
// previously built status.Detail from cfg.Settings (= docs.Global), ignoring
// .sagittarius/settings.json and startup mode-provider overrides until a later
// RebuildRunner.
func TestNewAppInitialStatusUsesMergedAndRuntimeProvider(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SAGITTARIUS_HOME", home)
	projectDir := t.TempDir()

	docs, err := config.LoadDocuments(projectDir)
	if err != nil {
		t.Fatalf("LoadDocuments: %v", err)
	}
	docs.Global.Providers = &config.ProvidersSettings{
		Active: string(config.BuiltInGeminiAPIKey),
		Custom: map[string]config.CustomProviderDefinition{
			"anthropic": {BaseURL: "https://api.anthropic.com/v1/messages", WireFormat: config.WireFormatAnthropic, DefaultModel: "claude-opus-5-5"},
		},
		Extra: map[string]json.RawMessage{
			"anthropic": json.RawMessage(`{"personality":"sysadmin"}`),
		},
	}
	docs.Global.Sagittarius = &config.SagittariusSettings{
		SystemPrompt: &config.SagittariusSystemPromptConfig{
			Personality: config.PersonalityPersonalAssistant,
			Variant:     config.VariantFull,
		},
	}
	if err := docs.Save(config.ScopeGlobal); err != nil {
		t.Fatalf("save global: %v", err)
	}
	proj := docs.TargetSettings(config.ScopeProject)
	proj.Sagittarius = &config.SagittariusSettings{
		SystemPrompt: &config.SagittariusSystemPromptConfig{
			Personality: config.PersonalityProgrammer,
			Variant:     config.VariantFull,
		},
	}
	if err := docs.Save(config.ScopeProject); err != nil {
		t.Fatalf("save project: %v", err)
	}

	runner, err := NewRunner(RunnerConfig{
		Generator: &fakeGenerator{},
		Model:     "gemini-4-carbon",
		WorkDir:   projectDir,
		Settings:  docs.Merged(),
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	// Case 1: Project says Programmer while Global says Personal assistant.
	app := NewApp(AppConfig{
		Runner:         runner,
		Loader:         docs.Loader(),
		Settings:       docs.Global,
		Documents:      docs,
		ProviderLabel:  "gemini",
		Model:          "gemini-4-carbon",
		BaseProviderID: string(config.BuiltInGeminiAPIKey),
	})
	if got := app.Status().Detail; got != "System Prompt: Programmer" {
		t.Fatalf("startup Detail = %q, want %q", got, "System Prompt: Programmer")
	}

	// Case 2: RuntimeProviderID routes startup to anthropic (which pins sysadmin).
	appRuntime := NewApp(AppConfig{
		Runner:            runner,
		Loader:            docs.Loader(),
		Settings:          docs.Global,
		Documents:         docs,
		ProviderLabel:     "anthropic",
		Model:             "claude-opus-5-5",
		BaseProviderID:    string(config.BuiltInGeminiAPIKey),
		RuntimeProviderID: "anthropic",
	})
	if got := appRuntime.Status().Detail; got != "System Prompt: System administrator" {
		t.Fatalf("startup Detail with RuntimeProviderID = %q, want %q", got, "System Prompt: System administrator")
	}
}

func TestSetInteractionModeRefreshesStatusDetail(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SAGITTARIUS_HOME", home)
	projectDir := t.TempDir()

	docs, err := config.LoadDocuments(projectDir)
	if err != nil {
		t.Fatalf("LoadDocuments: %v", err)
	}
	docs.Global.Providers = &config.ProvidersSettings{
		Active: string(config.BuiltInOpenAI),
		OpenAI: &config.ProviderInstanceConfig{
			Model: "gpt-4o",
			Models: map[string]config.ProviderModelConfig{
				"gpt-4o":      {Personality: config.PersonalityProgrammer},
				"gpt-4o-mini": {Personality: config.PersonalitySysadmin},
			},
		},
	}
	docs.Global.Sagittarius = &config.SagittariusSettings{
		Modes: &config.SagittariusModes{
			Plan: &config.SagittariusModeConfig{Model: "gpt-4o-mini"},
		},
	}
	if err := docs.Save(config.ScopeGlobal); err != nil {
		t.Fatalf("save global: %v", err)
	}

	runner, err := NewRunner(RunnerConfig{
		Generator:   &fakeGenerator{},
		Model:       "gpt-4o",
		WorkDir:     projectDir,
		Settings:    docs.Merged(),
		InitialMode: modes.ModeAgent,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	app := NewApp(AppConfig{
		Runner:         runner,
		Loader:         docs.Loader(),
		Settings:       docs.Global,
		Documents:      docs,
		ProviderLabel:  "openai",
		Model:          "gpt-4o",
		BaseProviderID: string(config.BuiltInOpenAI),
	})
	if got := app.Status().Detail; got != "System Prompt: Programmer" {
		t.Fatalf("initial Detail = %q, want Programmer", got)
	}
	if _, err := app.deps.Hooks.SetInteractionMode(context.Background(), modes.ModePlan); err != nil {
		t.Fatalf("SetInteractionMode(plan): %v", err)
	}
	if got := app.Status().Detail; got != "System Prompt: System administrator" {
		t.Fatalf("plan Detail = %q, want System administrator", got)
	}
}

func TestApplySystemPromptPresetScopesClearAndShadowWarnings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SAGITTARIUS_HOME", home)
	t.Setenv("OPENAI_API_KEY", "test-key")
	projectDir := t.TempDir()

	docs, err := config.LoadDocuments(projectDir)
	if err != nil {
		t.Fatalf("LoadDocuments: %v", err)
	}
	docs.Global.Providers = &config.ProvidersSettings{
		Active: string(config.BuiltInOpenAI),
		OpenAI: &config.ProviderInstanceConfig{
			Model:       "gpt-4o",
			Personality: config.PersonalityPersonalAssistant,
		},
	}
	if err := docs.Save(config.ScopeGlobal); err != nil {
		t.Fatalf("save global: %v", err)
	}
	rec := session.NewRecorderWithConfig(t.TempDir(), "sess-sp", "hash", "main", session.RecorderConfig{
		PersonaPreset: "personal-assistant",
	})
	runner, err := NewRunner(RunnerConfig{
		Generator:       &fakeGenerator{},
		Model:           "gpt-4o",
		WorkDir:         projectDir,
		Settings:        docs.Merged(),
		SessionRecorder: rec,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	app := NewApp(AppConfig{
		Runner:         runner,
		Loader:         docs.Loader(),
		Settings:       docs.Global,
		Documents:      docs,
		ProviderLabel:  "openai",
		Model:          "gpt-4o",
		BaseProviderID: string(config.BuiltInOpenAI),
	})
	h := &appHooks{app: app}

	// 1. Applying programmer to project while openai pins personal-assistant
	// must warn about the provider override.
	msg, err := h.ApplySystemPromptPreset(context.Background(), "programmer", config.ScopeProject)
	if err != nil {
		t.Fatalf("ApplySystemPromptPreset(project): %v", err)
	}
	if !strings.Contains(msg, "provider openai has a System prompt override (personal-assistant)") {
		t.Fatalf("expected provider shadow warning, got: %q", msg)
	}
	if got := docs.Project.Sagittarius.SystemPrompt.Personality; got != config.PersonalityProgrammer {
		t.Fatalf("project personality = %q, want programmer", got)
	}

	// 2. Clear provider override, then save globally while project still overrides:
	// must warn that .sagittarius/settings.json shadows the global write.
	docs.Global.Providers.OpenAI.Personality = ""
	if err := docs.Save(config.ScopeGlobal); err != nil {
		t.Fatalf("save global: %v", err)
	}
	msg, err = h.ApplySystemPromptPreset(context.Background(), "sysadmin", config.ScopeGlobal)
	if err != nil {
		t.Fatalf("ApplySystemPromptPreset(global): %v", err)
	}
	if !strings.Contains(msg, ".sagittarius/settings.json still sets a project system prompt") {
		t.Fatalf("expected project shadow warning, got: %q", msg)
	}
	if got := docs.Global.Sagittarius.SystemPrompt.Personality; got != config.PersonalitySysadmin {
		t.Fatalf("global personality = %q, want sysadmin", got)
	}

	// 3. Clearing project scope lets the global sysadmin take effect cleanly.
	msg, err = h.ApplySystemPromptPreset(context.Background(), "", config.ScopeProject)
	if err != nil {
		t.Fatalf("ApplySystemPromptPreset(clear project): %v", err)
	}
	if !strings.Contains(msg, "System administrator") || !strings.Contains(msg, "cleared in .sagittarius/settings.json") {
		t.Fatalf("clear message = %q", msg)
	}
	if docs.Project.Sagittarius != nil && docs.Project.Sagittarius.SystemPrompt != nil {
		t.Fatalf("project SystemPrompt should be cleared, got %+v", docs.Project.Sagittarius.SystemPrompt)
	}
	if got := app.Status().Detail; got != "System Prompt: System administrator" {
		t.Fatalf("Detail after clear = %q, want System administrator", got)
	}
	if got := rec.PersonaPreset(); got != "sysadmin" {
		t.Fatalf("recorder PersonaPreset after mid-session /system-prompt = %q, want sysadmin", got)
	}
}

func TestModesDialogDepsReadsMergedActiveModels(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SAGITTARIUS_HOME", home)
	projectDir := t.TempDir()

	docs, err := config.LoadDocuments(projectDir)
	if err != nil {
		t.Fatalf("LoadDocuments: %v", err)
	}
	docs.Global.Providers = &config.ProvidersSettings{
		Active: string(config.BuiltInGeminiAPIKey),
		GeminiAPIKey: &config.ProviderInstanceConfig{
			ActiveModels: []string{"gemini-2.5-pro"},
		},
	}
	if err := docs.Save(config.ScopeGlobal); err != nil {
		t.Fatalf("save global: %v", err)
	}
	proj := docs.TargetSettings(config.ScopeProject)
	proj.Providers = &config.ProvidersSettings{
		GeminiAPIKey: &config.ProviderInstanceConfig{
			ActiveModels: []string{"gemini-4-carbon"},
		},
	}
	if err := docs.Save(config.ScopeProject); err != nil {
		t.Fatalf("save project: %v", err)
	}

	app := NewApp(AppConfig{
		Loader:    docs.Loader(),
		Settings:  docs.Global,
		Documents: docs,
	})
	entries := app.ModesDialogDeps().AllActiveModels()
	found := false
	for _, e := range entries {
		if e.Model == "gemini-4-carbon" {
			found = true
		}
		if e.Model == "gemini-2.5-pro" {
			t.Fatalf("modes dialog returned global-only model %q instead of merged project activeModels", e.Model)
		}
	}
	if !found {
		t.Fatalf("modes dialog missing project active model gemini-4-carbon; got %+v", entries)
	}
	_ = provider.AllActiveModels // keep package reference clear
}

// TestProjectSetActiveModelsPreservesSiblingProviderPins guards Bugbot #4:
// curating models on one provider in ScopeProject must prune against the
// merged catalog so project-scoped mode/subagent pins referencing global
// curated models on sibling providers are never wiped.
func TestProjectSetActiveModelsPreservesSiblingProviderPins(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SAGITTARIUS_HOME", home)
	projectDir := t.TempDir()

	docs, err := config.LoadDocuments(projectDir)
	if err != nil {
		t.Fatalf("LoadDocuments: %v", err)
	}
	docs.Global.Providers = &config.ProvidersSettings{
		Active: string(config.BuiltInGeminiAPIKey),
		GeminiAPIKey: &config.ProviderInstanceConfig{
			ActiveModels: []string{"gemini-4-carbon"},
		},
		Custom: map[string]config.CustomProviderDefinition{
			"anthropic": {BaseURL: "https://api.anthropic.com/v1/messages", WireFormat: config.WireFormatAnthropic, DefaultModel: "claude-opus-5-5"},
		},
	}
	if err := provider.SetActiveModels(docs.Global, "anthropic", []string{"claude-opus-5-5", "claude-sonnet-4-6"}); err != nil {
		t.Fatal(err)
	}
	if err := docs.Save(config.ScopeGlobal); err != nil {
		t.Fatal(err)
	}

	proj := docs.TargetSettings(config.ScopeProject)
	proj.Sagittarius = &config.SagittariusSettings{
		Modes: &config.SagittariusModes{
			Plan: &config.SagittariusModeConfig{Provider: "anthropic", Model: "claude-sonnet-4-6"},
			Ask:  &config.SagittariusModeConfig{Provider: "gemini-apikey", Model: "gemini-4-carbon"},
		},
	}
	if err := docs.Save(config.ScopeProject); err != nil {
		t.Fatal(err)
	}

	app := NewApp(AppConfig{Loader: docs.Loader(), Settings: docs.Global, Documents: docs})
	deps := app.ProviderDialogDeps()
	// Narrow gemini-apikey active models in ScopeProject to gemini-4-argon:
	// the stale ask pin (gemini-4-carbon) should be pruned, while the valid
	// sibling plan pin (anthropic/claude-sonnet-4-6) must survive!
	if err := deps.SetActiveModels(context.Background(), string(config.BuiltInGeminiAPIKey), []string{"gemini-4-argon"}, config.ScopeProject); err != nil {
		t.Fatalf("SetActiveModels(project): %v", err)
	}
	if got := docs.Project.Sagittarius.Modes.Plan.Model; got != "claude-sonnet-4-6" {
		t.Fatalf("sibling anthropic plan pin wiped by gemini project curation: got %q", got)
	}
	if got := docs.Project.Sagittarius.Modes.Ask.Model; got != "" {
		t.Fatalf("stale gemini ask pin should be pruned, got %q", got)
	}
}
