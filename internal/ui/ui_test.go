package ui_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

type recordingUI struct {
	mu     sync.Mutex
	events []ui.StreamEvent
	status []ui.StatusBar
	errs   []error
}

func (r *recordingUI) Run(context.Context, ui.App) error { return ui.ErrNotRunning }

func (r *recordingUI) RenderStream(delta ui.StreamEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, delta)
	return nil
}

func (r *recordingUI) SetStatus(status ui.StatusBar) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status = append(r.status, status)
	return nil
}

func (r *recordingUI) PromptInput() (string, error) { return "", ui.ErrNotRunning }

func (r *recordingUI) ShowError(err error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errs = append(r.errs, err)
	r.events = append(r.events, ui.StreamEvent{Type: ui.StreamError, Err: err})
	return nil
}

func TestStreamEventRender(t *testing.T) {
	t.Parallel()
	rec := &recordingUI{}

	deltas := []ui.StreamEvent{
		{Type: ui.StreamTextDelta, Text: "hello "},
		{Type: ui.StreamTextDelta, Text: "world"},
		{Type: ui.StreamDone},
	}
	for _, d := range deltas {
		if err := rec.RenderStream(d); err != nil {
			t.Fatalf("RenderStream: %v", err)
		}
	}

	rec.mu.Lock()
	if len(rec.events) != 3 {
		t.Fatalf("events len = %d, want 3", len(rec.events))
	}
	if rec.events[0].Text != "hello " {
		t.Errorf("first delta = %q", rec.events[0].Text)
	}
	if rec.events[2].Type != ui.StreamDone {
		t.Errorf("last type = %v, want StreamDone", rec.events[2].Type)
	}
	rec.mu.Unlock()

	testErr := errors.New("boom")
	if err := rec.ShowError(testErr); err != nil {
		t.Fatalf("ShowError: %v", err)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.events) != 4 || rec.events[3].Err != testErr {
		t.Fatal("ShowError did not append StreamError event")
	}
}

func TestStatusBarUpdate(t *testing.T) {
	t.Parallel()
	rec := &recordingUI{}
	st := ui.StatusBar{Left: "gemini-apikey", Right: "ready"}
	if err := rec.SetStatus(st); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.status) != 1 || rec.status[0].Left != "gemini-apikey" {
		t.Fatalf("status = %+v", rec.status)
	}
}

func TestErrNotRunning(t *testing.T) {
	t.Parallel()
	rec := &recordingUI{}
	if _, err := rec.PromptInput(); !errors.Is(err, ui.ErrNotRunning) {
		t.Fatalf("PromptInput err = %v", err)
	}
}

func TestStreamEventTypesDistinct(t *testing.T) {
	t.Parallel()
	seen := map[ui.StreamEventType]struct{}{}
	for _, typ := range []ui.StreamEventType{
		ui.StreamTextDelta,
		ui.StreamToolStart,
		ui.StreamError,
		ui.StreamDone,
	} {
		seen[typ] = struct{}{}
	}
	if len(seen) != 4 {
		t.Fatal("stream event types should be distinct")
	}
}

// Ensure recordingUI satisfies ui.UI at compile time.
var _ ui.UI = (*recordingUI)(nil)

func TestRecordingUINonBlocking(t *testing.T) {
	t.Parallel()
	rec := &recordingUI{}
	done := make(chan struct{})
	go func() {
		_ = rec.RenderStream(ui.StreamEvent{Type: ui.StreamTextDelta, Text: "x"})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("RenderStream blocked")
	}
}
