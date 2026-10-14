package bubbletea

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/undeadindustries/sagittarius/internal/ui"
	"github.com/undeadindustries/sagittarius/internal/ui/sessionpickdialog"
)

// sessionPickApp is a quitApp that also supplies the /chat resume switcher's
// catalog, so the overlay can be driven end to end without a real session dir.
type sessionPickApp struct {
	quitApp
	deps sessionpickdialog.Deps
}

func (a sessionPickApp) SessionPickDialogDeps() sessionpickdialog.Deps { return a.deps }

type stubSessionPickDeps struct {
	cat sessionpickdialog.Catalog
}

func (s stubSessionPickDeps) Load(context.Context) (sessionpickdialog.Catalog, error) {
	return s.cat, nil
}

func sessionPickCatalog(currentID string) sessionpickdialog.Catalog {
	return sessionpickdialog.Catalog{
		Sessions:    []sessionpickdialog.SessionEntry{{Index: 1, ID: "sess-a", Title: "Alpha work", MessageCount: 3}},
		Checkpoints: []string{"beta"},
		CurrentID:   currentID,
	}
}

// flattenCmdMsgs runs cmd and every command it batches, returning all messages.
// Batched commands come back as tea.BatchMsg, so this is how a test observes what
// the dialog's load or the parent's submit would have sent to Update.
func flattenCmdMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	var out []tea.Msg
	for _, sub := range batch {
		out = append(out, flattenCmdMsgs(sub)...)
	}
	return out
}

// loadSessionPick opens the switcher and feeds its async load back into Update,
// the same sequence the bubbletea runtime performs.
func loadSessionPick(t *testing.T, m *model) {
	t.Helper()
	cmd := m.openDialog(ui.DialogSessionPick)
	if m.sessionPickOverlay == nil {
		t.Fatal("session switcher overlay was not opened")
	}
	for _, msg := range flattenCmdMsgs(cmd) {
		m.Update(msg)
	}
}

func newSessionPickModel(currentID string) *model {
	app := sessionPickApp{deps: stubSessionPickDeps{cat: sessionPickCatalog(currentID)}}
	m := newModel(ui.Options{}, app, NewTerminal(ui.Options{}))
	m.ctx = context.Background()
	m.width = 100
	m.height = 40
	return m
}

func submittedLines(msgs []tea.Msg) []string {
	var lines []string
	for _, msg := range msgs {
		if sub, ok := msg.(submitMsg); ok {
			lines = append(lines, sub.line)
		}
	}
	return lines
}

func TestSessionPickOpensAndRendersCatalog(t *testing.T) {
	t.Parallel()
	m := newSessionPickModel("sess-current")
	loadSessionPick(t, m)
	// The title is gradient-colored per character when a truecolor profile is
	// active (other tests in this package set one), so match on plain text.
	view := stripANSI(m.View())
	for _, want := range []string{"Switch session", "Alpha work", "beta"} {
		if !strings.Contains(view, want) {
			t.Errorf("switcher view missing %q", want)
		}
	}
}

func TestSessionPickEscClosesWithoutSubmitting(t *testing.T) {
	t.Parallel()
	m := newSessionPickModel("sess-current")
	loadSessionPick(t, m)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.sessionPickOverlay != nil {
		t.Fatal("Esc must close the session switcher")
	}
	if lines := submittedLines(flattenCmdMsgs(cmd)); len(lines) != 0 {
		t.Errorf("Esc submitted %v, want nothing", lines)
	}
}

func TestSessionPickEnterSubmitsResumeLine(t *testing.T) {
	t.Parallel()
	m := newSessionPickModel("sess-current")
	loadSessionPick(t, m)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.sessionPickOverlay != nil {
		t.Fatal("a successful pick must close the switcher")
	}
	lines := submittedLines(flattenCmdMsgs(cmd))
	if len(lines) != 1 || lines[0] != "/chat resume sess-a" {
		t.Errorf("submitted lines = %v, want [/chat resume sess-a]", lines)
	}
}

func TestSessionPickRefusedPickKeepsOverlayOpen(t *testing.T) {
	t.Parallel()
	// The row under the cursor is the current session, so the pick is refused
	// and must not submit anything or close the menu.
	m := newSessionPickModel("sess-a")
	loadSessionPick(t, m)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.sessionPickOverlay == nil {
		t.Fatal("refused pick must keep the switcher open")
	}
	if lines := submittedLines(flattenCmdMsgs(cmd)); len(lines) != 0 {
		t.Errorf("refused pick submitted %v, want nothing", lines)
	}
}

func TestSessionPickUnavailableWithoutHost(t *testing.T) {
	t.Parallel()
	m := newModel(ui.Options{}, quitApp{}, NewTerminal(ui.Options{}))
	m.ctx = context.Background()
	if cmd := m.openDialog(ui.DialogSessionPick); cmd != nil {
		t.Errorf("unavailable switcher returned a command")
	}
	if m.sessionPickOverlay != nil {
		t.Fatal("switcher must not open without a dialog host")
	}
}
