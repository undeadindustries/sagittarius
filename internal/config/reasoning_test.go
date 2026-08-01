package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestModelReasoningRule(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		wireFormat WireFormat
		model      string
		wantMatch  bool
		wantMech   ReasoningMechanism
		wantDyn    bool
		wantMand   bool
		wantEffort []string
	}{
		{"gemini-3 dynamic", WireFormatGemini, "gemini-3-pro", true, ReasoningMechanismGeminiDynamic, true, false, []string{"minimal", "low", "medium", "high"}},
		{"gemini-2.5 dynamic", WireFormatGemini, "models/gemini-2.5-flash", true, ReasoningMechanismGeminiDynamic, true, false, []string{"minimal", "low", "medium", "high"}},
		{"gemini-1.5 unmatched", WireFormatGemini, "gemini-1.5-pro", false, ReasoningMechanismNone, false, false, nil},
		{"gpt-5-pro mandatory", WireFormatOpenAIResponses, "gpt-5-pro", true, ReasoningMechanismFixedEffort, false, true, []string{"high"}},
		{"gpt-5.4 xhigh family", WireFormatOpenAIResponses, "gpt-5.4-turbo", true, ReasoningMechanismFixedEffort, false, false, []string{"none", "low", "medium", "high", "xhigh"}},
		{"gpt-5.1 none-capable family", WireFormatOpenAIResponses, "gpt-5.1", true, ReasoningMechanismFixedEffort, false, false, []string{"none", "low", "medium", "high"}},
		{"gpt-5 base family", WireFormatOpenAIResponses, "gpt-5", true, ReasoningMechanismFixedEffort, false, false, []string{"minimal", "low", "medium", "high"}},
		{"o3 family", WireFormatOpenAIResponses, "o3-mini", true, ReasoningMechanismFixedEffort, false, false, []string{"low", "medium", "high"}},
		{"unmatched responses model", WireFormatOpenAIResponses, "text-davinci-003", false, ReasoningMechanismNone, false, false, nil},
		{"openai-chat never matches statically", WireFormatOpenAIChat, "openai/gpt-5", false, ReasoningMechanismNone, false, false, nil},
		{"empty model", WireFormatGemini, "", false, ReasoningMechanismNone, false, false, nil},
		{"case-insensitive + provider-prefixed", WireFormatGemini, "Google/GEMINI-3-Flash", true, ReasoningMechanismGeminiDynamic, true, false, []string{"minimal", "low", "medium", "high"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			profile, matched := ModelReasoningRule(tc.wireFormat, tc.model)
			if matched != tc.wantMatch {
				t.Fatalf("matched = %v, want %v", matched, tc.wantMatch)
			}
			if !matched {
				return
			}
			if profile.Mechanism != tc.wantMech {
				t.Errorf("mechanism = %q, want %q", profile.Mechanism, tc.wantMech)
			}
			if profile.SupportsDynamic != tc.wantDyn {
				t.Errorf("SupportsDynamic = %v, want %v", profile.SupportsDynamic, tc.wantDyn)
			}
			if profile.Mandatory != tc.wantMand {
				t.Errorf("Mandatory = %v, want %v", profile.Mandatory, tc.wantMand)
			}
			if len(profile.ValidEfforts) != len(tc.wantEffort) {
				t.Fatalf("ValidEfforts = %v, want %v", profile.ValidEfforts, tc.wantEffort)
			}
			for i, e := range tc.wantEffort {
				if profile.ValidEfforts[i] != e {
					t.Errorf("ValidEfforts[%d] = %q, want %q", i, profile.ValidEfforts[i], e)
				}
			}
		})
	}
}

func TestResolveReasoningRequestPrecedence(t *testing.T) {
	t.Parallel()

	openAISettings := func(inst *ProviderInstanceConfig) *Settings {
		return &Settings{Providers: &ProvidersSettings{Active: string(BuiltInOpenAIResponses), OpenAIResponses: inst}}
	}

	t.Run("session override wins over everything", func(t *testing.T) {
		t.Parallel()
		s := openAISettings(&ProviderInstanceConfig{ReasoningEffort: "low"})
		got := ResolveReasoningRequest(s, string(BuiltInOpenAIResponses), "gpt-5", "high")
		if got == nil || got.Effort != "high" {
			t.Fatalf("got %#v, want effort=high", got)
		}
	})

	t.Run("pinned model-level effort beats provider-level effort", func(t *testing.T) {
		t.Parallel()
		s := openAISettings(&ProviderInstanceConfig{
			ReasoningEffort: "low",
			Models:          map[string]ProviderModelConfig{"gpt-5": {ReasoningEffort: "high"}},
		})
		got := ResolveReasoningRequest(s, string(BuiltInOpenAIResponses), "gpt-5", "")
		if got == nil || got.Effort != "high" {
			t.Fatalf("got %#v, want effort=high", got)
		}
	})

	t.Run("pinned provider-level effort used when no model override", func(t *testing.T) {
		t.Parallel()
		s := openAISettings(&ProviderInstanceConfig{ReasoningEffort: "medium"})
		got := ResolveReasoningRequest(s, string(BuiltInOpenAIResponses), "gpt-5", "")
		if got == nil || got.Effort != "medium" {
			t.Fatalf("got %#v, want effort=medium", got)
		}
	})

	t.Run("gemini dynamic default when no pin", func(t *testing.T) {
		t.Parallel()
		s := &Settings{Providers: &ProvidersSettings{Active: string(BuiltInGeminiAPIKey), GeminiAPIKey: &ProviderInstanceConfig{}}}
		got := ResolveReasoningRequest(s, string(BuiltInGeminiAPIKey), "gemini-3-pro", "")
		if got == nil || got.Effort != "" || !got.Enabled {
			t.Fatalf("got %#v, want enabled dynamic (empty effort)", got)
		}
	})

	t.Run("openai-responses family default omits field", func(t *testing.T) {
		t.Parallel()
		s := openAISettings(&ProviderInstanceConfig{})
		got := ResolveReasoningRequest(s, string(BuiltInOpenAIResponses), "gpt-5", "")
		if got != nil {
			t.Fatalf("got %#v, want nil (let provider apply its own default)", got)
		}
	})

	t.Run("discovered openrouter capability enables reasoning", func(t *testing.T) {
		t.Parallel()
		s := &Settings{Providers: &ProvidersSettings{
			Active: "openrouter",
			Custom: map[string]CustomProviderDefinition{
				"openrouter": {WireFormat: WireFormatOpenAIChat},
			},
			Extra: map[string]json.RawMessage{
				"openrouter": json.RawMessage(`{"models":{"anthropic/claude-4":{"reasoningSupported":true}}}`),
			},
		}}
		got := ResolveReasoningRequest(s, "openrouter", "anthropic/claude-4", "")
		if got == nil || !got.Enabled || got.Effort != "" {
			t.Fatalf("got %#v, want enabled with no pinned effort", got)
		}
	})

	t.Run("openrouter model without discovered support sends nothing", func(t *testing.T) {
		t.Parallel()
		s := &Settings{Providers: &ProvidersSettings{
			Active: "openrouter",
			Custom: map[string]CustomProviderDefinition{
				"openrouter": {WireFormat: WireFormatOpenAIChat},
			},
		}}
		got := ResolveReasoningRequest(s, "openrouter", "meta/llama-3", "")
		if got != nil {
			t.Fatalf("got %#v, want nil", got)
		}
	})

	t.Run("nil settings resolves to nil", func(t *testing.T) {
		t.Parallel()
		if got := ResolveReasoningRequest(nil, "openrouter", "any", ""); got != nil {
			t.Fatalf("got %#v, want nil", got)
		}
	})
}

func TestDescribeReasoningCapability(t *testing.T) {
	t.Parallel()

	t.Run("gemini dynamic", func(t *testing.T) {
		t.Parallel()
		s := &Settings{Providers: &ProvidersSettings{Active: string(BuiltInGeminiAPIKey), GeminiAPIKey: &ProviderInstanceConfig{}}}
		got := DescribeReasoningCapability(s, string(BuiltInGeminiAPIKey), "gemini-3-pro")
		if !strings.Contains(got, "adaptive") {
			t.Errorf("got %q, want mention of adaptive", got)
		}
	})

	t.Run("fixed effort mandatory", func(t *testing.T) {
		t.Parallel()
		s := &Settings{Providers: &ProvidersSettings{Active: string(BuiltInOpenAIResponses), OpenAIResponses: &ProviderInstanceConfig{}}}
		got := DescribeReasoningCapability(s, string(BuiltInOpenAIResponses), "gpt-5-pro")
		if !strings.Contains(got, "mandatory") || !strings.Contains(got, "high") {
			t.Errorf("got %q, want mandatory + high", got)
		}
	})

	t.Run("fixed effort non-mandatory", func(t *testing.T) {
		t.Parallel()
		s := &Settings{Providers: &ProvidersSettings{Active: string(BuiltInOpenAIResponses), OpenAIResponses: &ProviderInstanceConfig{}}}
		got := DescribeReasoningCapability(s, string(BuiltInOpenAIResponses), "gpt-5")
		if !strings.Contains(got, "adaptive by default") || strings.Contains(got, "mandatory") {
			t.Errorf("got %q, want adaptive-by-default, no mandatory", got)
		}
	})

	t.Run("openrouter discovered supported", func(t *testing.T) {
		t.Parallel()
		s := &Settings{Providers: &ProvidersSettings{
			Active: "openrouter",
			Custom: map[string]CustomProviderDefinition{"openrouter": {WireFormat: WireFormatOpenAIChat}},
			Extra: map[string]json.RawMessage{
				"openrouter": json.RawMessage(`{"models":{"anthropic/claude-4":{"reasoningSupported":true}}}`),
			},
		}}
		got := DescribeReasoningCapability(s, "openrouter", "anthropic/claude-4")
		if !strings.Contains(got, "enabled") {
			t.Errorf("got %q, want mention of enabled", got)
		}
	})

	t.Run("openrouter discovered unsupported", func(t *testing.T) {
		t.Parallel()
		s := &Settings{Providers: &ProvidersSettings{
			Active: "openrouter",
			Custom: map[string]CustomProviderDefinition{"openrouter": {WireFormat: WireFormatOpenAIChat}},
			Extra: map[string]json.RawMessage{
				"openrouter": json.RawMessage(`{"models":{"meta/llama-3":{"reasoningSupported":false}}}`),
			},
		}}
		got := DescribeReasoningCapability(s, "openrouter", "meta/llama-3")
		if !strings.Contains(got, "not offered") {
			t.Errorf("got %q, want 'not offered'", got)
		}
	})

	t.Run("unknown model", func(t *testing.T) {
		t.Parallel()
		s := &Settings{Providers: &ProvidersSettings{
			Active: "openrouter",
			Custom: map[string]CustomProviderDefinition{"openrouter": {WireFormat: WireFormatOpenAIChat}},
		}}
		got := DescribeReasoningCapability(s, "openrouter", "meta/llama-3")
		if !strings.Contains(got, "not supported") {
			t.Errorf("got %q, want 'not supported'", got)
		}
	})

	t.Run("nil settings does not panic", func(t *testing.T) {
		t.Parallel()
		got := DescribeReasoningCapability(nil, "openrouter", "any")
		if got == "" {
			t.Error("expected a non-empty fallback description")
		}
	})
}

func TestProviderModelConfigReasoningCapabilityRoundTrip(t *testing.T) {
	t.Parallel()
	tru := true
	fls := false
	mc := ProviderModelConfig{ReasoningSupported: &tru, ReasoningMandatory: &fls}
	b, err := json.Marshal(mc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got ProviderModelConfig
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ReasoningSupported == nil || !*got.ReasoningSupported {
		t.Fatalf("ReasoningSupported did not round-trip: %#v", got.ReasoningSupported)
	}
	if got.ReasoningMandatory == nil || *got.ReasoningMandatory {
		t.Fatalf("ReasoningMandatory did not round-trip: %#v", got.ReasoningMandatory)
	}
}
