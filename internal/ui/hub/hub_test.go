package hub

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

type fakeChildUI struct {
	mu           sync.Mutex
	events       []ui.StreamEvent
	statuses     []ui.StatusBar
	errors       []error
	runStarted   chan struct{}
	runBlock     chan struct{}
	runErr       error
	promptResult string
	promptErr    error
}

func newFakeChildUI() *fakeChildUI {
	return &fakeChildUI{
		runStarted: make(chan struct{}),
		runBlock:   make(chan struct{}),
	}
}

func (f *fakeChildUI) Run(ctx context.Context, app ui.App) error {
	close(f.runStarted)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-f.runBlock:
		return f.runErr
	}
}

func (f *fakeChildUI) RenderStream(delta ui.StreamEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, delta)
	return nil
}

func (f *fakeChildUI) SetStatus(status ui.StatusBar) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statuses = append(f.statuses, status)
	return nil
}

func (f *fakeChildUI) ShowError(err error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errors = append(f.errors, err)
	return nil
}

func (f *fakeChildUI) PromptInput() (string, error) {
	return f.promptResult, f.promptErr
}

func (f *fakeChildUI) getEvents() []ui.StreamEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ui.StreamEvent(nil), f.events...)
}

type fakeApp struct {
	mu        sync.Mutex
	inputs    []string
	handlerFn func(ctx context.Context, input string) (<-chan ui.StreamEvent, error)
}

func (a *fakeApp) HandleInput(ctx context.Context, input string) (<-chan ui.StreamEvent, error) {
	a.mu.Lock()
	a.inputs = append(a.inputs, input)
	fn := a.handlerFn
	a.mu.Unlock()

	if fn != nil {
		return fn(ctx, input)
	}

	ch := make(chan ui.StreamEvent, 2)
	ch <- ui.StreamEvent{Type: ui.StreamTextDelta, Text: "echo: " + input}
	ch <- ui.StreamEvent{Type: ui.StreamDone}
	close(ch)
	return ch, nil
}

func TestHubFanOut(t *testing.T) {
	child1 := newFakeChildUI()
	child2 := newFakeChildUI()

	h := New(
		Child{UI: child1, Attribution: "(via TUI) "},
		Child{UI: child2, Attribution: "(via Google Chat) "},
	)
	defer h.Close()

	ev := ui.StreamEvent{Type: ui.StreamInfo, Text: "system update"}
	if err := h.RenderStream(ev); err != nil {
		t.Fatalf("RenderStream: %v", err)
	}

	status := ui.StatusBar{Left: "Agent", Right: "Working"}
	if err := h.SetStatus(status); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}

	testErr := fmt.Errorf("connection error")
	if err := h.ShowError(testErr); err != nil {
		t.Fatalf("ShowError: %v", err)
	}

	if len(child1.getEvents()) != 1 || child1.getEvents()[0].Text != "system update" {
		t.Errorf("child1 missed RenderStream event")
	}
	if len(child2.getEvents()) != 1 || child2.getEvents()[0].Text != "system update" {
		t.Errorf("child2 missed RenderStream event")
	}

	if len(child1.statuses) != 1 || child1.statuses[0].Left != "Agent" {
		t.Errorf("child1 missed SetStatus")
	}
	if len(child2.statuses) != 1 || child2.statuses[0].Left != "Agent" {
		t.Errorf("child2 missed SetStatus")
	}

	if len(child1.errors) != 1 || child1.errors[0] != testErr {
		t.Errorf("child1 missed ShowError")
	}
	if len(child2.errors) != 1 || child2.errors[0] != testErr {
		t.Errorf("child2 missed ShowError")
	}
}

func TestHubAppForTeeExcludesOriginatorAndCrossEchoes(t *testing.T) {
	child1 := newFakeChildUI() // e.g. TUI
	child2 := newFakeChildUI() // e.g. Google Chat

	h := New(
		Child{UI: child1, Attribution: "(via Terminal) "},
		Child{UI: child2, Attribution: "(via Google Chat) "},
	)
	defer h.Close()

	app := &fakeApp{}
	h.realApp = app

	// Google Chat sends a message
	gcApp := h.AppFor(child2)
	ch, err := gcApp.HandleInput(context.Background(), "list files")
	if err != nil {
		t.Fatalf("HandleInput: %v", err)
	}

	var receivedByOriginator []ui.StreamEvent
	for ev := range ch {
		receivedByOriginator = append(receivedByOriginator, ev)
	}

	// Originator (child2) should have received events via returned channel
	if len(receivedByOriginator) != 2 {
		t.Fatalf("originator got %d events, want 2", len(receivedByOriginator))
	}
	if receivedByOriginator[0].Text != "echo: list files" {
		t.Errorf("originator got text %q, want 'echo: list files'", receivedByOriginator[0].Text)
	}

	// Originator should NOT have received duplicate events via RenderStream
	if len(child2.getEvents()) != 0 {
		t.Errorf("originator received %d events via RenderStream, want 0", len(child2.getEvents()))
	}

	// Non-originator (child1 / TUI) should have received:
	// 1. Cross-echo of user turn with attribution
	// 2. StreamTextDelta ("echo: list files")
	// 3. StreamDone
	c1Events := child1.getEvents()
	if len(c1Events) != 3 {
		t.Fatalf("child1 got %d events, want 3 (1 echo + 2 stream)", len(c1Events))
	}

	if c1Events[0].Type != ui.StreamScrollback || c1Events[0].ScrollbackRole != ui.ScrollbackUser {
		t.Errorf("c1Events[0] is not a ScrollbackUser: %+v", c1Events[0])
	}
	if c1Events[0].Text != "(via Google Chat) list files" {
		t.Errorf("c1Events[0] text = %q, want '(via Google Chat) list files'", c1Events[0].Text)
	}

	if c1Events[1].Type != ui.StreamTextDelta || c1Events[1].Text != "echo: list files" {
		t.Errorf("c1Events[1] = %+v", c1Events[1])
	}
	if c1Events[2].Type != ui.StreamDone {
		t.Errorf("c1Events[2] = %+v", c1Events[2])
	}
}

func TestHubQueueCapAndSerialization(t *testing.T) {
	child := newFakeChildUI()
	h := New(Child{UI: child, Attribution: ""})
	defer h.Close()

	turnRunning := make(chan struct{}, 10)
	allowTurnFinish := make(chan struct{}, 10)

	app := &fakeApp{
		handlerFn: func(ctx context.Context, input string) (<-chan ui.StreamEvent, error) {
			turnRunning <- struct{}{}

			ch := make(chan ui.StreamEvent, 1)
			go func() {
				<-allowTurnFinish
				ch <- ui.StreamEvent{Type: ui.StreamDone}
				close(ch)
			}()
			return ch, nil
		},
	}
	h.realApp = app

	childApp := h.AppFor(child)

	// Turn 0 starts
	ch0, err := childApp.HandleInput(context.Background(), "turn-0")
	if err != nil {
		t.Fatalf("turn 0 failed: %v", err)
	}
	<-turnRunning

	// Fill queue with 5 turns (DefaultQueueCap = 5)
	var mu sync.Mutex
	var queueErrs []error
	var queueChs []<-chan ui.StreamEvent
	var wg sync.WaitGroup

	for i := 1; i <= 5; i++ {
		idx := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch, err := childApp.HandleInput(context.Background(), fmt.Sprintf("turn-%d", idx))
			mu.Lock()
			if err != nil {
				queueErrs = append(queueErrs, err)
			} else {
				queueChs = append(queueChs, ch)
			}
			mu.Unlock()
		}()
	}

	// Give goroutines time to enqueue
	time.Sleep(50 * time.Millisecond)

	// 6th turn should be rejected with ErrQueueFull
	_, err6 := childApp.HandleInput(context.Background(), "turn-6-overflow")
	if err6 != ErrQueueFull {
		t.Errorf("turn 6 got error %v, want %v", err6, ErrQueueFull)
	}

	// Pre-fill allowTurnFinish with enough tokens for all turns (turn 0 + 5 queued turns)
	for i := 0; i < 6; i++ {
		allowTurnFinish <- struct{}{}
	}

	for range ch0 {
	}

	wg.Wait()

	mu.Lock()
	if len(queueErrs) > 0 {
		t.Errorf("queued turns failed: %v", queueErrs)
	}
	if len(queueChs) != 5 {
		t.Errorf("got %d queued turn channels, want 5", len(queueChs))
	}
	mu.Unlock()
}

func TestHubSingleChild(t *testing.T) {
	child := newFakeChildUI()
	h := New(Child{UI: child, Attribution: "(via Google Chat) "})
	defer h.Close()

	app := &fakeApp{}
	h.realApp = app

	childApp := h.AppFor(child)
	ch, err := childApp.HandleInput(context.Background(), "single child turn")
	if err != nil {
		t.Fatalf("HandleInput failed: %v", err)
	}

	var events []ui.StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].Text != "echo: single child turn" {
		t.Errorf("unexpected event text: %q", events[0].Text)
	}
}
