// Package screendialog is the one-time overlay that offers inline mode after
// the first OSC 52 copy of an SSH session.
package screendialog

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/undeadindustries/sagittarius/internal/ui"
	"github.com/undeadindustries/sagittarius/internal/ui/overlay"
	"github.com/undeadindustries/sagittarius/internal/ui/theme"
)

// ChoiceKeepFullscreen leaves the session on the full-screen layout.
// ChoiceInline switches this session to inline and asks the caller to save
// ui.screen=auto so later SSH sessions start inline.
const (
	ChoiceKeepFullscreen = "keep"
	ChoiceInline         = "inline"
)

// Model is the "Copying over SSH" overlay. hint is the terminal's
// native-selection phrase, from clipboard.NativeSelectionHint.
type Model struct {
	th     theme.Theme
	width  int
	height int
	hint   string
	cursor int
	done   bool
	choice string
}

// New returns the dialog. hint is shown in the "select with ..." sentence.
func New(hint string) Model {
	return Model{th: theme.Default(), hint: strings.TrimSpace(hint), choice: ChoiceKeepFullscreen}
}

// SetSize informs the dialog of the terminal dimensions.
func (m Model) SetSize(w, h int) Model {
	m.width = w
	m.height = h
	return m
}

// SetTheme applies the resolved color theme.
func (m Model) SetTheme(th theme.Theme) Model {
	m.th = th
	return m
}

// Done reports whether the user has chosen.
func (m Model) Done() bool { return m.done }

// Choice is ChoiceKeepFullscreen or ChoiceInline. Keep is the result of Esc.
func (m Model) Choice() string { return m.choice }

// Update handles arrows, Enter, and Esc. Esc keeps full screen.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok || m.done {
		return m, nil
	}
	switch km.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < 1 {
			m.cursor++
		}
	case "enter":
		m.done = true
		if m.cursor == 1 {
			m.choice = ChoiceInline
		} else {
			m.choice = ChoiceKeepFullscreen
		}
	case "esc":
		m.done = true
		m.choice = ChoiceKeepFullscreen
	}
	return m, nil
}

// View renders the explanation and the two choices.
func (m Model) View() string {
	width := overlay.ContentWidth(m.width, overlay.DefaultMinWidth)
	var b strings.Builder
	b.WriteString(overlay.Title(m.th, "Copying over SSH"))
	b.WriteString("\n\n")
	b.WriteString(ui.WrapText(explainCopy(), width))
	b.WriteString("\n\n")
	b.WriteString(ui.WrapText(explainSelect(m.hint), width))
	b.WriteString("\n\n")
	b.WriteString(ui.WrapText(explainInline(), width))
	b.WriteString("\n\n")
	b.WriteString(overlay.Row(m.th, "Keep full screen", m.cursor == 0))
	b.WriteString("\n")
	b.WriteString(overlay.Row(m.th, "Use inline mode over SSH", m.cursor == 1))
	b.WriteString("\n\n")
	b.WriteString(overlay.Hints(m.th, "↑/↓ move • Enter select • Esc keeps full screen"))
	return overlay.Frame(m.th, m.width, overlay.DefaultMinWidth, b.String())
}

func explainCopy() string {
	return "That copy went out over OSC 52. It reaches your clipboard only when your terminal allows it. Terminal.app never does. iTerm2 does after you turn on Settings > General > Selection > Applications in terminal may access clipboard."
}

func explainSelect(hint string) string {
	if hint == "" {
		hint = "Fn in Terminal.app, Option in iTerm2, Shift in most others"
	}
	return fmt.Sprintf("To copy with your terminal instead, select with %s. That copies what is on screen.", hint)
}

func explainInline() string {
	return "Or use inline mode over SSH. Finished output goes into your terminal's scrollback, so you can scroll, search, and select all of it. Dialogs such as /providers still open full screen."
}
