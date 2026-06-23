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

func TestSetUITheme(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		set       string
		wantTheme string
		wantUI    UISettings
	}{
		{"set greyscale", `{}`, "greyscale", "greyscale", UISettings{Theme: "greyscale"}},
		{"default clears", `{"ui":{"theme":"greyscale"}}`, "default", "", UISettings{}},
		{"empty clears", `{"ui":{"theme":"greyscale"}}`, "", "", UISettings{}},
		{
			"preserves hide flags",
			`{"ui":{"hideBanner":true,"hideTips":true}}`,
			"greyscale",
			"greyscale",
			UISettings{Theme: "greyscale", HideBanner: true, HideTips: true},
		},
		{
			"clears theme keeps hide flags",
			`{"ui":{"theme":"greyscale","hideBanner":true}}`,
			"default",
			"",
			UISettings{HideBanner: true},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var top map[string]json.RawMessage
			if err := json.Unmarshal([]byte(tc.raw), &top); err != nil {
				t.Fatalf("setup: %v", err)
			}
			s := &Settings{Raw: top}
			if err := s.SetUITheme(tc.set); err != nil {
				t.Fatalf("SetUITheme(%q): %v", tc.set, err)
			}
			if got := s.UI(); got != tc.wantUI {
				t.Errorf("UI() = %+v, want %+v", got, tc.wantUI)
			}
			if got := s.UI().Theme; got != tc.wantTheme {
				t.Errorf("UI().Theme = %q, want %q", got, tc.wantTheme)
			}
		})
	}
}

func TestSetUIThemeInitializesRaw(t *testing.T) {
	s := &Settings{}
	if err := s.SetUITheme("greyscale"); err != nil {
		t.Fatalf("SetUITheme: %v", err)
	}
	if got := s.UI().Theme; got != "greyscale" {
		t.Errorf("UI().Theme = %q, want greyscale", got)
	}
}

func TestSetUIThemeNil(t *testing.T) {
	var s *Settings
	if err := s.SetUITheme("greyscale"); err == nil {
		t.Error("SetUITheme on nil Settings = nil error, want error")
	}
}
