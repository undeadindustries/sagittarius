package systempromptdialog

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/ui/theme"
)

type presetOption struct {
	id    string
	label string
}

// Model is the project system-prompt preset picker overlay.
type Model struct {
	deps Deps
	ctx  context.Context
	th   theme.Theme

	width  int
	height int

	options []presetOption
	cursor  int
	done    bool
	status  string
	errMsg  string
	info    string

	spin     spinner.Model
	applying bool
}

// applyResultMsg carries the outcome of an off-Update ApplyPreset call (which
// reloads the system instruction and rebuilds the runner) so it never blocks the
// UI loop.
type applyResultMsg struct {
	info  string
	label string
	err   error
}

// New constructs the project system-prompt picker.
func New(ctx context.Context, deps Deps) Model {
	presets := config.SortedSystemPromptPresets()
	opts := make([]presetOption, len(presets))
	for i, p := range presets {
		opts[i] = presetOption{id: p.ID, label: p.Label}
	}
	cursor := 0
	if id := deps.CurrentPresetID(); id != "" {
		for i, o := range opts {
			if o.id == id {
				cursor = i
				break
			}
		}
	}
	return Model{
		deps:    deps,
		ctx:     ctx,
		th:      theme.Default(),
		options: opts,
		cursor:  cursor,
		spin:    spinner.New(spinner.WithSpinner(spinner.MiniDot)),
	}
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
	if m.applying {
		return m, nil
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "esc", "q":
		m.done = true
		return m, nil
	case "up", "k":
		if len(m.options) > 0 {
			m.cursor = (m.cursor - 1 + len(m.options)) % len(m.options)
		}
		return m, nil
	case "down", "j":
		if len(m.options) > 0 {
			m.cursor = (m.cursor + 1) % len(m.options)
		}
		return m, nil
	case "enter":
		return m.applyCurrent()
	}
	return m, nil
}

func (m Model) applyCurrent() (Model, tea.Cmd) {
	if m.cursor < 0 || m.cursor >= len(m.options) {
		return m, nil
	}
	p := m.options[m.cursor]
	ctx := m.ctx
	deps := m.deps
	m.applying = true
	m.errMsg = ""
	m.info = fmt.Sprintf("Applying %s…", p.label)
	return m, tea.Batch(
		m.spin.Tick,
		func() tea.Msg {
			info, err := deps.ApplyPreset(ctx, p.id)
			return applyResultMsg{info: info, label: p.label, err: err}
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
	m.info = msg.info
	m.status = fmt.Sprintf("System prompt → %s", msg.label)
	m.done = true
	return m, nil
}
