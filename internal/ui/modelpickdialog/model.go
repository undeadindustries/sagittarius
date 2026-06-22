package modelpickdialog

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/undeadindustries/sagittarius/internal/ui/theme"
)

// Model is the global model picker overlay driven by the parent Bubble Tea model.
// It lists all activated (Provider/Model) pairs and selects the current one;
// when Done reports true the parent removes the overlay.
type Model struct {
	deps Deps
	ctx  context.Context
	th   theme.Theme

	width  int
	height int

	entries     []ModelEntry
	curProvider string
	curModel    string

	cursor int
	done   bool
	status string

	errMsg string
	info   string
}

// New constructs the global model picker.
func New(ctx context.Context, deps Deps) Model {
	m := Model{
		deps: deps,
		ctx:  ctx,
		th:   theme.Default(),
	}
	m.entries = deps.AllActiveModels()
	m.curProvider = deps.CurrentProviderID()
	m.curModel = deps.CurrentModel()
	m.cursor = currentIndex(m.entries, m.curProvider, m.curModel)
	if len(m.entries) == 0 {
		m.errMsg = "No active models. Open /providers, select a provider, then activate models."
	}
	return m
}

// Done reports whether the dialog has finished and should be removed.
func (m Model) Done() bool { return m.done }

// Status returns a one-line message to surface after the dialog closes.
func (m Model) Status() string { return m.status }

// SetSize informs the dialog of the terminal dimensions.
func (m Model) SetSize(w, h int) Model {
	m.width = w
	m.height = h
	return m
}

// SetTheme applies the resolved color theme to the overlay.
func (m Model) SetTheme(th theme.Theme) Model {
	m.th = th
	return m
}

// Update advances the picker for one message.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "esc", "q":
		m.done = true
		return m, nil
	case "up", "k":
		m.cursor = wrapDec(m.cursor, len(m.entries))
		return m, nil
	case "down", "j":
		m.cursor = wrapInc(m.cursor, len(m.entries))
		return m, nil
	case "enter":
		return m.selectCurrent()
	}
	return m, nil
}

func (m Model) selectCurrent() (Model, tea.Cmd) {
	if m.cursor < 0 || m.cursor >= len(m.entries) {
		return m, nil
	}
	e := m.entries[m.cursor]
	if err := m.deps.SelectCurrentModel(m.ctx, e.ProviderID, e.Model); err != nil {
		m.errMsg = err.Error()
		return m, nil
	}
	m.curProvider = e.ProviderID
	m.curModel = e.Model
	m.status = fmt.Sprintf("Model → %s/%s.", e.DisplayID, e.Model)
	m.info = m.status
	m.errMsg = ""
	return m, nil
}

func currentIndex(entries []ModelEntry, providerID, model string) int {
	for i, e := range entries {
		if e.ProviderID == providerID && e.Model == model {
			return i
		}
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
