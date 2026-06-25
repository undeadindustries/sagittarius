package settingsdialog

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/ui/scopedialog"
	"github.com/undeadindustries/sagittarius/internal/ui/theme"
)

// Model is the /settings curated browser overlay.
type Model struct {
	deps Deps
	ctx  context.Context
	th   theme.Theme

	width  int
	height int

	entries      []SettingEntry
	cursor       int
	scopeSel     scopedialog.ScopeSelector
	scopeFocused bool

	editing  bool
	editKey  string
	editKind SettingKind
	choices  []string
	input    textinput.Model

	info   string
	errMsg string
	done   bool
	status string
}

// New constructs the settings browser with an initial scope.
func New(ctx context.Context, deps Deps) Model {
	sel := scopedialog.NewScopeSelector(config.ScopeGlobal)
	if !deps.ProjectAvailable() {
		sel.Disabled = true
	}
	m := Model{
		deps:     deps,
		ctx:      ctx,
		th:       theme.Default(),
		scopeSel: sel,
	}
	m.entries = deps.ListSettings(m.scopeSel.Scope)
	return m
}

// Done reports whether the overlay should be removed.
func (m Model) Done() bool { return m.done }

// Status returns a one-line message to surface after close.
func (m Model) Status() string { return m.status }

// SetSize informs the dialog of the terminal dimensions.
func (m Model) SetSize(w, h int) Model { m.width = w; m.height = h; return m }

// SetTheme applies the resolved color theme.
func (m Model) SetTheme(th theme.Theme) Model { m.th = th; return m }

// Update advances the dialog for one message.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	// When the scope selector is focused, delegate to it and reload entries on change.
	if _, ok := msg.(scopedialog.ScopeChangedMsg); ok {
		m.entries = m.deps.ListSettings(m.scopeSel.Scope)
		return m, nil
	}
	if m.scopeFocused {
		sel, cmd := m.scopeSel.Update(msg)
		m.scopeSel = sel
		return m, cmd
	}
	if m.editing {
		return m.updateEdit(msg)
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	// Tab toggles scope focus.
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
		m.moveCursor(-1)
		return m, nil
	case "down", "j":
		m.moveCursor(1)
		return m, nil
	case "enter", " ":
		return m.activateCurrent(key.String())
	case "ctrl+l":
		return m.clearCurrent()
	}
	return m, nil
}

func (m *Model) moveCursor(delta int) {
	n := len(m.entries)
	if n == 0 {
		return
	}
	for {
		m.cursor = (m.cursor + delta + n) % n
		if !m.entries[m.cursor].IsHeader() {
			break
		}
	}
}

func (e SettingEntry) IsHeader() bool { return e.Kind == KindHeader }

func (m Model) currentEntry() (SettingEntry, bool) {
	if m.cursor < 0 || m.cursor >= len(m.entries) {
		return SettingEntry{}, false
	}
	e := m.entries[m.cursor]
	if e.IsHeader() || e.ReadOnly {
		return SettingEntry{}, false
	}
	return e, true
}

func (m Model) activateCurrent(keyStr string) (Model, tea.Cmd) {
	e, ok := m.currentEntry()
	if !ok {
		return m, nil
	}
	switch e.Kind {
	case KindBool:
		// Toggle the bool inline.
		newVal := "true"
		if e.Value == "true" {
			newVal = "false"
		}
		return m.saveValue(e.Key, newVal)
	case KindEnum:
		if keyStr == "enter" {
			next := nextChoice(e.Value, e.Choices)
			return m.saveValue(e.Key, next)
		}
	case KindInt, KindString:
		if keyStr == "enter" {
			return m.startEdit(e)
		}
	}
	return m, nil
}

func nextChoice(current string, choices []string) string {
	for i, c := range choices {
		if c == current {
			return choices[(i+1)%len(choices)]
		}
	}
	if len(choices) > 0 {
		return choices[0]
	}
	return current
}

func (m Model) startEdit(e SettingEntry) (Model, tea.Cmd) {
	ti := textinput.New()
	ti.SetValue(e.Value)
	ti.Focus()
	m.editing = true
	m.editKey = e.Key
	m.editKind = e.Kind
	m.choices = e.Choices
	m.input = ti
	m.errMsg = ""
	return m, textinput.Blink
}

func (m Model) updateEdit(msg tea.Msg) (Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if ok {
		switch key.String() {
		case "esc":
			m.editing = false
			return m, nil
		case "enter":
			return m.commitEdit()
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) commitEdit() (Model, tea.Cmd) {
	v := strings.TrimSpace(m.input.Value())
	if m.editKind == KindInt {
		if _, err := strconv.Atoi(v); err != nil {
			m.errMsg = fmt.Sprintf("must be an integer, got %q", v)
			return m, nil
		}
	}
	m.editing = false
	return m.saveValue(m.editKey, v)
}

func (m Model) saveValue(key, value string) (Model, tea.Cmd) {
	scope := m.scopeSel.Scope
	if err := m.deps.SetValue(m.ctx, scope, key, value); err != nil {
		m.errMsg = err.Error()
		return m, nil
	}
	m.info = fmt.Sprintf("Saved %s = %s (%s).", key, value, scope)
	m.errMsg = ""
	m.entries = m.deps.ListSettings(scope)
	return m, nil
}

func (m Model) clearCurrent() (Model, tea.Cmd) {
	e, ok := m.currentEntry()
	if !ok {
		return m, nil
	}
	scope := m.scopeSel.Scope
	if err := m.deps.ClearValue(m.ctx, scope, e.Key); err != nil {
		m.errMsg = err.Error()
		return m, nil
	}
	m.info = fmt.Sprintf("Cleared %s from %s settings.", e.Key, scope)
	m.errMsg = ""
	m.entries = m.deps.ListSettings(scope)
	return m, nil
}
