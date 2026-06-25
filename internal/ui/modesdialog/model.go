package modesdialog

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/ui/scopedialog"
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

	modes      []ModeEntry
	modeCursor int
	screen     screen
	done       bool
	status     string

	// targetMode is the mode name being edited in screenPicker.
	targetMode string

	models     []ModelEntry
	pickCursor int

	// scopeSel controls which settings file the override is written to.
	scopeSel     scopedialog.ScopeSelector
	scopeFocused bool

	spin     spinner.Model
	applying bool

	errMsg string
	info   string
}

// applyResultMsg carries the outcome of an off-Update mode-override write (which
// rebuilds the runner) so a cold provider switch never blocks the UI loop.
type applyResultMsg struct {
	info string
	err  error
}

// New constructs the modes-override editor.
func New(ctx context.Context, deps Deps) Model {
	sel := scopedialog.NewScopeSelector(config.ScopeProject)
	if !deps.ProjectAvailable() {
		sel.Disabled = true
	}
	m := Model{
		deps:     deps,
		ctx:      ctx,
		th:       theme.Default(),
		screen:   screenModes,
		scopeSel: sel,
		spin:     newDialogSpinner(),
	}
	m.modes = deps.ListModes()
	return m
}

// newDialogSpinner returns the small braille-dot spinner used while an apply runs.
func newDialogSpinner() spinner.Model {
	return spinner.New(spinner.WithSpinner(spinner.MiniDot))
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
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if !m.applying {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case applyResultMsg:
		return m.handleApplyResult(msg)
	}
	// While an apply is in flight, swallow input so the selection can't change
	// mid-rebuild (the spinner keeps animating via the TickMsg case above).
	if m.applying {
		return m, nil
	}
	// Scope-changed messages are handled by updating the embedded selector.
	if _, ok := msg.(scopedialog.ScopeChangedMsg); ok {
		return m, nil
	}

	// When the scope selector has focus, delegate navigation to it.
	if m.scopeFocused && m.screen == screenPicker {
		sel, cmd := m.scopeSel.Update(msg)
		m.scopeSel = sel
		return m, cmd
	}

	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	// Tab toggles focus between the model list and the scope selector.
	if key.String() == "tab" && m.screen == screenPicker && !m.scopeSel.Disabled {
		m.scopeFocused = !m.scopeFocused
		if m.scopeFocused {
			m.scopeSel.Focus()
		} else {
			m.scopeSel.Blur()
		}
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
		m.scopeFocused = false
		m.scopeSel.Blur()
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
	// Prepend the sentinel so users can always get back to "no override".
	active := m.deps.AllActiveModels()
	m.models = append([]ModelEntry{{IsClear: true}}, active...)
	m.pickCursor = m.currentPickIndex()
	m.screen = screenPicker
	m.errMsg = ""
	m.info = ""
	return m, nil
}

func (m Model) currentPickIndex() int {
	cur := m.modes[m.modeCursor]
	if cur.Model == "" {
		return 0 // no override → pre-select the "(use default)" sentinel
	}
	for i, e := range m.models {
		if !e.IsClear && e.ProviderID == cur.Provider && e.Model == cur.Model {
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
	if e.IsClear {
		return m.clearCurrentOverride()
	}
	scope := m.scopeSel.Scope
	mode := m.targetMode
	ctx := m.ctx
	deps := m.deps
	m.applying = true
	m.errMsg = ""
	m.info = fmt.Sprintf("Setting %s mode → %s/%s…", mode, e.DisplayID, e.Model)
	m.scopeFocused = false
	m.scopeSel.Blur()
	m.screen = screenModes
	return m, tea.Batch(
		m.spin.Tick,
		func() tea.Msg {
			err := deps.SetModeOverride(ctx, mode, e.ProviderID, e.Model, scope)
			return applyResultMsg{
				info: fmt.Sprintf("%s mode → %s/%s. (%s)", mode, e.DisplayID, e.Model, scope),
				err:  err,
			}
		},
	)
}

func (m Model) clearCurrentOverride() (Model, tea.Cmd) {
	if m.modeCursor < 0 || m.modeCursor >= len(m.modes) {
		return m, nil
	}
	mode := m.modes[m.modeCursor].Mode
	scope := m.scopeSel.Scope
	ctx := m.ctx
	deps := m.deps
	m.applying = true
	m.errMsg = ""
	m.info = fmt.Sprintf("Clearing %s mode override…", mode)
	m.scopeFocused = false
	m.scopeSel.Blur()
	m.screen = screenModes
	return m, tea.Batch(
		m.spin.Tick,
		func() tea.Msg {
			err := deps.ClearModeOverride(ctx, mode, scope)
			return applyResultMsg{
				info: fmt.Sprintf("%s mode override cleared (uses default).", mode),
				err:  err,
			}
		},
	)
}

func (m Model) handleApplyResult(msg applyResultMsg) (Model, tea.Cmd) {
	m.applying = false
	if msg.err != nil {
		m.errMsg = msg.err.Error()
		m.info = ""
		return m, nil
	}
	m.modes = m.deps.ListModes()
	m.info = msg.info
	m.status = msg.info
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
