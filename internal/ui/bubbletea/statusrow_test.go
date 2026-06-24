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
	for _, want := range []string{"auto-approve all (yolo)", "2 AGENTS.md files", "4 skills"} {
		if !strings.Contains(row, want) {
			t.Errorf("status row missing %q\n%s", want, row)
		}
	}
}

func TestRenderStatusRowEmptyWhenNoData(t *testing.T) {
	t.Parallel()
	// quitApp does not implement ComposerStatusProvider and there are no loaded
	// memory files, so the row collapses entirely.
	m := newTestModel()
	if got := m.renderStatusRow(); got != "" {
		t.Fatalf("status row should be empty without data, got %q", got)
	}
	if m.statusRowRows() != 0 {
		t.Fatalf("statusRowRows = %d, want 0", m.statusRowRows())
	}
}
