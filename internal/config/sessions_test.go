package config

import "testing"

func strPtr(s string) *string { return &s }

func TestSessionsAutoTitle(t *testing.T) {
	tests := []struct {
		name    string
		global  *Settings
		project *Settings
		want    AutoTitlePolicy
	}{
		{name: "nil settings default prompt", global: nil, project: nil, want: AutoTitlePrompt},
		{
			name:    "global auto",
			global:  &Settings{Sagittarius: &SagittariusSettings{Sessions: &SagittariusSessionsConfig{AutoTitle: strPtr("auto")}}},
			project: nil,
			want:    AutoTitleAuto,
		},
		{
			name:    "global auto, project off wins",
			global:  &Settings{Sagittarius: &SagittariusSettings{Sessions: &SagittariusSessionsConfig{AutoTitle: strPtr("auto")}}},
			project: &Settings{Sagittarius: &SagittariusSettings{Sessions: &SagittariusSessionsConfig{AutoTitle: strPtr("off")}}},
			want:    AutoTitleOff,
		},
		{
			name:    "unrecognized value falls back to prompt",
			global:  &Settings{Sagittarius: &SagittariusSettings{Sessions: &SagittariusSessionsConfig{AutoTitle: strPtr("bogus")}}},
			project: nil,
			want:    AutoTitlePrompt,
		},
		{
			name:    "empty string falls back to prompt",
			global:  &Settings{Sagittarius: &SagittariusSettings{Sessions: &SagittariusSessionsConfig{AutoTitle: strPtr("")}}},
			project: nil,
			want:    AutoTitlePrompt,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SessionsAutoTitle(tt.global, tt.project); got != tt.want {
				t.Errorf("SessionsAutoTitle() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestSessionsConfigRoundTrip verifies the sessions block marshals and
// unmarshals with its typed field and unknown keys intact.
func TestSessionsConfigRoundTrip(t *testing.T) {
	in := `{"autoTitle":"off","futureKey":{"nested":1}}`
	cfg, err := unmarshalSessionsConfig([]byte(in))
	if err != nil {
		t.Fatalf("unmarshalSessionsConfig: %v", err)
	}
	if cfg.AutoTitle == nil || *cfg.AutoTitle != "off" {
		t.Fatalf("AutoTitle = %v, want off", cfg.AutoTitle)
	}
	if len(cfg.Extra) != 1 {
		t.Fatalf("Extra = %v, want one unknown key", cfg.Extra)
	}

	out, err := marshalSessionsConfig(cfg)
	if err != nil {
		t.Fatalf("marshalSessionsConfig: %v", err)
	}
	cfg2, err := unmarshalSessionsConfig(out)
	if err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	if cfg2.AutoTitle == nil || *cfg2.AutoTitle != "off" {
		t.Errorf("round-tripped AutoTitle = %v, want off", cfg2.AutoTitle)
	}
	if len(cfg2.Extra) != 1 {
		t.Errorf("round-tripped Extra = %v, want one unknown key", cfg2.Extra)
	}
}

// TestSessionsMerge verifies project-over-global merge of the sessions block.
func TestSessionsMerge(t *testing.T) {
	global := &SagittariusSessionsConfig{AutoTitle: strPtr("auto")}
	project := &SagittariusSessionsConfig{AutoTitle: strPtr("off")}

	if got := mergeSessionsConfig(global, project); got == nil || *got.AutoTitle != "off" {
		t.Errorf("merge project-over-global = %v, want off", got)
	}
	if got := mergeSessionsConfig(global, nil); got == nil || *got.AutoTitle != "auto" {
		t.Errorf("merge nil project = %v, want auto", got)
	}
	if got := mergeSessionsConfig(nil, project); got == nil || *got.AutoTitle != "off" {
		t.Errorf("merge nil global = %v, want off", got)
	}
}
