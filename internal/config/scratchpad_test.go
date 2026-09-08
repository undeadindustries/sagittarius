package config

import (
	"testing"
)

func TestScratchpadEnabled(t *testing.T) {
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
			global: &Settings{Sagittarius: &SagittariusSettings{ScratchpadEnabled: &disabled}},
			want:   false,
		},
		{
			name:    "project wins",
			global:  &Settings{Sagittarius: &SagittariusSettings{ScratchpadEnabled: &disabled}},
			project: &Settings{Sagittarius: &SagittariusSettings{ScratchpadEnabled: &enabled}},
			want:    true,
		},
		{
			name:    "project unset falls back to global",
			global:  &Settings{Sagittarius: &SagittariusSettings{ScratchpadEnabled: &disabled}},
			project: &Settings{Sagittarius: &SagittariusSettings{}},
			want:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ScratchpadEnabled(tt.global, tt.project); got != tt.want {
				t.Errorf("ScratchpadEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestScratchpadEnabledRoundTrip pins the three document wiring points
// (unmarshal case, marshal add, reserved key) together: an explicit false must
// survive a save/load cycle, since the default is true and a lost false would
// silently re-enable the tool.
func TestScratchpadEnabledRoundTrip(t *testing.T) {
	off := false
	raw, err := marshalSagittarius(&SagittariusSettings{ScratchpadEnabled: &off})
	if err != nil {
		t.Fatal(err)
	}
	got, err := unmarshalSagittarius(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.ScratchpadEnabled == nil || *got.ScratchpadEnabled {
		t.Fatalf("round-trip lost scratchpadEnabled: %+v", got)
	}
	if _, leaked := got.Extra["scratchpadEnabled"]; leaked {
		t.Fatal("scratchpadEnabled leaked into Extra: missing reserved key")
	}
}
