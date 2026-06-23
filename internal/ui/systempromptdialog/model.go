package systempromptdialog

import (
	"context"
	"fmt"

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
	info, err := m.deps.ApplyPreset(m.ctx, p.id)
	if err != nil {
		m.errMsg = err.Error()
		return m, nil
	}
	m.info = info
	m.status = fmt.Sprintf("System prompt → %s", p.label)
	m.done = true
	return m, nil
}
