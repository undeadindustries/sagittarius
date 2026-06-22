package config

import "testing"

func TestModelTemperatureRule(t *testing.T) {
	t.Parallel()
	cases := []struct {
		model       string
		wantMatched bool
		wantOmit    bool
		wantVal     *float64
	}{
		{"gemini-3-pro", true, true, nil},
		{"google/gemini-3-pro-preview", true, true, nil},
		{"gemini-2.5-flash", true, true, nil},
		{"gpt-5-codex", true, true, nil},
		{"openai/gpt-5", true, true, nil},
		{"o3-mini", true, true, nil},
		{"o4-mini", true, true, nil},
		{"anthropic/claude-opus-4.8", true, true, nil},
		{"qwen3-coder-next", true, false, floatPtr(1.0)},
		{"gpt-4o", false, false, nil},
		{"gpt-4o-mini", false, false, nil},
		{"", false, false, nil},
	}
	for _, tc := range cases {
		temp, omit, matched := ModelTemperatureRule(tc.model)
		if matched != tc.wantMatched || omit != tc.wantOmit {
			t.Errorf("%q: matched=%v omit=%v, want matched=%v omit=%v", tc.model, matched, omit, tc.wantMatched, tc.wantOmit)
		}
		if (temp == nil) != (tc.wantVal == nil) || (temp != nil && tc.wantVal != nil && *temp != *tc.wantVal) {
			t.Errorf("%q: temp=%v, want %v", tc.model, temp, tc.wantVal)
		}
	}
}

func TestResolveEffectiveTemperature(t *testing.T) {
	t.Parallel()
	pin := floatPtr(0.7)
	settings := &Settings{
		Providers: &ProvidersSettings{
			Active: "openai",
			OpenAI: &ProviderInstanceConfig{Temperature: pin},
		},
	}
	// User pin wins even for an omit-family model.
	if got := ResolveEffectiveTemperature(settings, "openai", "gpt-5-codex"); got == nil || *got != 0.7 {
		t.Fatalf("user pin should win: got %v", got)
	}

	// No pin: omit family -> nil.
	noPin := &Settings{Providers: &ProvidersSettings{Active: "openai", OpenAI: &ProviderInstanceConfig{}}}
	if got := ResolveEffectiveTemperature(noPin, "openai", "gemini-2.5-flash"); got != nil {
		t.Fatalf("omit family should yield nil: got %v", got)
	}

	// No pin, no family opinion -> programmer personality default 0.35.
	if got := ResolveEffectiveTemperature(noPin, "openai", "gpt-4o"); got == nil || *got != 0.35 {
		t.Fatalf("programmer default expected: got %v", got)
	}

	// Creative assistant default for a generic model.
	creative := &Settings{Providers: &ProvidersSettings{
		Active:  "openai",
		OpenAI:  &ProviderInstanceConfig{Personality: PersonalityCreativeAssistant},
	}}
	if got := ResolveEffectiveTemperature(creative, "openai", "gpt-4o"); got == nil || *got != 0.85 {
		t.Fatalf("creative default expected: got %v", got)
	}
}
