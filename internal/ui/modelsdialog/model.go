package modelsdialog

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/undeadindustries/sagittarius/internal/ui/theme"
)

type screen int

const (
	screenList      screen = iota // global {Provider}/{Model} list
	screenSetting                 // per-model settings submenu
	screenEditField               // text input for a setting value
)

type settingItem struct {
	label string
	key   string
}

// Model is the per-model settings editor overlay.
type Model struct {
	deps Deps
	ctx  context.Context
	th   theme.Theme

	width  int
	height int

	entries []ModelEntry
	cursor  int
	screen  screen
	done    bool
	status  string

	// target is the model whose settings are being edited.
	targetProvider string
	targetModel    string

	settingItems  []settingItem
	settingValues map[string]string // current values for the target model

	// for screenEditField
	editKey   string
	editTitle string
	input     textinput.Model

	errMsg string
	info   string
}

// settingsMenu returns the ordered per-model settings rows.
var settingsMenu = []settingItem{
	{label: "temperature", key: "temperature"},
	{label: "contextLimit (tokens)", key: "contextLimit"},
	{label: "reasoningEffort", key: "reasoningEffort"},
	{label: "← Back", key: "back"},
}

// New constructs the per-model settings editor.
func New(ctx context.Context, deps Deps) Model {
	ti := textinput.New()
	ti.CharLimit = 256
	ti.Prompt = "> "

	m := Model{
		deps:   deps,
		ctx:    ctx,
		th:     theme.Default(),
		input:  ti,
		screen: screenList,
	}
	m.entries = deps.ListAllActiveModels()
	if len(m.entries) == 0 {
		m.errMsg = "No active models. Open /providers and activate some first."
	}
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
	if m.width > 0 {
		m.input.Width = m.contentWidth() - 2
	}
	return m
}

// SetTheme applies the resolved color theme.
func (m Model) SetTheme(th theme.Theme) Model {
	m.th = th
	return m
}

// Update advances the settings editor for one message.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	if m.screen == screenEditField {
		return m.handleEditFieldKey(key)
	}
	switch key.String() {
	case "esc", "q":
		return m.back()
	case "up", "k":
		m.cursor = wrapDec(m.cursor, m.listLen())
		return m, nil
	case "down", "j":
		m.cursor = wrapInc(m.cursor, m.listLen())
		return m, nil
	case "r":
		if m.screen == screenSetting {
			return m.clearCurrentSetting()
		}
	case "enter":
		return m.selectCurrent()
	}
	return m, nil
}

func (m Model) back() (Model, tea.Cmd) {
	m.errMsg = ""
	m.info = ""
	switch m.screen {
	case screenList:
		m.done = true
	case screenSetting:
		m.screen = screenList
		m.cursor = m.targetIndex()
	case screenEditField:
		m.screen = screenSetting
		m.cursor = 0
		m.input.Blur()
	}
	return m, nil
}

func (m Model) selectCurrent() (Model, tea.Cmd) {
	switch m.screen {
	case screenList:
		return m.openSettingsFor(m.cursor)
	case screenSetting:
		return m.selectSetting()
	}
	return m, nil
}

func (m Model) openSettingsFor(idx int) (Model, tea.Cmd) {
	if idx < 0 || idx >= len(m.entries) {
		return m, nil
	}
	e := m.entries[idx]
	m.targetProvider = e.ProviderID
	m.targetModel = e.Model
	m.settingItems = settingsMenu
	m.settingValues = m.deps.GetModelSettings(e.ProviderID, e.Model)
	m.screen = screenSetting
	m.cursor = 0
	m.errMsg = ""
	m.info = ""
	return m, nil
}

func (m Model) selectSetting() (Model, tea.Cmd) {
	if m.cursor < 0 || m.cursor >= len(m.settingItems) {
		return m, nil
	}
	item := m.settingItems[m.cursor]
	m.errMsg = ""
	m.info = ""
	switch item.key {
	case "back":
		return m.back()
	default:
		return m.openEditField(item)
	}
}

func (m Model) openEditField(item settingItem) (Model, tea.Cmd) {
	cur := m.settingValues[item.key]
	m.editKey = item.key
	m.editTitle = fmt.Sprintf("Set %s for %s", item.label, m.targetModel)
	m.input.SetValue(cur)
	m.input.Focus()
	m.screen = screenEditField
	return m, nil
}

func (m Model) handleEditFieldKey(key tea.KeyMsg) (Model, tea.Cmd) {
	switch key.String() {
	case "esc":
		m.screen = screenSetting
		m.input.Blur()
		m.errMsg = ""
		return m, nil
	case "enter":
		return m.commitEditField()
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(key)
	return m, cmd
}

func (m Model) commitEditField() (Model, tea.Cmd) {
	value := m.input.Value()
	if err := m.deps.SetModelSetting(m.ctx, m.targetProvider, m.targetModel, m.editKey, value); err != nil {
		m.errMsg = err.Error()
		return m, nil
	}
	m.settingValues = m.deps.GetModelSettings(m.targetProvider, m.targetModel)
	m.info = fmt.Sprintf("%s → %s", m.editKey, value)
	m.status = m.info
	m.screen = screenSetting
	m.cursor = 0
	m.input.Blur()
	return m, nil
}

func (m Model) clearCurrentSetting() (Model, tea.Cmd) {
	if m.cursor < 0 || m.cursor >= len(m.settingItems) {
		return m, nil
	}
	item := m.settingItems[m.cursor]
	if item.key == "back" {
		return m, nil
	}
	if err := m.deps.ClearModelSetting(m.ctx, m.targetProvider, m.targetModel, item.key); err != nil {
		m.errMsg = err.Error()
		return m, nil
	}
	m.settingValues = m.deps.GetModelSettings(m.targetProvider, m.targetModel)
	m.info = fmt.Sprintf("%s reset to default.", item.key)
	m.errMsg = ""
	return m, nil
}

// targetIndex returns the cursor position for the currently-edited target in the list.
func (m Model) targetIndex() int {
	for i, e := range m.entries {
		if e.ProviderID == m.targetProvider && e.Model == m.targetModel {
			return i
		}
	}
	return 0
}

func (m Model) listLen() int {
	switch m.screen {
	case screenList:
		return len(m.entries)
	case screenSetting:
		return len(m.settingItems)
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

func (m Model) contentWidth() int {
	w := m.width - 4
	if w < 20 {
		return 20
	}
	return w
}
