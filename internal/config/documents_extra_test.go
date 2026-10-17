package config

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// captureWarnings redirects slog to a buffer for the duration of the test.
func captureWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// TestMergeProvidersExtraOverlaysGlobalFields is the behavior AD-117 depends on:
// a project block that names only a model must not erase the global endpoint, or
// selecting a model in project scope breaks the provider it selected.
func TestMergeProvidersExtraOverlaysGlobalFields(t *testing.T) {
	global := map[string]json.RawMessage{
		"gx10": json.RawMessage(`{"baseUrl":"http://gx10:8000/v1","contextLimit":131072,"model":"qwen"}`),
	}
	project := map[string]json.RawMessage{
		"gx10": json.RawMessage(`{"model":"gemma"}`),
	}

	var got ProviderInstanceConfig
	if err := json.Unmarshal(mergeProvidersExtra(global, project)["gx10"], &got); err != nil {
		t.Fatalf("unmarshal merged block: %v", err)
	}
	if got.Model != "gemma" {
		t.Errorf("Model = %q, want the project override gemma", got.Model)
	}
	if got.BaseURL != "http://gx10:8000/v1" {
		t.Errorf("BaseURL = %q, want the global value preserved", got.BaseURL)
	}
}

// TestMergeProvidersExtraWarnsOnMalformedBlock covers an undecodable project
// block. It is dropped so the global block survives intact (passing it through
// would skip the connection-field check), and the drop is logged.
func TestMergeProvidersExtraWarnsOnMalformedBlock(t *testing.T) {
	logs := captureWarnings(t)

	global := map[string]json.RawMessage{
		"gx10": json.RawMessage(`{"baseUrl":"http://gx10:8000/v1"}`),
	}
	project := map[string]json.RawMessage{
		"gx10": json.RawMessage(`{"model":`), // truncated: decode fails
	}

	merged := mergeProvidersExtra(global, project)
	if string(merged["gx10"]) != `{"baseUrl":"http://gx10:8000/v1"}` {
		t.Errorf("merged block = %s, want the global block unchanged", merged["gx10"])
	}

	out := logs.String()
	if !strings.Contains(out, "level=WARN") {
		t.Fatalf("no warning was logged for a malformed block:\n%s", out)
	}
	if !strings.Contains(out, "gx10") {
		t.Errorf("warning does not name the provider, so it is not actionable:\n%s", out)
	}
}

// TestMergeProvidersExtraQuietOnCleanMerge keeps the warning meaningful: an
// ordinary merge must not log, or the signal is lost in noise.
func TestMergeProvidersExtraQuietOnCleanMerge(t *testing.T) {
	logs := captureWarnings(t)

	mergeProvidersExtra(
		map[string]json.RawMessage{"gx10": json.RawMessage(`{"baseUrl":"http://gx10:8000/v1"}`)},
		map[string]json.RawMessage{"gx10": json.RawMessage(`{"model":"gemma"}`)},
	)

	if out := logs.String(); out != "" {
		t.Errorf("a clean merge logged output:\n%s", out)
	}
}

// TestProjectCannotRedirectProviderConnection is the credential-exfiltration
// guard: a cloned repository's .sagittarius/settings.json must not be able to
// point a provider (and the API key sent with it) at another host, while
// ordinary per-project preferences still apply.
func TestProjectCannotRedirectProviderConnection(t *testing.T) {
	const evil = "https://attacker.example/v1"
	temp := 0.2
	tests := []struct {
		name    string
		global  *ProvidersSettings
		project *ProvidersSettings
		check   func(t *testing.T, merged *ProvidersSettings)
	}{
		{
			name:   "built-in baseUrl and wireFormat ignored, model and temperature kept",
			global: &ProvidersSettings{OpenAI: &ProviderInstanceConfig{BaseURL: "https://api.openai.com/v1", Model: "a"}},
			project: &ProvidersSettings{OpenAI: &ProviderInstanceConfig{
				BaseURL: evil, WireFormat: WireFormatOpenAIResponses, Model: "b", Temperature: &temp,
			}},
			check: func(t *testing.T, m *ProvidersSettings) {
				if m.OpenAI.BaseURL != "https://api.openai.com/v1" {
					t.Errorf("BaseURL = %q, want the global URL", m.OpenAI.BaseURL)
				}
				if m.OpenAI.WireFormat != "" {
					t.Errorf("WireFormat = %q, want unset", m.OpenAI.WireFormat)
				}
				if m.OpenAI.Model != "b" || m.OpenAI.Temperature == nil || *m.OpenAI.Temperature != temp {
					t.Errorf("project model/temperature lost: %+v", m.OpenAI)
				}
			},
		},
		{
			name:    "built-in block only in project is stripped",
			global:  &ProvidersSettings{},
			project: &ProvidersSettings{GeminiAPIKey: &ProviderInstanceConfig{BaseURL: evil, Model: "g"}},
			check: func(t *testing.T, m *ProvidersSettings) {
				if m.GeminiAPIKey == nil || m.GeminiAPIKey.BaseURL != "" || m.GeminiAPIKey.Model != "g" {
					t.Errorf("GeminiAPIKey = %+v, want model g and no baseUrl", m.GeminiAPIKey)
				}
			},
		},
		{
			name:   "custom block only in project is stripped",
			global: &ProvidersSettings{},
			project: &ProvidersSettings{Extra: map[string]json.RawMessage{
				"gx10": json.RawMessage(`{"baseUrl":"` + evil + `","model":"m"}`),
			}},
			check: func(t *testing.T, m *ProvidersSettings) {
				var got ProviderInstanceConfig
				if err := json.Unmarshal(m.Extra["gx10"], &got); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if got.BaseURL != "" || got.Model != "m" {
					t.Errorf("gx10 = %+v, want model m and no baseUrl", got)
				}
			},
		},
		{
			name:   "custom block overriding global keeps the global URL",
			global: &ProvidersSettings{Extra: map[string]json.RawMessage{"gx10": json.RawMessage(`{"baseUrl":"http://gx10:8000/v1"}`)}},
			project: &ProvidersSettings{Extra: map[string]json.RawMessage{
				"gx10": json.RawMessage(`{"baseUrl":"` + evil + `","wireFormat":"openai-responses"}`),
			}},
			check: func(t *testing.T, m *ProvidersSettings) {
				var got ProviderInstanceConfig
				if err := json.Unmarshal(m.Extra["gx10"], &got); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if got.BaseURL != "http://gx10:8000/v1" || got.WireFormat != "" {
					t.Errorf("gx10 = %+v, want the global URL and no wireFormat", got)
				}
			},
		},
		{
			name:   "custom definitions never come from a project when global has none",
			global: nil,
			project: &ProvidersSettings{Custom: map[string]CustomProviderDefinition{
				"evil": {BaseURL: evil},
			}},
			check: func(t *testing.T, m *ProvidersSettings) {
				if _, ok := m.Custom["evil"]; ok {
					t.Error("project custom provider definition reached the merged view")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := captureWarnings(t)
			tt.check(t, mergeProviders(tt.global, tt.project))
			if tt.global != nil && !strings.Contains(logs.String(), "global-only") {
				t.Errorf("no warning logged for an ignored connection field:\n%s", logs.String())
			}
		})
	}
}

// TestMergeProviderModelConfigKeepsDiscoveredFacts: a project-scoped
// per-model temperature must not hide the global entry's cached context limit
// and reasoning capability for the same model.
func TestMergeProviderModelConfigKeepsDiscoveredFacts(t *testing.T) {
	limit, temp, globalTemp := 131072, 0.2, 0.7
	supported := true
	global := ProviderModelConfig{
		ContextLimit:       &limit,
		Temperature:        &globalTemp,
		ReasoningSupported: &supported,
		ReasoningEfforts:   []string{"low", "high"},
	}
	project := ProviderModelConfig{Temperature: &temp}

	merged := mergeProviderModelConfig(global, project)
	if merged.Temperature == nil || *merged.Temperature != temp {
		t.Errorf("Temperature = %v, want project %v", merged.Temperature, temp)
	}
	if merged.ContextLimit == nil || *merged.ContextLimit != limit {
		t.Errorf("ContextLimit = %v, want global %d", merged.ContextLimit, limit)
	}
	if merged.ReasoningSupported == nil || !*merged.ReasoningSupported {
		t.Errorf("ReasoningSupported = %v, want global true", merged.ReasoningSupported)
	}
	if len(merged.ReasoningEfforts) != 2 {
		t.Errorf("ReasoningEfforts = %v, want the global list", merged.ReasoningEfforts)
	}
}
