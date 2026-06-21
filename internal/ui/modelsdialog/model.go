package modelsdialog

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// Model is the models picker overlay. It is driven by the parent Bubble Tea
// model exactly like the providers wizard: the parent forwards messages to
// Update while active and renders View; when Done reports true the parent
// removes the overlay.
type Model struct {
	deps Deps
	ctx  context.Context

	width  int
	height int

	providerID    string
	providerLabel string
	models        []string
	current       string

	cursor int
	done   bool
	status string

	errMsg string
	info   string
}

// New constructs the picker for the active provider's active models.
func New(ctx context.Context, deps Deps) Model {
	id := deps.ActiveProviderID()
	m := Model{
		deps:          deps,
		ctx:           ctx,
		providerID:    id,
		providerLabel: deps.ActiveProviderLabel(),
	}
	if id == "" {
		m.errMsg = "No active provider. Open /providers to switch or add one."
		return m
	}
	m.models = deps.ActiveModels(id)
	m.current = deps.CurrentModel(id)
	m.cursor = indexOf(m.models, m.current)
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
		m.cursor = wrapDec(m.cursor, len(m.models))
		return m, nil
	case "down", "j":
		m.cursor = wrapInc(m.cursor, len(m.models))
		return m, nil
	case "enter":
		return m.selectCurrent()
	}
	return m, nil
}

func (m Model) selectCurrent() (Model, tea.Cmd) {
	if m.cursor < 0 || m.cursor >= len(m.models) {
		return m, nil
	}
	model := m.models[m.cursor]
	if err := m.deps.SetModel(m.ctx, m.providerID, model); err != nil {
		m.errMsg = err.Error()
		return m, nil
	}
	m.current = model
	m.status = fmt.Sprintf("Model → %s for %s.", model, m.providerLabel)
	m.info = m.status
	m.errMsg = ""
	return m, nil
}

func indexOf(items []string, target string) int {
	for i, it := range items {
		if it == target {
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
