package bubbletea

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
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
	// handleSubmit batches the stream pump with the working-spinner tick; pull
	// the stream event out of the batch.
	evMsg, ok := findStreamEventMsg(cmd())
	if !ok {
		t.Fatalf("no streamEventMsg in submit cmd")
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
	m.activeStreamGen = 1

	cmd := waitStream(events, 1)
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

func TestScrollbackRolesRender(t *testing.T) {
	t.Parallel()
	m := newModel(ui.Options{ThemeName: "greyscale"}, quitApp{}, NewTerminal(ui.Options{}))
	m.width = 80
	m.height = 24

	m.addBlock(roleUser, "hello there")
	m.addResponseDelta("Hi, ")
	m.addResponseDelta("how can I help?")
	m.closeResponse()
	m.addBlock(roleInfo, "context reloaded")
	m.addBlock(roleError, "boom")

	out := stripANSI(m.renderScrollback(80))
	for _, want := range []string{"You › hello there", "✦ Hi, how can I help?", "ℹ context reloaded", "✕ boom"} {
		if !strings.Contains(out, want) {
			t.Errorf("scrollback missing %q\n%s", want, out)
		}
	}
}

// stripANSI removes SGR escape sequences so tests can assert on plain text.
var ansiSeq = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string { return ansiSeq.ReplaceAllString(s, "") }

// findStreamEventMsg resolves a (possibly batched) message to the first
// streamEventMsg it yields, evaluating batched sub-commands as needed.
func findStreamEventMsg(msg tea.Msg) (streamEventMsg, bool) {
	switch v := msg.(type) {
	case streamEventMsg:
		return v, true
	case tea.BatchMsg:
		for _, c := range v {
			if c == nil {
				continue
			}
			if ev, ok := findStreamEventMsg(c()); ok {
				return ev, true
			}
		}
	}
	return streamEventMsg{}, false
}

func TestResponseDeltasAccumulateIntoOneBlock(t *testing.T) {
	t.Parallel()
	m := newModel(ui.Options{}, quitApp{}, NewTerminal(ui.Options{}))
	m.addResponseDelta("a")
	m.addResponseDelta("b")
	m.addResponseDelta("c")
	if got := len(m.blocks); got != 1 {
		t.Fatalf("blocks = %d, want 1 accumulated response block", got)
	}
	if m.blocks[0].text != "abc" || m.blocks[0].role != roleResponse {
		t.Fatalf("block = %+v, want {roleResponse abc}", m.blocks[0])
	}
	// A non-text block must close the response so the next delta starts fresh.
	m.addBlock(roleInfo, "x")
	m.addResponseDelta("d")
	if got := len(m.blocks); got != 3 {
		t.Fatalf("blocks = %d, want 3 (response, info, response)", got)
	}
}

func TestConfirmCardVisibleWhilePending(t *testing.T) {
	t.Parallel()
	m := newModel(ui.Options{ThemeName: "greyscale"}, quitApp{}, NewTerminal(ui.Options{}))
	m.width = 80
	m.height = 24

	reply := make(chan ui.ConfirmDecision, 1)
	m.handleStream(ui.StreamEvent{Type: ui.StreamToolStart, ToolName: "write_file", Text: "/tmp/x", ToolCallID: "c1"})
	m.handleStream(ui.StreamEvent{Type: ui.StreamToolConfirm, ToolName: "write_file", Text: "/tmp/x", ToolCallID: "c1", ConfirmReply: reply})
	out := stripANSI(m.renderScrollback(80))
	if !strings.Contains(out, "Write file") || !strings.Contains(out, "Allow for this session") {
		t.Fatalf("confirming card missing prompt:\n%s", out)
	}

	// "y" approves once and clears the pending confirmation.
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if got := <-reply; got != ui.ConfirmOnce {
		t.Fatalf("expected y to send ConfirmOnce, got %v", got)
	}
	if m.confirmReply != nil {
		t.Fatal("confirm should clear after answer")
	}
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

// mentionApp is a quitApp that serves "@path" mention completions, used to
// exercise the inline mention-suggestion UI.
type mentionApp struct {
	quitApp
}

func (mentionApp) CompleteMention(input string, cursor int) ui.Completions {
	at := strings.LastIndexByte(input[:cursor], '@')
	if at < 0 {
		return ui.Completions{}
	}
	return ui.Completions{
		Items: []ui.Suggestion{
			{Label: "internal/agent/app.go", Insert: "internal/agent/app.go", AppendSpace: true},
		},
		ReplaceFrom: at + 1,
	}
}

func TestMentionSuggestionsAppearOnAt(t *testing.T) {
	t.Parallel()
	m := newModel(ui.Options{}, mentionApp{}, NewTerminal(ui.Options{}))
	typeRunes(m, "explain @int")
	if len(m.suggestions) != 1 {
		t.Fatalf("mention suggestions = %d, want 1", len(m.suggestions))
	}
}

func TestMentionTabReplacesPartial(t *testing.T) {
	t.Parallel()
	m := newModel(ui.Options{}, mentionApp{}, NewTerminal(ui.Options{}))
	typeRunes(m, "explain @int")
	m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	if got := m.input.Value(); got != "explain @internal/agent/app.go " {
		t.Fatalf("input after tab = %q, want %q", got, "explain @internal/agent/app.go ")
	}
}

func TestClearScrollbackReplacesHistory(t *testing.T) {
	t.Parallel()
	m := newModel(ui.Options{}, quitApp{}, NewTerminal(ui.Options{}))
	m.addBlock(roleUser, "old turn")
	m.addBlock(roleResponse, "old reply")

	m.handleStream(ui.StreamEvent{Type: ui.StreamClearScrollback})
	if len(m.blocks) != 0 {
		t.Fatalf("blocks after clear = %d, want 0", len(m.blocks))
	}

	m.handleStream(ui.StreamEvent{Type: ui.StreamScrollback, Text: "restored", ScrollbackRole: ui.ScrollbackUser})
	if len(m.blocks) != 1 || m.blocks[0].text != "restored" {
		t.Fatalf("blocks after repaint = %+v, want single restored block", m.blocks)
	}
}

func TestInputByteCursorTracksCursor(t *testing.T) {
	t.Parallel()
	m := newModel(ui.Options{}, mentionApp{}, NewTerminal(ui.Options{}))
	typeRunes(m, "explain @int")
	if got := m.inputByteCursor(); got != 12 {
		t.Fatalf("cursor at end = %d, want 12", got)
	}
	for i := 0; i < 2; i++ {
		m.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	}
	if got := m.inputByteCursor(); got != 10 {
		t.Fatalf("cursor after 2 lefts = %d, want 10", got)
	}
}

func TestMentionTabPreservesSuffix(t *testing.T) {
	t.Parallel()
	m := newModel(ui.Options{}, mentionApp{}, NewTerminal(ui.Options{}))
	typeRunes(m, "explain @int here")
	// Move the cursor back to just after "@int" (before " here", 5 runes).
	for i := 0; i < 5; i++ {
		m.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	want := "explain @internal/agent/app.go  here"
	if got := m.input.Value(); got != want {
		t.Fatalf("input after mid-line tab = %q, want %q", got, want)
	}
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

type longSuggestApp struct {
	quitApp
}

func (longSuggestApp) Complete(input string) ui.Completions {
	if !strings.HasPrefix(input, "/") {
		return ui.Completions{}
	}
	items := make([]ui.Suggestion, 10)
	for i := 0; i < 10; i++ {
		items[i] = ui.Suggestion{Label: fmt.Sprintf("cmd%d", i), Insert: fmt.Sprintf("cmd%d", i)}
	}
	return ui.Completions{Items: items, ReplaceFrom: 1}
}

func TestSuggestionScrolling(t *testing.T) {
	t.Parallel()
	m := newModel(ui.Options{}, longSuggestApp{}, NewTerminal(ui.Options{}))
	typeRunes(m, "/")

	// Initially no highlight, showing top 8.
	rendered := m.renderSuggestions()
	if !strings.Contains(rendered, "cmd0") || !strings.Contains(rendered, "cmd7") {
		t.Errorf("expected cmd0..cmd7 in initial render")
	}
	if !strings.Contains(rendered, "↓ 2 more") {
		t.Errorf("expected bottom indicator '↓ 2 more', got:\n%s", rendered)
	}

	// Arrow up to wrap to the last item (index 9).
	m.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	if m.suggestionIdx != 9 {
		t.Fatalf("expected suggestionIdx 9, got %d", m.suggestionIdx)
	}

	rendered = m.renderSuggestions()
	if !strings.Contains(rendered, "cmd2") || !strings.Contains(rendered, "cmd9") {
		t.Errorf("expected cmd2..cmd9 in wrapped render")
	}
	if !strings.Contains(rendered, "↑ 2 more") {
		t.Errorf("expected top indicator '↑ 2 more', got:\n%s", rendered)
	}
	if strings.Contains(rendered, "↓") {
		t.Errorf("unexpected bottom indicator when at bottom")
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
