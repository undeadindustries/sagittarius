package config

import (
	"encoding/json"
	"testing"
)

func TestSettingsUI(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want UISettings
	}{
		{"absent", `{}`, UISettings{}},
		{"theme", `{"ui":{"theme":"greyscale"}}`, UISettings{Theme: "greyscale"}},
		{"trims theme", `{"ui":{"theme":"  greyscale  "}}`, UISettings{Theme: "greyscale"}},
		{"hide flags", `{"ui":{"hideBanner":true,"hideTips":true}}`, UISettings{HideBanner: true, HideTips: true}},
		{"malformed treated as unset", `{"ui":"oops"}`, UISettings{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var top map[string]json.RawMessage
			if err := json.Unmarshal([]byte(tc.raw), &top); err != nil {
				t.Fatalf("setup: %v", err)
			}
			s := &Settings{Raw: top}
			if got := s.UI(); got != tc.want {
				t.Errorf("UI() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestSettingsUINil(t *testing.T) {
	var s *Settings
	if got := s.UI(); got != (UISettings{}) {
		t.Errorf("nil Settings UI() = %+v, want zero value", got)
	}
}
