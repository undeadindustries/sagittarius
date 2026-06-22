package modesdialog

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/undeadindustries/sagittarius/internal/ui/theme"
)

type screen int

const (
	screenModes  screen = iota // list of modes with current overrides
	screenPicker               // global {Provider}/{Model} list for one mode
)

// Model is the mode-override editor overlay.
type Model struct {
	deps Deps
	ctx  context.Context
	th   theme.Theme

	width  int
	height int

	modes     []ModeEntry
	modeCursor int
	screen    screen
	done      bool
	status    string

	// targetMode is the mode name being edited in screenPicker.
	targetMode string

	models     []ModelEntry
	pickCursor int

	errMsg string
	info   string
}

// New constructs the modes-override editor.
func New(ctx context.Context, deps Deps) Model {
	m := Model{
		deps:   deps,
		ctx:    ctx,
		th:     theme.Default(),
		screen: screenModes,
	}
	m.modes = deps.ListModes()
	return m
}

// Done reports whether the dialog has finished.
func (m Model) Done() bool { return m.done }

// Status returns a one-line message to surface after close.
func (m Model) Status() string { return m.status }

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

// Update advances the editor for one message.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	listLen := m.currentListLen()
	switch key.String() {
	case "esc", "q":
		return m.back()
	case "up", "k":
		if m.screen == screenModes {
			m.modeCursor = wrapDec(m.modeCursor, listLen)
		} else {
			m.pickCursor = wrapDec(m.pickCursor, listLen)
		}
		return m, nil
	case "down", "j":
		if m.screen == screenModes {
			m.modeCursor = wrapInc(m.modeCursor, listLen)
		} else {
			m.pickCursor = wrapInc(m.pickCursor, listLen)
		}
		return m, nil
	case "r":
		if m.screen == screenModes {
			return m.clearCurrentOverride()
		}
	case "enter":
		return m.selectCurrent()
	}
	return m, nil
}

func (m Model) back() (Model, tea.Cmd) {
	switch m.screen {
	case screenModes:
		m.done = true
	case screenPicker:
		m.screen = screenModes
	}
	m.errMsg = ""
	m.info = ""
	return m, nil
}

func (m Model) selectCurrent() (Model, tea.Cmd) {
	switch m.screen {
	case screenModes:
		return m.openPicker()
	case screenPicker:
		return m.applyOverride()
	}
	return m, nil
}

func (m Model) openPicker() (Model, tea.Cmd) {
	if m.modeCursor < 0 || m.modeCursor >= len(m.modes) {
		return m, nil
	}
	m.targetMode = m.modes[m.modeCursor].Mode
	m.models = m.deps.AllActiveModels()
	m.pickCursor = m.currentPickIndex()
	m.screen = screenPicker
	m.errMsg = ""
	m.info = ""
	return m, nil
}

func (m Model) currentPickIndex() int {
	cur := m.modes[m.modeCursor]
	for i, e := range m.models {
		if e.ProviderID == cur.Provider && e.Model == cur.Model {
			return i
		}
	}
	return 0
}

func (m Model) applyOverride() (Model, tea.Cmd) {
	if m.pickCursor < 0 || m.pickCursor >= len(m.models) {
		return m, nil
	}
	e := m.models[m.pickCursor]
	if err := m.deps.SetModeOverride(m.ctx, m.targetMode, e.ProviderID, e.Model); err != nil {
		m.errMsg = err.Error()
		return m, nil
	}
	m.modes = m.deps.ListModes()
	m.info = fmt.Sprintf("%s mode → %s/%s.", m.targetMode, e.DisplayID, e.Model)
	m.status = m.info
	m.screen = screenModes
	return m, nil
}

func (m Model) clearCurrentOverride() (Model, tea.Cmd) {
	if m.modeCursor < 0 || m.modeCursor >= len(m.modes) {
		return m, nil
	}
	mode := m.modes[m.modeCursor].Mode
	if err := m.deps.ClearModeOverride(m.ctx, mode); err != nil {
		m.errMsg = err.Error()
		return m, nil
	}
	m.modes = m.deps.ListModes()
	m.info = fmt.Sprintf("%s mode override cleared (uses default).", mode)
	m.status = m.info
	m.errMsg = ""
	return m, nil
}

func (m Model) currentListLen() int {
	switch m.screen {
	case screenModes:
		return len(m.modes)
	case screenPicker:
		return len(m.models)
	}
	return 0
}

func wrapInc(i, n int) int {
	if n <= 0 {
		return 0
	}
	return (i + 1) % n
}

func wrapDec(i, n int) int {
	if n <= 0 {
		return 0
	}
	return (i - 1 + n) % n
}
