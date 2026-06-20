package bubbletea

import (
	"context"
	"errors"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

type quitApp struct{}

func (quitApp) HandleInput(context.Context, string) (<-chan ui.StreamEvent, error) {
	ch := make(chan ui.StreamEvent)
	close(ch)
	return ch, nil
}

func TestUIRunCancelClean(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	term := NewTerminal(ui.Options{
		BannerTitle: "Test",
		Version:     "test",
		Headless:    true,
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- term.Run(ctx, quitApp{})
	}()

	time.Sleep(30 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("Run returned %v, want nil or context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not exit after context cancel")
	}
}

func TestRenderStreamWhenNotRunning(t *testing.T) {
	t.Parallel()
	term := NewTerminal(ui.Options{})
	if err := term.RenderStream(ui.StreamEvent{Type: ui.StreamTextDelta, Text: "x"}); !errors.Is(err, ui.ErrNotRunning) {
		t.Fatalf("RenderStream err = %v", err)
	}
}

func TestModelViewNonEmpty(t *testing.T) {
	t.Parallel()
	m := newModel(ui.Options{BannerTitle: "Sagittarius", Version: "1.0"}, quitApp{}, NewTerminal(ui.Options{}))
	m.width = 80
	m.height = 24
	if m.View() == "" {
		t.Fatal("expected non-empty view")
	}
}

func TestModelQuitCommand(t *testing.T) {
	t.Parallel()
	m := newModel(ui.Options{}, quitApp{}, NewTerminal(ui.Options{}))
	_, cmd := m.handleSubmit("/quit")
	if cmd == nil {
		t.Fatal("expected quit cmd")
	}
	// tea.Quit returns a Quit command; executing yields QuitMsg in program context.
	_ = cmd
}

func TestModelStreamPump(t *testing.T) {
	t.Parallel()
	m := newModel(ui.Options{}, quitApp{}, NewTerminal(ui.Options{}))
	events := make(chan ui.StreamEvent, 3)
	events <- ui.StreamEvent{Type: ui.StreamTextDelta, Text: "a"}
	events <- ui.StreamEvent{Type: ui.StreamTextDelta, Text: "b"}
	events <- ui.StreamEvent{Type: ui.StreamDone}
	close(events)
	m.stream = events

	cmd := waitStream(events)
	msg := cmd()
	evMsg, ok := msg.(streamEventMsg)
	if !ok {
		t.Fatalf("msg type %T", msg)
	}
	updated, next := m.handleStream(evMsg.event)
	if next == nil {
		t.Fatal("expected follow-up cmd for next stream chunk")
	}
	_ = updated
	_ = next()
}

func TestHandleKeyCtrlC(t *testing.T) {
	t.Parallel()
	m := newModel(ui.Options{}, quitApp{}, NewTerminal(ui.Options{}))
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected quit on ctrl+c")
	}
}
