package config

import (
	"testing"
)

func TestScriptToolEnabled(t *testing.T) {
	enabled := true
	disabled := false
	tests := []struct {
		name    string
		global  *Settings
		project *Settings
		want    bool
	}{
		{name: "nil settings", want: false},
		{
			name:   "global enabled",
			global: &Settings{Sagittarius: &SagittariusSettings{ScriptToolEnabled: &enabled}},
			want:   true,
		},
		{
			name:    "project wins",
			global:  &Settings{Sagittarius: &SagittariusSettings{ScriptToolEnabled: &enabled}},
			project: &Settings{Sagittarius: &SagittariusSettings{ScriptToolEnabled: &disabled}},
			want:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ScriptToolEnabled(tt.global, tt.project); got != tt.want {
				t.Errorf("ScriptToolEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestScriptToolEnabledRoundTrip(t *testing.T) {
	on := true
	raw, err := marshalSagittarius(&SagittariusSettings{ScriptToolEnabled: &on})
	if err != nil {
		t.Fatal(err)
	}
	got, err := unmarshalSagittarius(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.ScriptToolEnabled == nil || !*got.ScriptToolEnabled {
		t.Fatalf("round-trip lost scriptToolEnabled: %+v", got)
	}
}
