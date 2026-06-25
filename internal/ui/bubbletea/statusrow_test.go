package bubbletea

import (
	"context"
	"strings"
	"testing"

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
	m.width = 80
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
	if !strings.Contains(row, "Alt+M") {
		t.Errorf("status row missing mouse toggle\n%s", row)
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
	if !strings.Contains(row, "Alt+M") {
		t.Errorf("status row missing Alt+M\n%s", row)
	}
	if m.statusRowRows() != 1 {
		t.Fatalf("statusRowRows = %d, want 1", m.statusRowRows())
	}
}

func TestScrollShortcutHintsForGOOS(t *testing.T) {
	t.Parallel()
	cases := []struct {
		goos string
		want string
	}{
		{"darwin", "Fn↑ Fn↓ · Alt+M"},
		{"windows", "Pg↑ Pg↓ · Alt+M"},
		{"linux", "Pg↑ Pg↓ · Alt+M"},
		{"freebsd", "Pg↑ Pg↓ · Alt+M"},
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
