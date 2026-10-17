package bubbletea

import (
	"io"
	"testing"
	"time"

	"github.com/creack/pty"

	"github.com/undeadindustries/sagittarius/internal/config"
)

func TestParseXTVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
		want string
		ok   bool
	}{
		{"kitty", "\x1bP>|kitty(0.36.0)\x1b\\", "kitty(0.36.0)", true},
		{"wezterm", "\x1bP>|WezTerm 20240203-110809-5046fc22\x1b\\", "WezTerm 20240203-110809-5046fc22", true},
		{"ghostty", "\x1bP>|ghostty 1.0.0\x1b\\", "ghostty 1.0.0", true},
		{"foot bel", "\x1bP>|foot(1.17.2)\a", "foot(1.17.2)", true},
		{"iterm2", "\x1bP>|iTerm2 3.5.0\x1b\\", "iTerm2 3.5.0", true},
		{"no reply", "\x1b[?62;c", "", false},
		{"garbage", "not a terminal", "", false},
		{"marker outside dcs", "version >|kitty\x1b\\", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := parseXTVersion(tc.raw)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("parseXTVersion = %q, %v; want %q, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestOSC52ByDefault(t *testing.T) {
	t.Parallel()
	if !osc52ByDefault("kitty(0.36.0)") || !osc52ByDefault("WezTerm 20240203") || !osc52ByDefault("ghostty 1.0.0") || !osc52ByDefault("foot(1.17.2)") {
		t.Fatal("known terminals should allow OSC 52")
	}
	if osc52ByDefault("iTerm2 3.5.0") || osc52ByDefault("Apple_Terminal") || osc52ByDefault("") {
		t.Fatal("iTerm2 and unknown terminals are not on the allowlist")
	}
}

func TestResolveScreenMode(t *testing.T) {
	t.Parallel()
	kitty := func() (string, bool) { return "kitty(0.40.0)", true }
	none := func() (string, bool) { return "", false }
	iterm := func() (string, bool) { return "iTerm2 3.5.0", true }
	tests := []struct {
		name    string
		setting string
		remote  bool
		mux     bool
		probe   func() (string, bool)
		inline  bool
	}{
		{"fullscreen forced", config.UIScreenFullscreen, true, false, none, false},
		{"empty is fullscreen", "", true, false, none, false},
		{"inline forced", config.UIScreenInline, false, false, kitty, true},
		{"auto local", config.UIScreenAuto, false, false, none, false},
		{"auto ssh mux", config.UIScreenAuto, true, true, kitty, true},
		{"auto ssh kitty", config.UIScreenAuto, true, false, kitty, false},
		{"auto ssh no reply", config.UIScreenAuto, true, false, none, true},
		{"auto ssh iterm", config.UIScreenAuto, true, false, iterm, true},
		{"auto ssh nil probe", config.UIScreenAuto, true, false, nil, true},
		{"unknown is fullscreen", "split", true, false, none, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			inline := resolveScreenMode(tc.setting, tc.remote, tc.mux, tc.probe)
			if inline != tc.inline {
				t.Fatalf("inline = %v, want %v", inline, tc.inline)
			}
		})
	}
}

func TestProbeTTY(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatalf("pty: %v", err)
	}
	t.Cleanup(func() {
		_ = master.Close()
		_ = slave.Close()
	})

	go func() {
		buf := make([]byte, 64)
		_, _ = io.ReadAtLeast(master, buf, 1)
		_, _ = io.WriteString(master, "\x1bP>|ghostty 1.1.0\x1b\\\x1b[?62;c")
	}()
	name, ok := probeTTY(slave, time.Second)
	if !ok || name != "ghostty 1.1.0" {
		t.Fatalf("probeTTY = %q, %v", name, ok)
	}
}

func TestProbeTTYTimeout(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatalf("pty: %v", err)
	}
	t.Cleanup(func() {
		_ = master.Close()
		_ = slave.Close()
	})
	// Drain the query so the write cannot block, and never answer.
	go func() {
		buf := make([]byte, 64)
		_, _ = master.Read(buf)
	}()
	if _, ok := probeTTY(slave, 30*time.Millisecond); ok {
		t.Fatal("expected timeout with no reply")
	}
}

func TestDA1Seen(t *testing.T) {
	t.Parallel()
	if !da1Seen([]byte("\x1bP>|kitty\x1b\\\x1b[?62;4c")) {
		t.Fatal("DA1 after XTVERSION not seen")
	}
	if da1Seen([]byte("\x1b[>0q")) {
		t.Fatal("query is not a DA1 reply")
	}
}
