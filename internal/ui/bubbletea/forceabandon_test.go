package bubbletea

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

func TestForceAbandonClearsQueue(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.turnInFlight = true
	m.turnCanceled = true
	m.queue = []string{"stale prompt"}
	ch := make(chan ui.StreamEvent, 1)
	m.stream = ch

	cmd := m.forceAbandonTurn()
	if len(m.queue) != 0 {
		t.Fatalf("queue = %v, want empty after force abandon", m.queue)
	}
	if cmd == nil {
		t.Fatal("expected drain command when stream is attached")
	}
	close(ch)
}

func TestForceAbandonInvalidatesStaleStreamEvents(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.activeStreamGen = 1
	m.turnInFlight = true

	_, _ = m.handleStreamGen(1, ui.StreamEvent{Type: ui.StreamTextDelta, Text: "before"})
	m.forceAbandonTurn()
	if m.activeStreamGen != 2 {
		t.Fatalf("activeStreamGen = %d, want 2 after abandon", m.activeStreamGen)
	}

	before := len(m.blocks)
	_, _ = m.handleStreamGen(1, ui.StreamEvent{Type: ui.StreamTextDelta, Text: "stale"})
	if len(m.blocks) != before {
		t.Fatal("stale stream event with old generation should be ignored")
	}
}

func TestSubmitBlockedWhileTurnInFlight(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.turnInFlight = true

	_, cmd := m.handleSubmit(submitMsg{line: "hello", display: "hello"})
	if cmd != nil {
		t.Fatal("submit should be blocked while turnInFlight")
	}
}

func TestTurnDrainDoneClearsTurnInFlight(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.turnInFlight = true

	updated, _ := m.Update(turnDrainDoneMsg{})
	m = updated.(*model)
	if m.turnInFlight {
		t.Fatal("turnInFlight should clear after drain completes")
	}
}

// TestDrainStreamCmdReleasesOnStall is the regression test for the permanent
// composer wedge. A turn goroutine stuck inside a tool call never closes its
// stream channel, so an unbounded drain never reported back and turnInFlight
// stayed true for the rest of the session.
func TestDrainStreamCmdReleasesOnStall(t *testing.T) {
	t.Parallel()
	ch := make(chan ui.StreamEvent) // never closed, like a wedged turn

	msg, ok := drainStreamCmdFor(ch, 50*time.Millisecond)().(turnDrainDoneMsg)
	if !ok {
		t.Fatal("want a turnDrainDoneMsg once the drain gives up")
	}
	if !msg.stalled {
		t.Fatal("stalled should be set when the stream never closes")
	}
}

// TestDrainStreamCmdReportsCleanClose guards the ordinary path: a stream that
// closes must not be reported as stalled, so no spurious notice is shown.
func TestDrainStreamCmdReportsCleanClose(t *testing.T) {
	t.Parallel()
	ch := make(chan ui.StreamEvent, 1)
	ch <- ui.StreamEvent{Type: ui.StreamTextDelta, Text: "late delta"}
	close(ch)

	msg, ok := drainStreamCmdFor(ch, 5*time.Second)().(turnDrainDoneMsg)
	if !ok {
		t.Fatal("want a turnDrainDoneMsg after the stream closes")
	}
	if msg.stalled {
		t.Fatal("stalled should be clear when the stream closes normally")
	}
}

// TestStalledDrainClearsTurnInFlight asserts the composer is released even when
// the drain gave up, and that the user is told why the next message may bounce.
func TestStalledDrainClearsTurnInFlight(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.turnInFlight = true

	updated, _ := m.Update(turnDrainDoneMsg{stalled: true})
	m = updated.(*model)
	if m.turnInFlight {
		t.Fatal("turnInFlight must clear even when the drain stalled")
	}
	if !hasInfoBlock(m, "already in progress") {
		t.Fatalf("want a notice that the next message may bounce, got blocks:\n%s", blockTexts(m))
	}
}

// TestForceAbandonAnnouncesStop covers the silent second Esc: the reported
// session showed one "Turn canceled." and then unexplained rejections.
func TestForceAbandonAnnouncesStop(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.turnInFlight = true
	m.turnCanceled = true

	m.forceAbandonTurn()
	if !hasInfoBlock(m, "Stopped waiting for the turn") {
		t.Fatalf("want a force-stop notice in the scrollback, got blocks:\n%s", blockTexts(m))
	}
}

// hasInfoBlock reports whether any info block contains want. Scrollback blocks
// are checked instead of the rendered view because rendering wraps to the
// terminal width and would split the phrase under test.
func hasInfoBlock(m *model, want string) bool {
	for _, blk := range m.blocks {
		if blk.role == roleInfo && strings.Contains(blk.text, want) {
			return true
		}
	}
	return false
}

func blockTexts(m *model) string {
	var b strings.Builder
	for _, blk := range m.blocks {
		fmt.Fprintf(&b, "[%d] %s\n", blk.role, blk.text)
	}
	return b.String()
}
