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
	ch := make(chan ui.StreamEvent, 2)
	ch <- ui.StreamEvent{Type: ui.StreamQuit}
	ch <- ui.StreamEvent{Type: ui.StreamDone}
	close(ch)
	return ch, nil
}

// completerApp is a quitApp that also serves a fixed completion list, used to
// exercise the inline suggestion UI.
type completerApp struct {
	quitApp
	res ui.Completions
}

func (c completerApp) Complete(input string) ui.Completions {
	if input == "" || input[0] != '/' {
		return ui.Completions{ReplaceFrom: len(input)}
	}
	return c.res
}

func typeRunes(m *model, s string) {
	for _, r := range s {
		m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
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
		t.Fatal("expected stream cmd for /quit")
	}
	msg := cmd()
	evMsg, ok := msg.(streamEventMsg)
	if !ok {
		t.Fatalf("msg type %T", msg)
	}
	updated, quitCmd := m.handleStream(evMsg.event)
	if quitCmd == nil {
		t.Fatal("expected quit cmd from StreamQuit")
	}
	_ = updated
	_ = quitCmd
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

func suggestApp() completerApp {
	return completerApp{res: ui.Completions{
		Items: []ui.Suggestion{
			{Label: "provider", Description: "Manage providers", Insert: "provider", AppendSpace: true},
			{Label: "quit", Description: "Exit", Insert: "quit"},
		},
		ReplaceFrom: 1,
	}}
}

func TestSuggestionsAppearOnSlash(t *testing.T) {
	t.Parallel()
	m := newModel(ui.Options{}, suggestApp(), NewTerminal(ui.Options{}))
	typeRunes(m, "/")
	if len(m.suggestions) != 2 {
		t.Fatalf("suggestions = %d, want 2", len(m.suggestions))
	}
	if m.suggestionIdx != -1 {
		t.Fatalf("suggestionIdx = %d, want -1 (no highlight until arrow)", m.suggestionIdx)
	}
}

func TestSuggestionArrowNavigationWraps(t *testing.T) {
	t.Parallel()
	m := newModel(ui.Options{}, suggestApp(), NewTerminal(ui.Options{}))
	typeRunes(m, "/")

	m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.suggestionIdx != 0 {
		t.Fatalf("after down idx = %d, want 0", m.suggestionIdx)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.suggestionIdx != 1 {
		t.Fatalf("after second down idx = %d, want 1", m.suggestionIdx)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.suggestionIdx != 0 {
		t.Fatalf("after wrap down idx = %d, want 0", m.suggestionIdx)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	if m.suggestionIdx != 1 {
		t.Fatalf("after wrap up idx = %d, want 1", m.suggestionIdx)
	}
}

func TestTabCompletesAndAppendsSpace(t *testing.T) {
	t.Parallel()
	m := newModel(ui.Options{}, suggestApp(), NewTerminal(ui.Options{}))
	typeRunes(m, "/")

	// Tab with no highlight accepts the first suggestion ("provider").
	m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	if got := m.input.Value(); got != "/provider " {
		t.Fatalf("input after tab = %q, want %q", got, "/provider ")
	}
}

func TestEnterOnTerminalSuggestionSubmits(t *testing.T) {
	t.Parallel()
	m := newModel(ui.Options{}, suggestApp(), NewTerminal(ui.Options{}))
	typeRunes(m, "/")

	// Highlight "quit" (terminal, no trailing space) and press enter.
	m.handleKey(tea.KeyMsg{Type: tea.KeyDown}) // idx 0 provider
	m.handleKey(tea.KeyMsg{Type: tea.KeyDown}) // idx 1 quit
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected submit cmd for terminal suggestion")
	}
	msg := cmd()
	sm, ok := msg.(submitMsg)
	if !ok {
		t.Fatalf("msg type %T, want submitMsg", msg)
	}
	if sm.line != "/quit" {
		t.Fatalf("submitted line = %q, want /quit", sm.line)
	}
	if len(m.suggestions) != 0 {
		t.Error("suggestions should be cleared after submit")
	}
}

func TestEnterOnParentSuggestionDoesNotSubmit(t *testing.T) {
	t.Parallel()
	m := newModel(ui.Options{}, suggestApp(), NewTerminal(ui.Options{}))
	typeRunes(m, "/")

	m.handleKey(tea.KeyMsg{Type: tea.KeyDown}) // highlight "provider" (AppendSpace)
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("parent suggestion should complete, not submit")
	}
	if got := m.input.Value(); got != "/provider " {
		t.Fatalf("input = %q, want %q", got, "/provider ")
	}
}

func TestNoSuggestionsForPlainText(t *testing.T) {
	t.Parallel()
	m := newModel(ui.Options{}, suggestApp(), NewTerminal(ui.Options{}))
	typeRunes(m, "hello")
	if len(m.suggestions) != 0 {
		t.Fatalf("plain text produced %d suggestions", len(m.suggestions))
	}
	// Enter on plain text submits normally.
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected submit for plain text")
	}
}
