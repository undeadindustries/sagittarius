package modelsdialog

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/ui/overlay"
	"github.com/undeadindustries/sagittarius/internal/ui/scopedialog"
	"github.com/undeadindustries/sagittarius/internal/ui/theme"
)

type screen int

const (
	screenList      screen = iota // global {Provider}/{Model} list
	screenSetting                 // per-model settings submenu
	screenEditField               // text input for a setting value
	screenPickValue               // discrete picker (reasoningEffort)
)

type settingItem struct {
	label string
	key   string
}

// pickOption is one row in screenPickValue.
type pickOption struct {
	label string // displayed
	value string // "" means clear/inherit; "custom" opens text field
}

const (
	pickInheritValue = ""
	pickCustomValue  = "__custom__"
)

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

	// scopeSel picks the file writes land in; it opens on Global because
	// per-model tuning is usually shared across projects.
	scopeSel scopedialog.ScopeSelector

	settingItems  []settingItem
	settingValues map[string]string // values set in the selected scope
	// inheritedValues are the global values a project-scope view falls back
	// to; nil while the selector is on Global.
	inheritedValues map[string]string
	// capabilityHint is a read-only one-line description of the target
	// model's resolved reasoning mechanism, shown above the settings list.
	capabilityHint string

	// for screenEditField
	editKey   string
	editTitle string
	input     textinput.Model

	// for screenPickValue
	pickKey     string
	pickTitle   string
	pickOptions []pickOption

	errMsg string
	info   string
}

// settingsMenu returns the ordered per-model settings rows.
var settingsMenu = []settingItem{
	{label: "temperature", key: "temperature"},
	{label: "contextLimit (tokens)", key: "contextLimit"},
	{label: "reasoningEffort", key: "reasoningEffort"},
	{label: "reasoningHistory", key: "reasoningHistory"},
	{label: "showThinking (true/false)", key: "showThinking"},
	{label: "thinkingBudgetTokens (0 = off)", key: "thinkingBudgetTokens"},
	{label: "hardThinkingBudget (true/false)", key: "hardThinkingBudget"},
	{label: "← Back", key: "back"},
}

// New constructs the per-model settings editor.
func New(ctx context.Context, deps Deps) Model {
	ti := textinput.New()
	ti.CharLimit = 256
	ti.Prompt = "> "
	overlay.StyleTextInput(&ti, theme.Default())

	m := Model{
		deps:     deps,
		ctx:      ctx,
		th:       theme.Default(),
		input:    ti,
		screen:   screenList,
		scopeSel: scopedialog.NewScopeSelector(config.ScopeGlobal),
	}
	m.scopeSel.Disabled = !deps.ProjectAvailable()
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
	overlay.StyleTextInput(&m.input, th)
	return m
}

// Update advances the settings editor for one message.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if _, ok := msg.(scopedialog.ScopeChangedMsg); ok {
		m.reloadValues()
		return m, nil
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	if m.screen == screenEditField {
		return m.handleEditFieldKey(key)
	}
	if sel, cmd, handled := m.scopeSel.HandleKey(key); handled {
		m.scopeSel = sel
		m.info = ""
		return m, cmd
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
	case screenPickValue:
		m.screen = screenSetting
		m.cursor = 0
	}
	return m, nil
}

func (m Model) selectCurrent() (Model, tea.Cmd) {
	switch m.screen {
	case screenList:
		return m.openSettingsFor(m.cursor)
	case screenSetting:
		return m.selectSetting()
	case screenPickValue:
		return m.selectPickOption()
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
	m.reloadValues()
	m.capabilityHint = m.deps.ReasoningCapabilityHint(e.ProviderID, e.Model)
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
	case "reasoningEffort":
		return m.openReasoningPicker()
	case "reasoningHistory":
		return m.openReasoningHistoryPicker()
	default:
		return m.openEditField(item)
	}
}

func (m Model) openReasoningHistoryPicker() (Model, tea.Cmd) {
	opts := []pickOption{
		{label: "default (inherit)", value: pickInheritValue},
		{label: "auto (model-family default + prune)", value: "auto"},
		{label: "keep (replay reasoning_content + prune older)", value: "keep"},
		{label: "keep-all (replay all turns, never prune)", value: "keep-all"},
		{label: "none (strip reasoning_content)", value: "none"},
	}
	m.pickKey = "reasoningHistory"
	m.pickTitle = fmt.Sprintf("Reasoning history for %s", m.targetModel)
	m.pickOptions = opts
	m.cursor = 0
	m.screen = screenPickValue
	return m, nil
}

func (m Model) openReasoningPicker() (Model, tea.Cmd) {
	efforts, defaultEffort, known := m.deps.ReasoningOptions(m.targetProvider, m.targetModel)
	opts := []pickOption{{label: "default (inherit)", value: pickInheritValue}}
	for _, e := range efforts {
		label := e
		if e == defaultEffort && defaultEffort != "" {
			label += " (model default)"
		}
		opts = append(opts, pickOption{label: label, value: e})
	}
	if !known {
		opts = append(opts, pickOption{label: "custom… (unverified)", value: pickCustomValue})
	}
	m.pickKey = "reasoningEffort"
	m.pickTitle = fmt.Sprintf("Reasoning effort for %s", m.targetModel)
	m.pickOptions = opts
	m.cursor = 0
	m.screen = screenPickValue
	return m, nil
}

func (m Model) selectPickOption() (Model, tea.Cmd) {
	if m.cursor < 0 || m.cursor >= len(m.pickOptions) {
		return m, nil
	}
	opt := m.pickOptions[m.cursor]
	switch opt.value {
	case pickInheritValue:
		if err := m.deps.ClearModelSetting(m.ctx, m.targetProvider, m.targetModel, m.pickKey, m.scopeSel.Scope); err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		m.reloadValues()
		m.info = fmt.Sprintf("%s → default (inherit)", m.pickKey)
		m.status = m.info
		m.screen = screenSetting
		m.cursor = 0
		return m, nil
	case pickCustomValue:
		return m.openEditField(settingItem{label: "reasoningEffort (unverified)", key: "reasoningEffort"})
	default:
		if err := m.deps.SetModelSetting(m.ctx, m.targetProvider, m.targetModel, m.pickKey, opt.value, m.scopeSel.Scope); err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		m.reloadValues()
		m.info = fmt.Sprintf("%s → %s", m.pickKey, opt.value)
		m.status = m.info
		m.screen = screenSetting
		m.cursor = 0
		return m, nil
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
	if err := m.deps.SetModelSetting(m.ctx, m.targetProvider, m.targetModel, m.editKey, value, m.scopeSel.Scope); err != nil {
		m.errMsg = err.Error()
		return m, nil
	}
	m.reloadValues()
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
	if err := m.deps.ClearModelSetting(m.ctx, m.targetProvider, m.targetModel, item.key, m.scopeSel.Scope); err != nil {
		m.errMsg = err.Error()
		return m, nil
	}
	m.reloadValues()
	m.info = fmt.Sprintf("%s reset to default.", item.key)
	m.errMsg = ""
	return m, nil
}

// reloadValues re-reads the target model's values for the selected scope, plus
// the global values a project view inherits. No-op before a model is chosen.
func (m *Model) reloadValues() {
	if m.targetProvider == "" {
		return
	}
	scope := m.scopeSel.Scope
	m.settingValues = m.deps.GetModelSettings(m.targetProvider, m.targetModel, scope)
	m.inheritedValues = nil
	if scope == config.ScopeProject {
		m.inheritedValues = m.deps.GetModelSettings(m.targetProvider, m.targetModel, config.ScopeGlobal)
	}
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
	case screenPickValue:
		return len(m.pickOptions)
	}
	return 0
}

func (m Model) contentWidth() int {
	return overlay.ContentWidth(m.width, overlay.DefaultMinWidth)
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
