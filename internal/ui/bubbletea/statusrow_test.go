package bubbletea

import (
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

type statusApp struct {
	cs ui.ComposerStatus
}

func (statusApp) HandleInput(context.Context, string) (<-chan ui.StreamEvent, error) {
	ch := make(chan ui.StreamEvent)
	close(ch)
	return ch, nil
}

func (a statusApp) ComposerStatus() ui.ComposerStatus { return a.cs }

func TestApprovalHint(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"default":  "Tools: confirm before run",
		"":         "Tools: confirm before run",
		"autoEdit": "Tools: auto-accept edits",
		"yolo":     "Tools: auto-approve all (yolo)",
	}
	for in, want := range cases {
		if got := approvalHint(in); got != want {
			t.Errorf("approvalHint(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestContextSummaryOmitsZero(t *testing.T) {
	t.Parallel()
	if got := contextSummary(0, 0); got != "" {
		t.Fatalf("empty summary = %q, want empty", got)
	}
	if got := contextSummary(1, 0); got != "1 AGENTS.md file" {
		t.Fatalf("summary = %q", got)
	}
	if got := contextSummary(2, 3); got != "2 AGENTS.md files · 3 skills" {
		t.Fatalf("summary = %q", got)
	}
	if got := contextSummary(0, 1); got != "1 skill" {
		t.Fatalf("summary = %q", got)
	}
}

func TestRenderStatusRowShowsApprovalAndCounts(t *testing.T) {
	t.Parallel()
	opts := ui.Options{ThemeName: "greyscale", LoadedMemoryFiles: []string{"/a/AGENTS.md", "/b/AGENTS.md"}}
	app := statusApp{cs: ui.ComposerStatus{ApprovalMode: "yolo", SkillCount: 4}}
	m := newModel(opts, app, NewTerminal(ui.Options{}))
	// Wide terminal so the full composed row fits; clamping at narrow widths is
	// covered by TestStatusRowNeverExceedsWidth.
	m.width = 120
	m.height = 24

	if m.statusRowRows() != 1 {
		t.Fatalf("statusRowRows = %d, want 1", m.statusRowRows())
	}
	row := stripANSI(m.renderStatusRow())
	hints := scrollShortcutHints()
	for _, want := range []string{"auto-approve all (yolo)", hints, "2 AGENTS.md files", "4 skills"} {
		if !strings.Contains(row, want) {
			t.Errorf("status row missing %q\n%s", want, row)
		}
	}
}

func TestRenderStatusRowShowsScrollHintsWithoutComposerStatus(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.width = 80
	row := stripANSI(m.renderStatusRow())
	hints := scrollShortcutHints()
	if !strings.Contains(row, hints) {
		t.Errorf("status row missing scroll hints %q\n%s", hints, row)
	}
	if m.statusRowRows() != 1 {
		t.Fatalf("statusRowRows = %d, want 1", m.statusRowRows())
	}
}

// TestStatusRowNeverExceedsWidth reproduces the ghosting bug: with the default
// approval policy plus a skill count, the status row's left hints + right counts
// exceed an 80-col terminal, soft-wrap, and leave duplicate footer/status rows.
// renderStatusRowLine must clamp to width.
func TestStatusRowNeverExceedsWidth(t *testing.T) {
	t.Parallel()
	opts := ui.Options{ThemeName: "greyscale"}
	app := statusApp{cs: ui.ComposerStatus{ApprovalMode: "default", SkillCount: 1}}
	m := newModel(opts, app, NewTerminal(ui.Options{}))
	for _, w := range []int{40, 60, 80, 100} {
		m.width = w
		if got := lipgloss.Width(m.renderStatusRow()); got > w {
			t.Errorf("status row width = %d at terminal width %d (overflows → ghosting)", got, w)
		}
	}
}

// TestFitLeftRightKeepsRight verifies the truncation favors the right-aligned
// segment (live metrics) and never produces an over-width pair.
func TestFitLeftRightKeepsRight(t *testing.T) {
	t.Parallel()
	left := strings.Repeat("L", 70)
	right := "↑33.6k ↓1 100% ctx"
	gotLeft, gotRight := fitLeftRight(left, right, 80)
	if gotRight != right {
		t.Errorf("right segment was altered: %q", gotRight)
	}
	if total := lipgloss.Width(gotLeft) + lipgloss.Width(gotRight) + 1; total > 80 {
		t.Errorf("fitted parts still overflow: %d > 80", total)
	}
}

// TestViewNeverExceedsTerminalWidth is the end-to-end guard: a full frame with a
// realistic composer status must not emit any line wider than the terminal, so
// the renderer cannot ghost.
func TestViewNeverExceedsTerminalWidth(t *testing.T) {
	t.Parallel()
	opts := ui.Options{ThemeName: "greyscale", LoadedMemoryFiles: []string{"/a/AGENTS.md"}}
	app := statusApp{cs: ui.ComposerStatus{ApprovalMode: "default", SkillCount: 1}}
	m := newModel(opts, app, NewTerminal(ui.Options{}))
	m.width = 80
	m.height = 24
	m.status = ui.StatusBar{
		Left:   "~/src/test",
		Right:  "openrouter - z-ai/glm-4.5-air",
		Detail: "System Prompt: Programmer",
	}
	for i, line := range strings.Split(m.View(), "\n") {
		if w := lipgloss.Width(line); w > m.width {
			t.Errorf("View line %d width = %d exceeds %d: %q", i, w, m.width, stripANSI(line))
		}
	}
}

func TestScrollShortcutHintsForGOOS(t *testing.T) {
	t.Parallel()
	cases := []struct {
		goos string
		want string
	}{
		{"darwin", "Fn↑ Fn↓ · Alt+M or ⌥M mouse · Ctrl+T thinking"},
		{"windows", "Pg↑ Pg↓ · Alt+M or ⌥M mouse · Ctrl+T thinking"},
		{"linux", "Pg↑ Pg↓ · Alt+M or ⌥M mouse · Ctrl+T thinking"},
		{"freebsd", "Pg↑ Pg↓ · Alt+M or ⌥M mouse · Ctrl+T thinking"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.goos, func(t *testing.T) {
			t.Parallel()
			if got := scrollShortcutHintsForGOOS(tc.goos); got != tc.want {
				t.Fatalf("scrollShortcutHintsForGOOS(%q) = %q, want %q", tc.goos, got, tc.want)
			}
		})
	}
}
