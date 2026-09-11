package bubbletea

import (
	"context"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

type sidebarRecordingApp struct {
	recordingApp
	mu        sync.Mutex
	questions []string
}

func (a *sidebarRecordingApp) AskSidebar(_ context.Context, question string) (<-chan ui.StreamEvent, error) {
	a.mu.Lock()
	a.questions = append(a.questions, question)
	a.mu.Unlock()
	ch := make(chan ui.StreamEvent, 2)
	ch <- ui.StreamEvent{Type: ui.StreamTextDelta, Text: "still running"}
	ch <- ui.StreamEvent{Type: ui.StreamDone}
	close(ch)
	return ch, nil
}

func (a *sidebarRecordingApp) asked() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.questions...)
}

func runningWaitCard() *toolCard {
	return &toolCard{callID: "wait-1", toolName: wireWaitUntil, phase: toolRunning}
}

func TestBusyEnterDuringWaitAsksSidebar(t *testing.T) {
	t.Parallel()
	app := &sidebarRecordingApp{}
	m := newShortcutModel(app)
	m.busy = true
	m.cardByID = map[string]*toolCard{"wait-1": runningWaitCard()}
	m.activeCard = m.cardByID["wait-1"]
	m.input.SetValue("is it done yet?")

	_, cmd := m.handleBusyEnter()
	if cmd == nil {
		t.Fatal("expected AskSidebar command")
	}
	if len(m.queue) != 0 {
		t.Fatalf("queue = %v, want empty during a wait", m.queue)
	}
	if got := m.input.Value(); got != "" {
		t.Fatalf("input = %q, want empty", got)
	}
	msg := cmd()
	sidebarMsg, ok := msg.(sidebarStreamEventMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want sidebarStreamEventMsg", msg)
	}
	m.handleSidebarStream(sidebarMsg)
	if got := app.asked(); len(got) != 1 || got[0] != "is it done yet?" {
		t.Fatalf("AskSidebar questions = %v", got)
	}
}

func TestBusyEnterWithoutWaitStillQueues(t *testing.T) {
	t.Parallel()
	app := &sidebarRecordingApp{}
	m := newShortcutModel(app)
	m.busy = true
	m.input.SetValue("do this next")

	_, cmd := m.handleBusyEnter()
	if cmd != nil {
		t.Fatal("expected no command when there is no wait card")
	}
	if len(m.queue) != 1 || m.queue[0] != "do this next" {
		t.Fatalf("queue = %v, want queued message", m.queue)
	}
	if len(app.asked()) != 0 {
		t.Fatalf("AskSidebar should not run, got %v", app.asked())
	}
}

func TestEscDuringSidebarCancelsQuestionOnly(t *testing.T) {
	t.Parallel()
	canceled := false
	m := newShortcutModel(&sidebarRecordingApp{})
	m.busy = true
	m.turnCancel = func() { t.Fatal("turn should not cancel") }
	m.sidebarCancel = func() { canceled = true }

	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Fatalf("esc during sidebar should not return a command, got %T", cmd)
	}
	if !canceled {
		t.Fatal("expected sidebar cancel")
	}
	if m.sidebarCancel != nil {
		t.Fatal("sidebarCancel should be cleared")
	}
}

func TestWaitPlaceholder(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.busy = true
	m.cardByID = map[string]*toolCard{"wait-1": runningWaitCard()}
	m.syncInputPlaceholder()
	if m.input.Placeholder != inputPlaceholderWait {
		t.Fatalf("placeholder = %q, want %q", m.input.Placeholder, inputPlaceholderWait)
	}
}
