package config

import (
	"encoding/json"
	"testing"
)

func TestResolveShowThinkingPrecedence(t *testing.T) {
	t.Parallel()
	tru := true
	fls := false

	cases := []struct {
		name     string
		settings *Settings
		model    string
		want     bool
	}{
		{"nothing set", &Settings{}, "m", false},
		{
			name:     "global only",
			settings: &Settings{Raw: map[string]json.RawMessage{"ui": json.RawMessage(`{"showThinking":true}`)}},
			model:    "m",
			want:     true,
		},
		{
			name: "provider beats global",
			settings: &Settings{
				Raw:       map[string]json.RawMessage{"ui": json.RawMessage(`{"showThinking":false}`)},
				Providers: &ProvidersSettings{OpenAI: &ProviderInstanceConfig{ShowThinking: &tru}},
			},
			model: "m",
			want:  true,
		},
		{
			name: "model beats provider",
			settings: &Settings{
				Providers: &ProvidersSettings{OpenAI: &ProviderInstanceConfig{
					ShowThinking: &tru,
					Models:       map[string]ProviderModelConfig{"m-off": {ShowThinking: &fls}},
				}},
			},
			model: "m-off",
			want:  false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveShowThinking(tc.settings, "openai", tc.model); got != tc.want {
				t.Fatalf("ResolveShowThinking = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSetUIShowThinking(t *testing.T) {
	t.Parallel()
	s := &Settings{}
	if err := s.SetUIShowThinking(true); err != nil {
		t.Fatalf("SetUIShowThinking(true): %v", err)
	}
	if !s.UI().ShowThinking {
		t.Fatal("expected showThinking true after set")
	}
	// Clearing removes the key but preserves other ui.* values.
	s.Raw["ui"] = json.RawMessage(`{"showThinking":true,"hideBanner":true}`)
	if err := s.SetUIShowThinking(false); err != nil {
		t.Fatalf("SetUIShowThinking(false): %v", err)
	}
	ui := s.UI()
	if ui.ShowThinking {
		t.Fatal("expected showThinking cleared")
	}
	if !ui.HideBanner {
		t.Fatal("clearing showThinking must preserve hideBanner")
	}
}

func TestProviderModelConfigShowThinkingRoundTrip(t *testing.T) {
	t.Parallel()
	tru := true
	mc := ProviderModelConfig{ShowThinking: &tru}
	b, err := json.Marshal(mc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got ProviderModelConfig
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ShowThinking == nil || !*got.ShowThinking {
		t.Fatalf("showThinking did not round-trip: %#v", got.ShowThinking)
	}
}

func TestProviderInstanceShowThinkingRoundTrip(t *testing.T) {
	t.Parallel()
	fls := false
	raw, err := marshalProviderInstance(&ProviderInstanceConfig{ShowThinking: &fls})
	if err != nil {
		t.Fatalf("marshal instance: %v", err)
	}
	got, err := unmarshalProviderInstance(raw)
	if err != nil {
		t.Fatalf("unmarshal instance: %v", err)
	}
	if got.ShowThinking == nil || *got.ShowThinking {
		t.Fatalf("instance showThinking did not round-trip: %#v", got.ShowThinking)
	}
}
