package bubbletea

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

func TestBusyEnterQueuesMessage(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.busy = true
	m.input.SetValue("do this next")
	m.syncInputLayout()

	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

	if len(m.queue) != 1 || m.queue[0] != "do this next" {
		t.Fatalf("queue = %v, want one entry", m.queue)
	}
	if got := m.input.Value(); got != "" {
		t.Fatalf("input should be cleared after queueing, got %q", got)
	}
}

func TestInputPlaceholderReflectsBusy(t *testing.T) {
	t.Parallel()
	m := newTestModel()

	m.busy = false
	m.syncInputPlaceholder()
	if m.input.Placeholder != inputPlaceholderIdle {
		t.Fatalf("idle placeholder = %q, want %q", m.input.Placeholder, inputPlaceholderIdle)
	}

	m.busy = true
	m.syncInputPlaceholder()
	if m.input.Placeholder != inputPlaceholderBusy {
		t.Fatalf("busy placeholder = %q, want %q", m.input.Placeholder, inputPlaceholderBusy)
	}
}

func TestBusyEnterRejectsSlashCommands(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.busy = true
	m.input.SetValue("/help")
	m.syncInputLayout()

	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

	if len(m.queue) != 0 {
		t.Fatalf("slash command should not be queued, queue = %v", m.queue)
	}
}

func TestQueueFlushesOnStreamDone(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.busy = true
	m.queue = []string{"alpha", "beta"}

	_, cmd := m.handleStream(ui.StreamEvent{Type: ui.StreamDone})
	if cmd == nil {
		t.Fatal("StreamDone with a non-empty queue should return a submit command")
	}
	if len(m.queue) != 0 {
		t.Fatalf("queue should be cleared after flush, got %v", m.queue)
	}
	msg := cmd()
	sm, ok := msg.(submitMsg)
	if !ok {
		t.Fatalf("flush command should produce submitMsg, got %T", msg)
	}
	if sm.line != "alpha\n\nbeta" {
		t.Fatalf("flushed line = %q, want joined queue", sm.line)
	}
}

func TestPopQueuedIntoInputOnEmptyUp(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.queue = []string{"first", "second"}

	if !m.popQueuedIntoInput() {
		t.Fatal("popQueuedIntoInput should consume the queue when input is empty")
	}
	if got := m.input.Value(); got != "first\n\nsecond" {
		t.Fatalf("input after pop = %q, want joined queue", got)
	}
	if len(m.queue) != 0 {
		t.Fatalf("queue should be empty after pop, got %v", m.queue)
	}
}

func TestTypingStaysResponsiveWhileBusy(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.busy = true

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hi")})
	if got := m.input.Value(); got != "hi" {
		t.Fatalf("typing while busy should reach the input, got %q", got)
	}
}
