package provider

import (
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
)

func openaiSettings(inst *config.ProviderInstanceConfig) *config.Settings {
	return &config.Settings{Providers: &config.ProvidersSettings{
		Active: string(config.BuiltInOpenAI),
		OpenAI: inst,
	}}
}

func TestApplySystemPromptPresetWritesPersonalityAndVariant(t *testing.T) {
	t.Parallel()
	s := openaiSettings(&config.ProviderInstanceConfig{})
	res, err := ApplySystemPromptPreset(s, string(config.BuiltInOpenAI), "creative-assistant-lite")
	if err != nil {
		t.Fatalf("apply preset: %v", err)
	}
	inst := s.Providers.OpenAI
	if inst.Personality != config.PersonalityCreativeAssistant {
		t.Errorf("personality = %q, want %q", inst.Personality, config.PersonalityCreativeAssistant)
	}
	if string(inst.PromptMode) != config.VariantLite {
		t.Errorf("promptMode = %q, want %q", inst.PromptMode, config.VariantLite)
	}
	if res.DefaultTemperature == nil {
		t.Fatal("creative assistant should report a default temperature")
	}
	if res.TemperaturePinned {
		t.Error("temperature should not be pinned on a fresh instance")
	}
	if res.CompressionThreshold != config.VariantCompressionThreshold(config.VariantLite) {
		t.Errorf("compression = %v, want lite default", res.CompressionThreshold)
	}
}

func TestApplySystemPromptPresetReportsPins(t *testing.T) {
	t.Parallel()
	temp := 0.1
	comp := 0.9
	s := openaiSettings(&config.ProviderInstanceConfig{Temperature: &temp, CompressionThreshold: &comp})
	res, err := ApplySystemPromptPreset(s, string(config.BuiltInOpenAI), "programmer")
	if err != nil {
		t.Fatalf("apply preset: %v", err)
	}
	if !res.TemperaturePinned || !res.CompressionPinned {
		t.Errorf("expected both knobs pinned: temp=%v comp=%v", res.TemperaturePinned, res.CompressionPinned)
	}
	// Applying a preset must not clobber the user's pinned knobs.
	if s.Providers.OpenAI.Temperature == nil || *s.Providers.OpenAI.Temperature != 0.1 {
		t.Error("preset apply overwrote pinned temperature")
	}
}

func TestCurrentSystemPromptPresetRoundTrip(t *testing.T) {
	t.Parallel()
	s := openaiSettings(&config.ProviderInstanceConfig{})
	if _, err := ApplySystemPromptPreset(s, string(config.BuiltInOpenAI), "sysadmin-lite"); err != nil {
		t.Fatalf("apply preset: %v", err)
	}
	if got := CurrentSystemPromptPreset(s, string(config.BuiltInOpenAI)); got != "sysadmin-lite" {
		t.Errorf("current preset = %q, want sysadmin-lite", got)
	}
}

func TestClearProviderSettingRevertsField(t *testing.T) {
	t.Parallel()
	temp := 0.42
	s := openaiSettings(&config.ProviderInstanceConfig{Temperature: &temp, Personality: config.PersonalitySysadmin})
	if err := ClearProviderSetting(s, string(config.BuiltInOpenAI), "temperature"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if s.Providers.OpenAI.Temperature != nil {
		t.Error("temperature override was not cleared")
	}
	if s.Providers.OpenAI.Personality != config.PersonalitySysadmin {
		t.Error("clearing temperature must not touch personality")
	}
	if err := ClearProviderSetting(s, string(config.BuiltInOpenAI), "bogus"); err == nil {
		t.Error("expected error for unsupported key")
	}
}

func TestResetProviderInstanceOverridesPreservesStructural(t *testing.T) {
	t.Parallel()
	temp := 0.42
	s := openaiSettings(&config.ProviderInstanceConfig{
		Model:        "gpt-4o",
		Personality:  config.PersonalityCreativeAssistant,
		Temperature:  &temp,
		ActiveModels: []string{"gpt-4o", "gpt-4o-mini"},
	})
	if err := ResetProviderInstanceOverrides(s, string(config.BuiltInOpenAI)); err != nil {
		t.Fatalf("reset: %v", err)
	}
	inst := s.Providers.OpenAI
	if inst.Personality != "" || inst.Temperature != nil {
		t.Error("behavioral overrides not cleared")
	}
	if inst.Model != "gpt-4o" {
		t.Errorf("model should be preserved, got %q", inst.Model)
	}
	if len(inst.ActiveModels) != 2 {
		t.Errorf("activeModels should be preserved, got %v", inst.ActiveModels)
	}
}
