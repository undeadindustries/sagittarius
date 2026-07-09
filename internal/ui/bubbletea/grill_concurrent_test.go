package bubbletea

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

// recordingApp implements ui.App and records every line passed to HandleInput,
// so tests can assert whether a slash command actually reached the app layer
// (vs. being queued or rejected).
type recordingApp struct {
	mu    sync.Mutex
	calls []string
}

func (a *recordingApp) HandleInput(_ context.Context, line string) (<-chan ui.StreamEvent, error) {
	a.mu.Lock()
	a.calls = append(a.calls, line)
	a.mu.Unlock()
	return doneStream(), nil
}

func (a *recordingApp) callCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.calls)
}

func TestIsConcurrentSafeSlash(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"/goal pause":    true,
		"/goal status":   true,
		"/grill pause":   true,
		"/grill status":  true,
		"/GRILL PAUSE":   true, // case-insensitive
		"/stats":         true,
		"/stats tools":   true,
		"/grill done":    false,
		"/grill start x": false,
		"/grill resume":  false,
		"/grill clear":   false,
		"/goal done":     false,
		"/model gpt-5":   false,
	}
	for line, want := range cases {
		if got := isConcurrentSafeSlash(line); got != want {
			t.Errorf("isConcurrentSafeSlash(%q) = %v, want %v", line, got, want)
		}
	}
}

// TestGrillPauseRunsConcurrentlyWhileBusy asserts /grill pause reaches the app
// layer immediately even while a turn is in flight (m.busy=true), rather than
// being queued or rejected like most slash commands.
func TestGrillPauseRunsConcurrentlyWhileBusy(t *testing.T) {
	t.Parallel()
	app := &recordingApp{}
	m := newShortcutModel(app)
	m.busy = true
	m.input.SetValue("/grill pause")

	_, cmd := m.handleBusyEnter()
	if cmd == nil {
		t.Fatal("expected a command running /grill pause concurrently")
	}
	if m.input.Value() != "" {
		t.Fatalf("input should be cleared, got %q", m.input.Value())
	}
	if len(m.queue) != 0 {
		t.Fatalf("queue = %v, want empty (concurrent-safe commands are not queued)", m.queue)
	}
	cmd()
	if got := app.callCount(); got != 1 {
		t.Fatalf("HandleInput calls = %d, want 1", got)
	}
}

// TestGrillStatusRunsConcurrentlyWhileBusy mirrors the pause case for status.
func TestGrillStatusRunsConcurrentlyWhileBusy(t *testing.T) {
	t.Parallel()
	app := &recordingApp{}
	m := newShortcutModel(app)
	m.busy = true
	m.input.SetValue("/grill status")

	_, cmd := m.handleBusyEnter()
	if cmd == nil {
		t.Fatal("expected a command running /grill status concurrently")
	}
	cmd()
	if got := app.callCount(); got != 1 {
		t.Fatalf("HandleInput calls = %d, want 1", got)
	}
}

// TestGrillDoneRejectedWhileBusy asserts a non-safe /grill subcommand (like
// done, which drives a spec-generation turn) cannot be queued or run mid-turn.
func TestGrillDoneRejectedWhileBusy(t *testing.T) {
	t.Parallel()
	app := &recordingApp{}
	m := newShortcutModel(app)
	m.busy = true
	before := len(m.blocks)
	m.input.SetValue("/grill done")

	_, cmd := m.handleBusyEnter()
	if cmd != nil {
		t.Fatal("expected no command for a non-concurrent-safe slash command")
	}
	if len(m.blocks) != before+1 {
		t.Fatalf("expected a rejection notice block, got %d new blocks", len(m.blocks)-before)
	}
	if !strings.Contains(stripANSI(m.renderScrollback(80)), "cannot be queued") {
		t.Fatalf("expected a 'cannot be queued' notice:\n%s", stripANSI(m.renderScrollback(80)))
	}
	if app.callCount() != 0 {
		t.Fatal("HandleInput should not be called for a rejected slash command")
	}
}
