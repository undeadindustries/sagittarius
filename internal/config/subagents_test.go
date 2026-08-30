package config

import (
	"testing"
)

func TestSubagentsConfig(t *testing.T) {
	// 1. Valid enabled
	raw := []byte(`{"enabled": true}`)
	s, err := unmarshalSubagents(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Enabled == nil || *s.Enabled != true {
		t.Errorf("expected Enabled to be true, got %v", s.Enabled)
	}

	// Round trip
	b, err := marshalSubagents(s)
	if err != nil {
		t.Fatalf("unexpected error marshaling: %v", err)
	}
	if string(b) != `{"enabled":true}` {
		t.Errorf("unexpected marshaled result: %s", b)
	}

	// 2. Default and named
	raw2 := []byte(`{"default": {"model": "foo"}, "researcher": {"model": "bar"}}`)
	s2, err := unmarshalSubagents(raw2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s2.Default.Model != "foo" {
		t.Errorf("expected default model to be foo")
	}
	if s2.Named["researcher"].Model != "bar" {
		t.Errorf("expected named researcher model to be bar")
	}

	// 3. Invalid enabled (wrong type)
	raw3 := []byte(`{"enabled": {"not": "a bool"}}`)
	_, err = unmarshalSubagents(raw3)
	if err == nil {
		t.Errorf("expected error decoding invalid enabled")
	}
}

// TestSubagentClassesDoNotLeakIntoNamed guards the round-trip trap: every key
// under subagents that is not reserved becomes a Named entry, which would
// swallow a class's "enabled" into that entry's Extra.
func TestSubagentClassesDoNotLeakIntoNamed(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"research":{"enabled":true},"coding":{"enabled":true,"model":"qwen"},"investigator":{"model":"bar"}}`)
	s, err := unmarshalSubagents(raw)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := s.Named["research"]; ok {
		t.Error("research leaked into Named")
	}
	if _, ok := s.Named["coding"]; ok {
		t.Error("coding leaked into Named")
	}
	if s.Research == nil || s.Research.Enabled == nil || !*s.Research.Enabled {
		t.Errorf("research.enabled = %+v, want true", s.Research)
	}
	if s.Coding == nil || s.Coding.Model != "qwen" {
		t.Errorf("coding = %+v, want model qwen", s.Coding)
	}
	if s.Named["investigator"].Model != "bar" {
		t.Error("arbitrary named entries must still round-trip")
	}

	b, err := marshalSubagents(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	again, err := unmarshalSubagents(b)
	if err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	if again.Coding == nil || again.Coding.Enabled == nil || !*again.Coding.Enabled || again.Coding.Model != "qwen" {
		t.Errorf("coding did not survive round trip: %+v", again.Coding)
	}
	if again.Named["investigator"].Model != "bar" {
		t.Error("named entry did not survive round trip")
	}
}

func TestSubagentClassEnablement(t *testing.T) {
	t.Parallel()

	settings := func(body string) *Settings {
		t.Helper()
		s, err := decodeSettingsDocument([]byte(body))
		if err != nil {
			t.Fatalf("decode %s: %v", body, err)
		}
		return s
	}

	tests := []struct {
		name                   string
		global, project        string
		wantResearch, wantCode bool
	}{
		{
			name:    "unset defaults to off",
			global:  `{}`,
			project: `{}`,
		},
		{
			name:         "legacy enabled grants research only",
			global:       `{"sagittarius":{"subagents":{"enabled":true}}}`,
			project:      `{}`,
			wantResearch: true,
		},
		{
			name:         "explicit research beats legacy",
			global:       `{"sagittarius":{"subagents":{"enabled":true,"research":{"enabled":false}}}}`,
			project:      `{}`,
			wantResearch: false,
		},
		{
			name:     "coding is opt-in on its own key",
			global:   `{"sagittarius":{"subagents":{"coding":{"enabled":true}}}}`,
			project:  `{}`,
			wantCode: true,
		},
		{
			name:         "project overrides global",
			global:       `{"sagittarius":{"subagents":{"coding":{"enabled":true}}}}`,
			project:      `{"sagittarius":{"subagents":{"coding":{"enabled":false},"research":{"enabled":true}}}}`,
			wantResearch: true,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g, p := settings(tc.global), settings(tc.project)
			if got := ResearchSubagentsEnabled(g, p); got != tc.wantResearch {
				t.Errorf("ResearchSubagentsEnabled = %v, want %v", got, tc.wantResearch)
			}
			if got := CodingSubagentsEnabled(g, p); got != tc.wantCode {
				t.Errorf("CodingSubagentsEnabled = %v, want %v", got, tc.wantCode)
			}
		})
	}
}

func TestSubagentModelResolution(t *testing.T) {
	t.Parallel()

	const live = "live-model"
	cfg := &SagittariusSettings{
		Subagents: &SagittariusSubagents{
			Default: SagittariusSubagentConfig{Model: "subagent-default"},
			Coding:  &SagittariusSubagentClass{Model: "coding-model"},
		},
	}

	if got := SubagentModel(SubagentCoding, cfg, live); got != "coding-model" {
		t.Errorf("coding model = %q, want coding-model", got)
	}
	if got := SubagentModel(SubagentResearch, cfg, live); got != "subagent-default" {
		t.Errorf("research model = %q, want subagent-default", got)
	}
	if got := SubagentModel(SubagentCoding, &SagittariusSettings{}, live); got != live {
		t.Errorf("no override = %q, want %q", got, live)
	}
	if got := SubagentModel(SubagentCoding, nil, live); got != live {
		t.Errorf("nil cfg = %q, want %q", got, live)
	}
}
