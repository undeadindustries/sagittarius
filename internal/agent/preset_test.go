package agent

import (
	"context"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/slash"
)

// makePresetTestApp returns a minimal App and modelsDialogDeps suitable for
// testing ApplySystemPromptPreset. The active provider is set to Gemini so that
// editing OpenAI model config does not trigger RebuildRunner (which needs
// credentials). This isolates the test to the field-write logic only.
func makePresetTestApp(t *testing.T) (*App, *modelsDialogDeps) {
	t.Helper()
	dir := t.TempDir()
	loader, err := config.NewLoader(config.WithSettingsPath(dir + "/settings.json"))
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}
	settings := &config.Settings{
		Providers: &config.ProvidersSettings{
			// Active provider is Gemini so rebuildIfActive is skipped when
			// editing OpenAI model config (active != edited provider).
			Active: string(config.BuiltInGeminiAPIKey),
			OpenAI: &config.ProviderInstanceConfig{
				ActiveModels: []string{"gpt-4o"},
				Model:        "gpt-4o",
			},
		},
	}
	runner, err := NewRunner(RunnerConfig{
		Generator:   &fakeGenerator{},
		Model:       "gpt-4o",
		WorkDir:     dir,
		Interactive: false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	app := NewApp(AppConfig{
		Runner:    runner,
		Loader:    loader,
		Settings:  settings,
		SessionID: "test",
	})
	// Override deps so the injected loader + settings are used by modelsDialogDeps.
	app.deps = slash.Deps{
		Loader:   loader,
		Settings: settings,
		Hooks:    &appHooks{app: app},
	}
	return app, &modelsDialogDeps{app: app}
}

// TestApplySystemPromptPresetWritesBothFields is the regression test for the
// Bugbot finding: the prior implementation stored the raw preset id (e.g.
// "programmer-lite") directly into the personality field, which SetModelConfig
// rejects for variant ids. The fix looks up the preset and writes personality +
// promptMode separately so both fields are stored correctly.
func TestApplySystemPromptPresetWritesBothFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		presetID        string
		wantPersonality string
		wantPromptMode  string
	}{
		{"programmer", config.PersonalityProgrammer, config.VariantFull},
		{"programmer-lite", config.PersonalityProgrammer, config.VariantLite},
		{"sysadmin-lite", config.PersonalitySysadmin, config.VariantLite},
	}

	for _, tt := range tests {
		t.Run(tt.presetID, func(t *testing.T) {
			_, deps := makePresetTestApp(t)

			msg, err := deps.ApplySystemPromptPreset(context.Background(), string(config.BuiltInOpenAI), "gpt-4o", tt.presetID)
			if err != nil {
				t.Fatalf("ApplySystemPromptPreset(%q): %v", tt.presetID, err)
			}
			if msg == "" {
				t.Error("expected non-empty confirmation message")
			}

			settings := deps.settings()
			vals := provider.ModelConfigValues(settings, string(config.BuiltInOpenAI), "gpt-4o")

			if got := vals["personality"]; got != tt.wantPersonality {
				t.Errorf("personality = %q, want %q", got, tt.wantPersonality)
			}
			if got := vals["promptMode"]; got != tt.wantPromptMode {
				t.Errorf("promptMode = %q, want %q", got, tt.wantPromptMode)
			}

			// Verify round-trip: SystemPromptPresetID recovers the correct preset id.
			if got := deps.SystemPromptPresetID(string(config.BuiltInOpenAI), "gpt-4o"); got != tt.presetID {
				t.Errorf("SystemPromptPresetID round-trip = %q, want %q", got, tt.presetID)
			}
		})
	}
}

func TestApplySystemPromptPresetUnknownIDErrors(t *testing.T) {
	t.Parallel()

	_, deps := makePresetTestApp(t)
	_, err := deps.ApplySystemPromptPreset(context.Background(), string(config.BuiltInOpenAI), "gpt-4o", "not-a-preset")
	if err == nil {
		t.Fatal("expected error for unknown preset id, got nil")
	}
}
