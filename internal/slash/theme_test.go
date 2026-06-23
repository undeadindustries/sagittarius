package slash_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/slash"
)

func TestThemeRegistered(t *testing.T) {
	t.Parallel()
	reg := slash.NewRegistry()
	paths := [][]string{
		{"theme"},
		{"theme", "show"},
		{"theme", "default"},
		{"theme", "greyscale"},
	}
	for _, p := range paths {
		if reg.Lookup(p) == nil {
			t.Errorf("Lookup(%v) = nil, want command", p)
		}
	}
}

func TestThemeShowReportsCurrent(t *testing.T) {
	t.Parallel()
	settings := &config.Settings{Raw: map[string]json.RawMessage{
		"ui": json.RawMessage(`{"theme":"greyscale"}`),
	}}
	deps, _, _ := testDeps(t, settings)
	p := slash.NewProcessor()
	res := p.Process(context.Background(), "/theme show", deps)
	if !res.Handled {
		t.Fatal("expected handled result")
	}
	if len(res.Messages) == 0 || !strings.Contains(res.Messages[0], "greyscale") {
		t.Errorf("show messages = %v, want current theme greyscale", res.Messages)
	}
	if res.ThemeName != "" {
		t.Errorf("show ThemeName = %q, want empty (no live switch)", res.ThemeName)
	}
}

func TestThemeSwitch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"bare greyscale", "/theme greyscale", "greyscale"},
		{"bare default", "/theme default", "default"},
		{"set greyscale", "/theme set greyscale", "greyscale"},
		{"alias mono", "/theme mono", "greyscale"},
		{"alias normal", "/theme normal", "default"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			deps, _, hooks := testDeps(t, nil)
			p := slash.NewProcessor()
			res := p.Process(context.Background(), tc.input, deps)
			if !res.Handled {
				t.Fatalf("%q not handled", tc.input)
			}
			if res.Err != nil {
				t.Fatalf("%q error: %v", tc.input, res.Err)
			}
			if res.ThemeName != tc.want {
				t.Errorf("ThemeName = %q, want %q", res.ThemeName, tc.want)
			}
			if hooks.lastUITheme != tc.want {
				t.Errorf("persisted theme = %q, want %q", hooks.lastUITheme, tc.want)
			}
		})
	}
}

func TestThemeInvalid(t *testing.T) {
	t.Parallel()
	deps, _, hooks := testDeps(t, nil)
	p := slash.NewProcessor()
	res := p.Process(context.Background(), "/theme bogus", deps)
	if !res.Handled {
		t.Fatal("expected handled result")
	}
	if res.ThemeName != "" {
		t.Errorf("ThemeName = %q, want empty for invalid input", res.ThemeName)
	}
	if hooks.lastUITheme != "" {
		t.Errorf("persisted theme = %q, want empty (no Hooks call)", hooks.lastUITheme)
	}
	if len(res.Messages) == 0 || !strings.Contains(res.Messages[0], "Usage") {
		t.Errorf("messages = %v, want usage hint", res.Messages)
	}
}
