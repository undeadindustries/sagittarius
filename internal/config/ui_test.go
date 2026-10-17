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

func TestParseUIScreen(t *testing.T) {
	t.Parallel()
	tests := []struct {
		raw     string
		want    string
		wantErr bool
	}{
		{"", UIScreenFullscreen, false},
		{"auto", UIScreenAuto, false},
		{" FULLSCREEN ", UIScreenFullscreen, false},
		{"inline", UIScreenInline, false},
		{"split", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			t.Parallel()
			got, err := ParseUIScreen(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseUIScreen: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSetUIScreenAndMouse(t *testing.T) {
	s := &Settings{}
	if err := s.SetUIScreen("inline"); err != nil {
		t.Fatalf("SetUIScreen: %v", err)
	}
	if got := s.UI().Screen; got != UIScreenInline {
		t.Fatalf("screen = %q", got)
	}
	if err := s.SetUIMouse(false); err != nil {
		t.Fatalf("SetUIMouse: %v", err)
	}
	ui := s.UI()
	if ui.Mouse == nil || *ui.Mouse {
		t.Fatalf("mouse = %v, want false", ui.Mouse)
	}
	if err := s.SetUIScreen("auto"); err != nil {
		t.Fatalf("set auto: %v", err)
	}
	if got := s.UI().Screen; got != UIScreenAuto {
		t.Fatalf("auto screen = %q", got)
	}
	if err := s.SetUIScreen("fullscreen"); err != nil {
		t.Fatalf("clear screen: %v", err)
	}
	if err := s.SetUIMouse(true); err != nil {
		t.Fatalf("clear mouse: %v", err)
	}
	ui = s.UI()
	if ui.Screen != "" || ui.Mouse != nil {
		t.Fatalf("defaults not cleared: %+v", ui)
	}
	if err := s.SetUIScreen(""); err != nil {
		t.Fatalf("empty screen: %v", err)
	}
	if got := s.UI().Screen; got != "" {
		t.Fatalf("empty SetUIScreen stored %q", got)
	}
	if err := s.SetUISSHCopyTipShown(true); err != nil {
		t.Fatalf("set copy tip: %v", err)
	}
	if !s.UI().SSHCopyTipShown {
		t.Fatal("ssh copy tip not stored")
	}
	if err := s.SetUISSHCopyTipShown(false); err != nil {
		t.Fatalf("clear copy tip: %v", err)
	}
	if s.UI().SSHCopyTipShown {
		t.Fatal("ssh copy tip not cleared")
	}
	if err := s.SetUIScreen("nope"); err == nil {
		t.Fatal("invalid screen accepted")
	}
}
