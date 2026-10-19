package config

import (
	"testing"
)

func TestSkillsEnabled(t *testing.T) {
	enabled := true
	disabled := false
	tests := []struct {
		name    string
		global  *Settings
		project *Settings
		want    bool
	}{
		{name: "nil settings default on", want: true},
		{
			name:   "global disabled",
			global: &Settings{Sagittarius: &SagittariusSettings{SkillsEnabled: &disabled}},
			want:   false,
		},
		{
			name:    "project wins",
			global:  &Settings{Sagittarius: &SagittariusSettings{SkillsEnabled: &disabled}},
			project: &Settings{Sagittarius: &SagittariusSettings{SkillsEnabled: &enabled}},
			want:    true,
		},
		{
			name:    "project unset falls back to global",
			global:  &Settings{Sagittarius: &SagittariusSettings{SkillsEnabled: &disabled}},
			project: &Settings{Sagittarius: &SagittariusSettings{}},
			want:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SkillsEnabled(tt.global, tt.project); got != tt.want {
				t.Errorf("SkillsEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestSkillsEnabledRoundTrip pins the three document wiring points (unmarshal
// case, marshal add, reserved key) together: an explicit false must survive a
// save/load cycle, since the default is true and a lost false would silently
// re-enable skills.
func TestSkillsEnabledRoundTrip(t *testing.T) {
	off := false
	raw, err := marshalSagittarius(&SagittariusSettings{SkillsEnabled: &off})
	if err != nil {
		t.Fatal(err)
	}
	got, err := unmarshalSagittarius(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.SkillsEnabled == nil || *got.SkillsEnabled {
		t.Fatalf("round-trip lost skillsEnabled: %+v", got)
	}
	if _, leaked := got.Extra["skillsEnabled"]; leaked {
		t.Fatal("skillsEnabled leaked into Extra: missing reserved key")
	}
}
