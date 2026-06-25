package bubbletea

import (
	"testing"

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

	_, cmd := m.handleSubmit("hello")
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
