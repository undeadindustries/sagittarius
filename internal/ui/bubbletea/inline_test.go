package bubbletea

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/undeadindustries/sagittarius/internal/ui"
	"github.com/undeadindustries/sagittarius/internal/ui/onboardingdialog"
)

func newInlineTestModel() *model {
	m := newModel(ui.Options{ThemeName: "greyscale", Inline: true}, quitApp{}, NewTerminal(ui.Options{}))
	m.width = 80
	m.height = 24
	m.viewport.Width = 78
	// Tests drive commits directly. The welcome waits for a real resize.
	m.inlineBootPending = false
	return m
}

func TestInlinePrintRowsRejoinsCodeOnly(t *testing.T) {
	t.Parallel()
	lines := []scrollLine{
		{text: "✦ hello", indent: 2},
		{text: "  world", indent: 2, cont: true, join: " "},
		{text: "code-a", verbatim: true},
		{text: "code-b", verbatim: true, cont: true, join: ""},
		{text: "  next", indent: 2},
	}
	got := inlinePrintRows(lines)
	if got != "✦ hello\n  world\ncode-acode-b\n  next" {
		t.Fatalf("print = %q", got)
	}
}

func TestInlineCommitOrderAndHoldBack(t *testing.T) {
	t.Parallel()
	m := newInlineTestModel()
	m.addBlock(roleUser, "hello")
	m.addBlock(roleInfo, "note")
	running := &toolCard{phase: toolRunning, toolName: "read_file"}
	m.blocks = append(m.blocks, scrollBlock{role: roleToolCard, card: running})
	m.addBlock(roleInfo, "after")

	text, n := m.inlineChunk(0, true)
	if n != 2 {
		t.Fatalf("committed prefix = %d, want 2 (running card holds the rest)", n)
	}
	if !strings.Contains(text, "hello") || !strings.Contains(text, "note") || strings.Contains(text, "after") {
		t.Fatalf("chunk = %q", text)
	}
	if m.blockFinal(2) {
		t.Fatal("running card reported final")
	}
	running.phase = toolSuccess
	text, n = m.inlineChunk(0, true)
	if n != 4 {
		t.Fatalf("after settle, prefix = %d, want 4\n%s", n, text)
	}
}

func TestTaskGroupNotFinalUntilFollowed(t *testing.T) {
	t.Parallel()
	m := newInlineTestModel()
	group := &taskGroupBlock{tasks: []*toolCard{{phase: toolSuccess}}}
	m.blocks = []scrollBlock{{role: roleTaskGroup, taskGroup: group}}
	if m.blockFinal(0) {
		t.Fatal("trailing task group should stay live")
	}
	m.addBlock(roleUser, "next")
	if !m.blockFinal(0) {
		t.Fatal("settled task group with a later block should be final")
	}
	group.tasks[0].phase = toolRunning
	if m.blockFinal(0) {
		t.Fatal("running task holds the group")
	}
}

func TestInlineCommitOneAtATime(t *testing.T) {
	t.Parallel()
	m := newInlineTestModel()
	m.addBlock(roleUser, "one")
	m.addBlock(roleUser, "two")
	cmd := m.inlineCommitCmd()
	if cmd == nil || !m.printInFlight {
		t.Fatal("expected a print in flight")
	}
	if again := m.inlineCommitCmd(); again != nil {
		t.Fatal("second print started while one was in flight")
	}
	next, _ := m.handleInlinePrinted(inlinePrintedMsg{blocks: 2, gen: m.printGeneration})
	updated := next.(*model)
	if updated.printInFlight {
		t.Fatal("ack left a print in flight")
	}
	if updated.committed != 2 {
		t.Fatalf("committed = %d, want 2", updated.committed)
	}
}

func TestInlineQuitFlushesRemainder(t *testing.T) {
	t.Parallel()
	m := newInlineTestModel()
	m.blocks = []scrollBlock{{role: roleToolCard, card: &toolCard{phase: toolRunning, toolName: "shell"}}}
	m.addBlock(roleUser, "still here")
	cmd := m.beginQuit()
	if !m.quitting || !m.inlineQuit {
		t.Fatal("quit flags not set")
	}
	if cmd == nil {
		t.Fatal("expected a quit command")
	}
	if m.committed != len(m.blocks) {
		t.Fatalf("committed = %d, want %d", m.committed, len(m.blocks))
	}
}

func TestInlineViewKeepsTail(t *testing.T) {
	t.Parallel()
	m := newInlineTestModel()
	m.height = 12
	m.width = 40
	var long strings.Builder
	for i := 0; i < 30; i++ {
		long.WriteString("line of a streaming reply\n")
	}
	m.blocks = []scrollBlock{{role: roleResponse, text: long.String()}}
	m.openResponseIdx = 0
	view := m.inlineView()
	if strings.Count(view, "\n")+1 > m.height {
		t.Fatalf("inline view is taller than the terminal (%d lines)", strings.Count(view, "\n")+1)
	}
	if !strings.Contains(view, "streaming reply") {
		t.Fatal("live tail missing the open response")
	}
}

func TestInlineScrollAndMouseDoNothing(t *testing.T) {
	t.Parallel()
	m := newInlineTestModel()
	if !m.handleScrollKey("pgup") {
		t.Fatal("pgup should be consumed")
	}
	if cmd := m.setMouse(true); cmd != nil {
		t.Fatal("inline mode must not enable mouse capture")
	}
	_, cmd := m.Update(tea.KeyMsg{})
	_ = cmd
}

func TestInlineOverlayEdges(t *testing.T) {
	t.Parallel()
	m := newInlineTestModel()
	if m.inlineFollowUp(false) != nil {
		t.Fatal("empty model should not print")
	}
	empty := onboardingdialog.Model{}
	m.onboardingOverlay = &empty
	if cmd := m.inlineFollowUp(false); cmd == nil || !m.inlineAlt {
		t.Fatal("opening a dialog should enter the alternate screen")
	}
	if m.inlineFollowUp(true) != nil {
		t.Fatal("an open dialog should not enter the alternate screen again")
	}
	m.onboardingOverlay = nil
	if cmd := m.inlineFollowUp(true); cmd == nil || m.inlineAlt {
		t.Fatal("closing a dialog should exit the alternate screen")
	}
}
