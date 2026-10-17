package slash_test

import (
	"context"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/slash"
)

func TestScreenRegistered(t *testing.T) {
	t.Parallel()
	reg := slash.NewRegistry()
	for _, p := range [][]string{{"screen"}, {"screen", "fullscreen"}, {"screen", "inline"}, {"screen", "show"}, {"screen", "help"}} {
		if reg.Lookup(p) == nil {
			t.Errorf("Lookup(%v) = nil", p)
		}
	}
}

func TestScreenSetsMode(t *testing.T) {
	t.Parallel()
	p := slash.NewProcessor()
	cases := []struct {
		input string
		mode  string
	}{
		{"/screen", "show"},
		{"/screen show", "show"},
		{"/screen fullscreen", config.UIScreenFullscreen},
		{"/screen full", config.UIScreenFullscreen},
		{"/screen inline", config.UIScreenInline},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			res := p.Process(context.Background(), tc.input, slash.Deps{})
			if res.ScreenMode != tc.mode {
				t.Fatalf("ScreenMode = %q, want %q", res.ScreenMode, tc.mode)
			}
			if len(res.Messages) == 0 {
				t.Fatal("expected an explanation")
			}
		})
	}
	bad := p.Process(context.Background(), "/screen sideways", slash.Deps{})
	if bad.ScreenMode != "" || !strings.Contains(bad.Messages[0], "Usage") {
		t.Fatalf("bad args = %+v", bad)
	}
}

// TestScreenHelpExplainsModes pins what the help must say: each layout, the
// auto setting, how to switch, and that inline is the recommended one. It must
// not switch anything, and it works in screen reader mode.
func TestScreenHelpExplainsModes(t *testing.T) {
	t.Parallel()
	p := slash.NewProcessor()
	cases := []struct {
		name  string
		input string
		deps  slash.Deps
	}{
		{"subcommand", "/screen help", slash.Deps{}},
		{"upper case arg", "/screen HELP", slash.Deps{}},
		{"screen reader", "/screen help", slash.Deps{ScreenReader: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res := p.Process(context.Background(), tc.input, tc.deps)
			if !res.Handled || len(res.Messages) != 1 {
				t.Fatalf("result = %+v, want one handled message", res)
			}
			if res.ScreenMode != "" {
				t.Fatalf("help must not change the layout, ScreenMode = %q", res.ScreenMode)
			}
			text := res.Messages[0]
			for _, want := range []string{
				"Inline (recommended)", "Full screen", "Auto (ui.screen = auto)",
				"alternate screen", "scrollback", "OSC 52", "/screen inline", "--screen inline", "/settings",
			} {
				if !strings.Contains(text, want) {
					t.Errorf("help missing %q\n%s", want, text)
				}
			}
			if strings.Index(text, "Inline (recommended)") > strings.Index(text, "Full screen\n") {
				t.Error("inline should be listed before full screen")
			}
			for _, banned := range []string{"—", "“", "”", "’"} {
				if strings.Contains(text, banned) {
					t.Errorf("help contains %q, use plain punctuation", banned)
				}
			}
		})
	}
}

func TestScreenRefusesScreenReader(t *testing.T) {
	t.Parallel()
	p := slash.NewProcessor()
	res := p.Process(context.Background(), "/screen inline", slash.Deps{ScreenReader: true})
	if res.ScreenMode != "" {
		t.Fatalf("ScreenMode = %q, want empty", res.ScreenMode)
	}
	if len(res.Messages) == 0 || !strings.Contains(res.Messages[0], "Screen reader") {
		t.Fatalf("messages = %v", res.Messages)
	}
}
