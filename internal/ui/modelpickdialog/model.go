package modelpickdialog

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/ui/scopedialog"
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

	cursor       int
	scopeSel     scopedialog.ScopeSelector
	scopeFocused bool
	done         bool
	status       string

	spin     spinner.Model
	applying bool

	errMsg string
	info   string
}

// applyResultMsg carries the outcome of an off-Update SelectCurrentModel call so
// a cold provider switch (DNS/TLS/genai client build) never blocks the UI loop.
type applyResultMsg struct {
	providerID string
	displayID  string
	model      string
	scope      config.SettingScope
	err        error
}

// New constructs the global model picker.
func New(ctx context.Context, deps Deps) Model {
	sel := scopedialog.NewScopeSelector(config.ScopeProject)
	if !deps.ProjectAvailable() {
		sel.Disabled = true
	}
	m := Model{
		deps:     deps,
		ctx:      ctx,
		th:       theme.Default(),
		scopeSel: sel,
		spin:     newDialogSpinner(),
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
	// mid-rebuild (spinner keeps animating via the TickMsg case above).
	if m.applying {
		return m, nil
	}
	if _, ok := msg.(scopedialog.ScopeChangedMsg); ok {
		return m, nil
	}
	// Delegate to scope selector when it's focused.
	if m.scopeFocused {
		sel, cmd := m.scopeSel.Update(msg)
		m.scopeSel = sel
		return m, cmd
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	if key.String() == "tab" && !m.scopeSel.Disabled {
		m.scopeFocused = !m.scopeFocused
		if m.scopeFocused {
			m.scopeSel.Focus()
		} else {
			m.scopeSel.Blur()
		}
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
	scope := m.scopeSel.Scope
	ctx := m.ctx
	deps := m.deps
	m.applying = true
	m.errMsg = ""
	m.info = fmt.Sprintf("Switching to %s/%s…", e.DisplayID, e.Model)
	m.scopeFocused = false
	m.scopeSel.Blur()
	return m, tea.Batch(
		m.spin.Tick,
		func() tea.Msg {
			err := deps.SelectCurrentModel(ctx, e.ProviderID, e.Model, scope)
			return applyResultMsg{
				providerID: e.ProviderID,
				displayID:  e.DisplayID,
				model:      e.Model,
				scope:      scope,
				err:        err,
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
	m.curProvider = msg.providerID
	m.curModel = msg.model
	m.status = fmt.Sprintf("Model → %s/%s. (%s)", msg.displayID, msg.model, msg.scope)
	m.info = m.status
	m.errMsg = ""
	return m, nil
}

// newDialogSpinner returns the small braille-dot spinner used while an apply runs.
func newDialogSpinner() spinner.Model {
	return spinner.New(spinner.WithSpinner(spinner.MiniDot))
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
