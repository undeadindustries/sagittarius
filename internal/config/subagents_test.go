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

func TestSubagentTargetResolution(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		class        SubagentClass
		cfg          *SagittariusSettings
		liveProvider string
		liveModel    string
		want         SubagentTarget
	}{
		{
			name:         "nil cfg falls back to live pair",
			class:        SubagentCoding,
			cfg:          nil,
			liveProvider: "live-prov",
			liveModel:    "live-model",
			want:         SubagentTarget{Provider: "live-prov", Model: "live-model", Source: SubagentTargetLive},
		},
		{
			name:         "class pin wins with provider",
			class:        SubagentCoding,
			cfg:          &SagittariusSettings{Subagents: &SagittariusSubagents{Coding: &SagittariusSubagentClass{Provider: "local", Model: "qwen"}, Default: SagittariusSubagentConfig{Provider: "cloud", Model: "opus"}}},
			liveProvider: "live-prov",
			liveModel:    "live-model",
			want:         SubagentTarget{Provider: "local", Model: "qwen", Source: SubagentTargetClass},
		},
		{
			name:         "default pin used when class unset",
			class:        SubagentResearch,
			cfg:          &SagittariusSettings{Subagents: &SagittariusSubagents{Default: SagittariusSubagentConfig{Provider: "cloud", Model: "opus"}}},
			liveProvider: "live-prov",
			liveModel:    "live-model",
			want:         SubagentTarget{Provider: "cloud", Model: "opus", Source: SubagentTargetDefault},
		},
		{
			name:         "legacy model-only pin leaves provider empty for live resolution",
			class:        SubagentCoding,
			cfg:          &SagittariusSettings{Subagents: &SagittariusSubagents{Coding: &SagittariusSubagentClass{Model: "qwen"}}},
			liveProvider: "live-prov",
			liveModel:    "live-model",
			want:         SubagentTarget{Provider: "", Model: "qwen", Source: SubagentTargetClass},
		},
		{
			name:         "whitespace is trimmed",
			class:        SubagentCoding,
			cfg:          &SagittariusSettings{Subagents: &SagittariusSubagents{Coding: &SagittariusSubagentClass{Provider: " local ", Model: " qwen "}}},
			liveProvider: " live-prov ",
			liveModel:    " live-model ",
			want:         SubagentTarget{Provider: "local", Model: "qwen", Source: SubagentTargetClass},
		},
		{
			name:         "reviewer resolves like other classes",
			class:        SubagentReviewer,
			cfg:          &SagittariusSettings{Subagents: &SagittariusSubagents{Reviewer: &SagittariusSubagentClass{Provider: "local", Model: "gemma"}}},
			liveProvider: "live-prov",
			liveModel:    "live-model",
			want:         SubagentTarget{Provider: "local", Model: "gemma", Source: SubagentTargetClass},
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ResolveSubagentTarget(tc.class, tc.cfg, tc.liveProvider, tc.liveModel); got != tc.want {
				t.Errorf("ResolveSubagentTarget = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestSubagentProviderRoundTrip(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"default":{"provider":"cloud","model":"opus"},"research":{"provider":"local","model":"qwen"},"reviewer":{"enabled":true,"provider":"local","model":"gemma"}}`)
	s, err := unmarshalSubagents(raw)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if s.Default.Provider != "cloud" || s.Default.Model != "opus" {
		t.Errorf("default = %+v, want provider cloud model opus", s.Default)
	}
	if s.Research == nil || s.Research.Provider != "local" || s.Research.Model != "qwen" {
		t.Errorf("research = %+v, want provider local model qwen", s.Research)
	}
	if s.Reviewer == nil || s.Reviewer.Provider != "local" || s.Reviewer.Model != "gemma" {
		t.Errorf("reviewer = %+v, want provider local model gemma", s.Reviewer)
	}
	if _, ok := s.Named["reviewer"]; ok {
		t.Error("reviewer leaked into Named")
	}
	b, err := marshalSubagents(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	again, err := unmarshalSubagents(b)
	if err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	if again.Default.Provider != "cloud" || again.Research.Provider != "local" || again.Reviewer.Model != "gemma" {
		t.Errorf("provider pins did not survive round trip: %+v %+v %+v", again.Default, again.Research, again.Reviewer)
	}
}

func TestSetClearResetSubagentOverride(t *testing.T) {
	t.Parallel()

	s := &Settings{}
	if err := SetSubagentOverride(s, "research", "local", "qwen"); err != nil {
		t.Fatalf("set research: %v", err)
	}
	if err := SetSubagentOverride(s, "utility", "cloud", "opus"); err != nil {
		t.Fatalf("set utility: %v", err)
	}
	if err := SetSubagentOverride(s, "default", "cloud", "sonnet"); err != nil {
		t.Fatalf("set default: %v", err)
	}
	if s.Sagittarius.Subagents.Research.Model != "qwen" || s.Sagittarius.Subagents.Research.Provider != "local" {
		t.Errorf("research = %+v, want local/qwen", s.Sagittarius.Subagents.Research)
	}
	if s.Sagittarius.Goal.EvaluatorModel != "opus" {
		t.Errorf("utility = %+v, want opus", s.Sagittarius.Goal)
	}
	if got := ResolveSubagentTarget(SubagentResearch, s.Sagittarius, "live", "live-m"); got.Model != "qwen" || got.Provider != "local" {
		t.Errorf("target = %+v, want local/qwen", got)
	}

	ClearSubagentOverride(s, "research")
	if got := ResolveSubagentTarget(SubagentResearch, s.Sagittarius, "live", "live-m"); got.Source != SubagentTargetDefault || got.Model != "sonnet" {
		t.Errorf("after clear, target = %+v, want default sonnet", got)
	}

	if err := SetSubagentOverride(s, "bogus", "p", "m"); err == nil {
		t.Error("expected error for unknown slot")
	}
	ClearSubagentOverride(s, "bogus") // no-op, must not panic

	if !ResetSubagentOverrides(s) {
		t.Error("expected reset to report a change")
	}
	if ResetSubagentOverrides(s) {
		t.Error("second reset should report no change")
	}
	if got := ResolveSubagentTarget(SubagentResearch, s.Sagittarius, "live", "live-m"); got.Source != SubagentTargetLive {
		t.Errorf("after reset, target = %+v, want live", got)
	}
	// Enablement switches survive a reset.
	enabled := true
	s.Sagittarius.Subagents = &SagittariusSubagents{Research: &SagittariusSubagentClass{Enabled: &enabled}}
	if ResetSubagentOverrides(s) {
		t.Error("reset with no pins should report no change")
	}
	if s.Sagittarius.Subagents.Research.Enabled == nil || !*s.Sagittarius.Subagents.Research.Enabled {
		t.Error("reset must preserve the enabled switch")
	}
}

// TestReviewerDefaultFollowsCoding pins the AD-153 default: with no explicit
// reviewer setting the batch review follows the coding switch, and an explicit
// choice always wins.
func TestReviewerDefaultFollowsCoding(t *testing.T) {
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
		name            string
		global, project string
		want            bool
	}{
		{"both unset defaults to off", `{}`, `{}`, false},
		{"coding on defaults reviewer on", `{"sagittarius":{"subagents":{"coding":{"enabled":true}}}}`, `{}`, true},
		{"explicit false beats coding on", `{"sagittarius":{"subagents":{"coding":{"enabled":true},"reviewer":{"enabled":false}}}}`, `{}`, false},
		{"explicit on without coding", `{"sagittarius":{"subagents":{"reviewer":{"enabled":true}}}}`, `{}`, true},
		{
			"project overrides global",
			`{"sagittarius":{"subagents":{"coding":{"enabled":true}}}}`,
			`{"sagittarius":{"subagents":{"reviewer":{"enabled":false}}}}`,
			false,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ReviewSubagentsEnabled(settings(tc.global), settings(tc.project)); got != tc.want {
				t.Errorf("ReviewSubagentsEnabled = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSubagentMaxFixRoundsRoundTrip(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"reviewer":{"enabled":true,"maxFixRounds":2}}`)
	s, err := unmarshalSubagents(raw)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if s.Reviewer == nil || s.Reviewer.MaxFixRounds == nil || *s.Reviewer.MaxFixRounds != 2 {
		t.Fatalf("reviewer.maxFixRounds = %+v, want 2", s.Reviewer)
	}
	b, err := marshalSubagents(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	again, err := unmarshalSubagents(b)
	if err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	if again.Reviewer == nil || again.Reviewer.MaxFixRounds == nil || *again.Reviewer.MaxFixRounds != 2 {
		t.Errorf("maxFixRounds did not survive round trip: %+v", again.Reviewer)
	}

	mk := func(n int) *SagittariusSettings {
		return &SagittariusSettings{Subagents: &SagittariusSubagents{
			Reviewer: &SagittariusSubagentClass{MaxFixRounds: &n},
		}}
	}
	if got := ResolveSubagentMaxFixRounds(nil, DefaultSubagentMaxFixRounds); got != 1 {
		t.Errorf("nil = %d, want default 1", got)
	}
	if got := ResolveSubagentMaxFixRounds(mk(0), 1); got != 0 {
		t.Errorf("0 = %d, want 0 (review only)", got)
	}
	if got := ResolveSubagentMaxFixRounds(mk(3), 1); got != 3 {
		t.Errorf("3 = %d, want 3", got)
	}
	for _, n := range []int{-1, 4, 100} {
		if got := ResolveSubagentMaxFixRounds(mk(n), 1); got != 1 {
			t.Errorf("%d = %d, want default 1", n, got)
		}
		if err := ValidateSagittariusSettings(mk(n)); err == nil {
			t.Errorf("ValidateSagittariusSettings(%d) = nil, want a range error", n)
		}
	}
}

func TestSubagentMaxAttemptsRoundTrip(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"maxAttempts":3,"research":{"model":"qwen"}}`)
	s, err := unmarshalSubagents(raw)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if s.MaxAttempts == nil || *s.MaxAttempts != 3 {
		t.Errorf("maxAttempts = %v, want 3", s.MaxAttempts)
	}
	if _, ok := s.Named["maxAttempts"]; ok {
		t.Error("maxAttempts leaked into Named")
	}
	b, err := marshalSubagents(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	again, err := unmarshalSubagents(b)
	if err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	if again.MaxAttempts == nil || *again.MaxAttempts != 3 {
		t.Errorf("maxAttempts did not survive round trip: %v", again.MaxAttempts)
	}
}

func TestSubagentMaxConcurrentResolution(t *testing.T) {
	t.Parallel()

	mk := func(n int) *SagittariusSettings {
		return &SagittariusSettings{Subagents: &SagittariusSubagents{MaxConcurrent: &n}}
	}
	if got := ResolveSubagentMaxConcurrent(nil, 8); got != 8 {
		t.Errorf("nil = %d, want 8", got)
	}
	if got := ResolveSubagentMaxConcurrent(mk(1), 8); got != 1 {
		t.Errorf("1 = %d, want 1", got)
	}
	if got := ResolveSubagentMaxConcurrent(mk(16), 8); got != 16 {
		t.Errorf("16 = %d, want 16", got)
	}
	for _, n := range []int{0, -1, 17, 100} {
		if got := ResolveSubagentMaxConcurrent(mk(n), 8); got != 8 {
			t.Errorf("%d = %d, want default 8", n, got)
		}
	}

	raw := []byte(`{"maxConcurrent":4}`)
	s, err := unmarshalSubagents(raw)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if s.MaxConcurrent == nil || *s.MaxConcurrent != 4 {
		t.Fatalf("maxConcurrent = %v, want 4", s.MaxConcurrent)
	}
	if _, ok := s.Named["maxConcurrent"]; ok {
		t.Error("maxConcurrent leaked into Named")
	}
	b, err := marshalSubagents(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	again, err := unmarshalSubagents(b)
	if err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	if again.MaxConcurrent == nil || *again.MaxConcurrent != 4 {
		t.Errorf("maxConcurrent did not survive round trip: %v", again.MaxConcurrent)
	}

	for _, n := range []int{0, 17} {
		v := mk(n)
		if err := ValidateSagittariusSettings(v); err == nil {
			t.Errorf("maxConcurrent=%d should fail validation", n)
		}
	}
}
