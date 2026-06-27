package bubbletea

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/undeadindustries/sagittarius/internal/ui"
	"github.com/undeadindustries/sagittarius/internal/ui/theme"
)

const sampleDiff = "--- a/x\n+++ b/x\n@@ -1,2 +1,2 @@\n-old\n+new\n ctx\n"

func newTestModel() *model {
	m := newModel(ui.Options{ThemeName: "greyscale"}, quitApp{}, NewTerminal(ui.Options{}))
	m.width = 80
	m.height = 24
	return m
}

func TestRenderDiffLinesPreservesMarkers(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	out := stripANSI(strings.Join(m.renderDiffLines(sampleDiff, 80, 0), "\n"))
	for _, want := range []string{"@@ -1,2 +1,2 @@", "-old", "+new", " ctx"} {
		if !strings.Contains(out, want) {
			t.Errorf("diff render missing %q\n%s", want, out)
		}
	}
}

func TestRenderDiffLinesCaps(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	lines := m.renderDiffLines(sampleDiff, 80, 3)
	if len(lines) != 4 {
		t.Fatalf("capped diff = %d lines, want 4 (3 + footer)", len(lines))
	}
	if !strings.Contains(stripANSI(lines[3]), "more diff lines") {
		t.Fatalf("missing truncation footer: %q", stripANSI(lines[3]))
	}
}

func TestToolStartSummaryRendered(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.handleStream(ui.StreamEvent{Type: ui.StreamToolStart, ToolName: "write_file", Text: "hello.txt", ToolCallID: "c1"})
	out := stripANSI(m.renderScrollback(80))
	if !strings.Contains(out, "Write file") || !strings.Contains(out, "hello.txt") {
		t.Fatalf("tool-start card missing display name/summary: %s", out)
	}
}

func TestResultDiffColorizedInScrollback(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.handleStream(ui.StreamEvent{Type: ui.StreamToolStart, ToolName: "write_file", Text: "hello.txt", ToolCallID: "c1"})
	m.handleStream(ui.StreamEvent{Type: ui.StreamToolResult, ToolName: "write_file", Text: sampleDiff, ToolCallID: "c1"})
	out := stripANSI(m.renderScrollback(80))
	if !strings.Contains(out, "+new") || !strings.Contains(out, "-old") {
		t.Fatalf("result diff not rendered: %s", out)
	}
}

func TestTurnSpacingBetweenTurns(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.addBlock(roleUser, "first")
	m.addResponseDelta("reply")
	m.closeResponse()
	m.addBlock(roleUser, "second")
	out := stripANSI(m.renderScrollback(80))
	if !strings.Contains(out, "\n\nYou › second") {
		t.Fatalf("expected a blank line before the second turn:\n%s", out)
	}
}

func TestLoadedMemoryWelcomeLine(t *testing.T) {
	t.Parallel()
	th := theme.Greyscale()
	opts := ui.Options{LoadedMemoryFiles: []string{"/opt/global/AGENTS.md", "/srv/proj/AGENTS.md"}}
	line := stripANSI(renderLoadedMemory(opts, th))
	if !strings.Contains(line, "Loaded 2 AGENTS.md files:") {
		t.Fatalf("loaded-memory line wrong: %q", line)
	}
	if renderLoadedMemory(ui.Options{}, th) != "" {
		t.Fatal("no files should render no line")
	}
}

func TestConfirmSessionKeySendsDecision(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	reply := make(chan ui.ConfirmDecision, 1)
	m.handleStream(ui.StreamEvent{Type: ui.StreamToolStart, ToolName: "write_file", Text: "x", ToolCallID: "c1"})
	m.handleStream(ui.StreamEvent{Type: ui.StreamToolConfirm, ToolName: "write_file", Text: "x", ToolCallID: "c1", ConfirmReply: reply})
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	if got := <-reply; got != ui.ConfirmSession {
		t.Fatalf("key 2 sent %v, want ConfirmSession", got)
	}
	if m.confirmReply != nil {
		t.Fatal("confirm should clear after a decision")
	}
}

func TestEscCancelsTurn(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	canceled := false
	m.busy = true
	m.turnCancel = func() { canceled = true }
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if !canceled {
		t.Fatal("esc should cancel the in-flight turn")
	}
	if m.turnCancel != nil {
		t.Fatal("turnCancel should be cleared after cancel")
	}
}
