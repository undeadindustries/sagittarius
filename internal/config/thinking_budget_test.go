package config

import (
	"encoding/json"
	"testing"
)

func TestResolveThinkingBudget(t *testing.T) {
	t.Parallel()

	geminiSettings := func(inst *ProviderInstanceConfig) *Settings {
		return &Settings{Providers: &ProvidersSettings{
			Active:       string(BuiltInGeminiAPIKey),
			GeminiAPIKey: inst,
		}}
	}
	withModel := func(mc ProviderModelConfig) *Settings {
		return geminiSettings(&ProviderInstanceConfig{
			Models: map[string]ProviderModelConfig{"gemini-2.5-pro": mc},
		})
	}
	tokens := func(n int) *int { return &n }
	flag := func(b bool) *bool { return &b }

	tests := []struct {
		name       string
		settings   *Settings
		model      string
		wantTokens int
		wantHard   bool
	}{
		{
			name:     "nil settings",
			settings: nil,
			model:    "gemini-2.5-pro",
		},
		{
			name:     "no provider instance",
			settings: geminiSettings(nil),
			model:    "gemini-2.5-pro",
		},
		{
			name:     "no model entry",
			settings: withModel(ProviderModelConfig{ThinkingBudgetTokens: tokens(4000)}),
			model:    "some-other-model",
		},
		{
			name:       "budget without hard flag is advisory only",
			settings:   withModel(ProviderModelConfig{ThinkingBudgetTokens: tokens(4000)}),
			model:      "gemini-2.5-pro",
			wantTokens: 4000,
		},
		{
			name: "budget with hard flag",
			settings: withModel(ProviderModelConfig{
				ThinkingBudgetTokens: tokens(2048),
				HardThinkingBudget:   flag(true),
			}),
			model:      "gemini-2.5-pro",
			wantTokens: 2048,
			wantHard:   true,
		},
		{
			name: "hard flag alone cannot cut anything",
			settings: withModel(ProviderModelConfig{
				HardThinkingBudget: flag(true),
			}),
			model: "gemini-2.5-pro",
		},
		{
			name: "zero budget disables both, hard flag notwithstanding",
			settings: withModel(ProviderModelConfig{
				ThinkingBudgetTokens: tokens(0),
				HardThinkingBudget:   flag(true),
			}),
			model: "gemini-2.5-pro",
		},
		{
			name: "negative budget is treated as unset",
			settings: withModel(ProviderModelConfig{
				ThinkingBudgetTokens: tokens(-1),
				HardThinkingBudget:   flag(true),
			}),
			model: "gemini-2.5-pro",
		},
		{
			name: "hard flag explicitly false",
			settings: withModel(ProviderModelConfig{
				ThinkingBudgetTokens: tokens(512),
				HardThinkingBudget:   flag(false),
			}),
			model:      "gemini-2.5-pro",
			wantTokens: 512,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotTokens, gotHard := ResolveThinkingBudget(
				tc.settings, string(BuiltInGeminiAPIKey), tc.model,
			)
			if gotTokens != tc.wantTokens || gotHard != tc.wantHard {
				t.Fatalf("ResolveThinkingBudget = (%d, %v), want (%d, %v)",
					gotTokens, gotHard, tc.wantTokens, tc.wantHard)
			}
		})
	}
}

func TestProviderModelConfigThinkingBudgetRoundTrip(t *testing.T) {
	t.Parallel()

	const raw = `{"thinkingBudgetTokens":3000,"hardThinkingBudget":true,"somethingUnknown":42}`

	var mc ProviderModelConfig
	if err := json.Unmarshal([]byte(raw), &mc); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if mc.ThinkingBudgetTokens == nil || *mc.ThinkingBudgetTokens != 3000 {
		t.Fatalf("ThinkingBudgetTokens = %v, want 3000", mc.ThinkingBudgetTokens)
	}
	if mc.HardThinkingBudget == nil || !*mc.HardThinkingBudget {
		t.Fatalf("HardThinkingBudget = %v, want true", mc.HardThinkingBudget)
	}

	out, err := json.Marshal(mc)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var back map[string]any
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatalf("Unmarshal round trip: %v", err)
	}
	if got := back["thinkingBudgetTokens"]; got != float64(3000) {
		t.Errorf("thinkingBudgetTokens = %v, want 3000", got)
	}
	if got := back["hardThinkingBudget"]; got != true {
		t.Errorf("hardThinkingBudget = %v, want true", got)
	}
	// An unknown key must survive, or hand-edited settings lose data on save.
	if got := back["somethingUnknown"]; got != float64(42) {
		t.Errorf("somethingUnknown = %v, want 42 (unknown keys must round-trip)", got)
	}
}

// TestProviderModelConfigMarshalOmitsUnsetBudget guards against the pointer
// fields emitting a misleading zero when the user never set them.
func TestProviderModelConfigMarshalOmitsUnsetBudget(t *testing.T) {
	t.Parallel()

	out, err := json.Marshal(ProviderModelConfig{})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if got := string(out); got != "{}" {
		t.Fatalf("Marshal(empty) = %s, want {}", got)
	}
}

func TestProviderModelConfigIsEmpty(t *testing.T) {
	t.Parallel()

	if !(ProviderModelConfig{}).IsEmpty() {
		t.Fatal("zero value should be empty")
	}
	budget := 100
	if (ProviderModelConfig{ThinkingBudgetTokens: &budget}).IsEmpty() {
		t.Fatal("a pinned thinkingBudgetTokens must keep the entry alive")
	}
	hard := false
	if (ProviderModelConfig{HardThinkingBudget: &hard}).IsEmpty() {
		t.Fatal("an explicit hardThinkingBudget=false must keep the entry alive")
	}
}
