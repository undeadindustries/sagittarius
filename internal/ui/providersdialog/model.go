package providersdialog

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/ui/overlay"
	"github.com/undeadindustries/sagittarius/internal/ui/theme"
)

type screen int

const (
	// screenEditPick is now the root screen: shows the provider list; Enter = edit
	// sheet, a = add, x = remove custom, Esc = close.
	screenEditPick screen = iota
	screenEdit
	screenEditField
	screenEditPicker
	screenSetKey
	screenAddTemplate
	screenAdd
	screenAddModels
	screenRemove
	screenModels
	screenModelsAdd
)

// editKind classifies an editable row on the edit sheet.
type editKind int

const (
	editAPIKey     editKind = iota // opens Set API key entry
	editModel                      // opens model activation screen
	editOverride                   // providers.<id>.<key> instance override (text)
	editDefn                       // custom definition field
	editWireDefn                   // custom definition wireFormat toggle
	editPreset                     // system-prompt preset picker
	editEnum                       // fixed-choice override picker (toolCallParsing)
	editToggleBool                 // boolean override toggled in place (enableTools)
	editResetAll                   // reset all instance overrides
	editBack
)

type editItem struct {
	label string
	kind  editKind
	key   string // setting/definition key for override/defn/enum/toggle kinds
}

// pickerOption is one choice on the generic enum picker screen.
type pickerOption struct {
	id    string
	label string
}

// addState holds the multi-field add-provider wizard buffers.
type addState struct {
	fieldIdx    int
	displayName string
	hostOrURL   string
	port        string
	idOverride  string // empty → auto-generated from URL at submit
	envVar      string
	apiKey      string
	wire        config.WireFormat
	// presetID is set when the add flow was seeded from a ProviderPreset
	// template; it seeds the suggested provider id and is empty for the blank
	// (field-by-field) flow.
	presetID string
	// note carries the preset's caveat line (if any) for display during the
	// seeded add flow.
	note string
}

const (
	addFieldName = iota
	addFieldHostOrURL
	addFieldPort // shown only when hostOrURL has no port
	addFieldWire
	addFieldEnvVar
	addFieldAPIKey
	addFieldIdOverride // shows auto-generated id; user may change before confirm
	addFieldCount
)

// modelsLoadedMsg is delivered when an async DiscoverModels call completes.
type modelsLoadedMsg struct {
	id     string
	models []string
	err    error
}

// addResultMsg carries the outcome of an off-Update AddCustomProvider write so
// the credential/disk write never blocks the Bubble Tea loop and a spinner can
// animate while it runs.
type addResultMsg struct {
	id  string
	err error
}

// removeResultMsg carries the outcome of an off-Update RemoveCustomProvider
// delete (definition + instance settings + stored key) for the same reason.
type removeResultMsg struct {
	name string
	err  error
}

// Model is the providers wizard overlay. It is driven by the parent Bubble Tea
// model: the parent forwards messages to Update while the dialog is active and
// renders View; when Done reports true the parent removes the overlay.
type Model struct {
	deps Deps
	ctx  context.Context
	th   theme.Theme

	width  int
	height int

	screen screen
	done   bool
	status string // surfaced to the parent footer/scrollback after close

	errMsg string
	info   string

	providers []ProviderEntry
	cursor    int

	targetID       string
	targetIsCustom bool
	targetWire     config.WireFormat

	editItems   []editItem
	editingKey  string
	editingKind editKind

	// picker holds the generic enum-picker state (system prompt / toolCallParsing).
	pickerKey     string // "systemPrompt" or a setting key
	pickerTitle   string
	pickerOptions []pickerOption

	input textinput.Model

	add addState

	// removeTarget is the provider id pending delete confirmation on screenRemove.
	removeTarget string

	// spin animates while an add/remove write is in flight; saving gates it and
	// swallows input so the selection can't change mid-write.
	spin   spinner.Model
	saving bool

	loading   bool
	models    []string
	modelsErr string

	// checked is parallel to models on the activation (screenModels) screen:
	// checked[i] reports whether models[i] is in the curated active set.
	checked []bool

	// listOffset is the first visible row in long scrollable lists (models screen).
	listOffset int

	// modelsFrom records which screen opened the activation screen so back/save
	// returns there: screenEdit (per-provider edit sheet) or screenEditPick (root list).
	modelsFrom screen

	// modelsAddReturn records the screen that opened the manual model-name entry
	// (screenModels activation vs screenAddModels add flow) so commit returns there.
	modelsAddReturn screen
}

// New constructs the wizard at the menu screen with the current provider list.
func New(ctx context.Context, deps Deps) Model {
	ti := textinput.New()
	ti.CharLimit = 8192
	ti.Prompt = "> "

	m := Model{
		deps:      deps,
		ctx:       ctx,
		th:        theme.Default(),
		screen:    screenEditPick,
		input:     ti,
		add:       addState{wire: config.WireFormatOpenAIChat, fieldIdx: addFieldName},
		providers: deps.ListProviders(),
		spin:      newDialogSpinner(),
	}
	m.syncInputWidth()
	return m
}

// newDialogSpinner returns the small braille-dot spinner shown while an
// add/remove write runs (matches the other async overlays).
func newDialogSpinner() spinner.Model {
	return spinner.New(spinner.WithSpinner(spinner.MiniDot))
}

// Done reports whether the dialog has finished and should be removed.
func (m Model) Done() bool { return m.done }

// Status returns a one-line message to surface after the dialog closes.
func (m Model) Status() string { return m.status }

// SetSize informs the dialog of the terminal dimensions.
func (m Model) SetSize(w, h int) Model {
	m.width = w
	m.height = h
	m.syncInputWidth()
	m.ensureListVisible()
	return m
}

// SetTheme applies the resolved color theme to the overlay.
func (m Model) SetTheme(th theme.Theme) Model {
	m.th = th
	return m
}

func (m *Model) syncInputWidth() {
	if m.width <= 0 {
		return
	}
	w := m.contentWidth() - 2 // textinput prompt "> "
	if w < 1 {
		w = 1
	}
	m.input.Width = w
}

func (m Model) contentWidth() int {
	return overlay.ContentWidth(m.width, overlay.DefaultMinWidth)
}

// Update advances the dialog state machine for one message.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if !m.saving {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case addResultMsg:
		return m.handleAddResult(msg)
	case removeResultMsg:
		return m.handleRemoveResult(msg)
	case modelsLoadedMsg:
		return m.handleModelsLoaded(msg), nil
	case tea.KeyMsg:
		// Swallow input while an add/remove write is in flight (the spinner
		// keeps animating via the TickMsg case above).
		if m.saving {
			return m, nil
		}
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleModelsLoaded(msg modelsLoadedMsg) Model {
	if msg.id != m.targetID {
		return m // stale result for a different provider
	}
	m.loading = false
	if msg.err != nil {
		m.modelsErr = msg.err.Error()
	} else {
		m.modelsErr = ""
	}
	m.models = msg.models
	if len(m.models) == 0 {
		m.models = m.seedModels(msg.id)
	}
	m.cursor = 0
	switch m.screen {
	case screenModels:
		m.resetListScroll()
		m.initChecked()
	case screenAddModels:
		m.resetListScroll()
	}
	return m
}

// seedModels supplies a model list when discovery returns nothing (or errors):
// the curated activeModels, then the configured default, then the preset
// DefaultModel for a template-seeded add (needed for non-/v1 endpoints like
// z.ai whose /models discovery is unavailable).
func (m Model) seedModels(id string) []string {
	if curated := m.deps.ActiveModels(id); len(curated) > 0 {
		out := make([]string, len(curated))
		copy(out, curated)
		return out
	}
	if name := currentValue(m.deps.ProviderSettings(id), "model"); name != "" {
		return []string{name}
	}
	if m.add.presetID != "" {
		if p, ok := config.LookupProviderPreset(m.add.presetID); ok && p.DefaultModel != "" {
			return []string{p.DefaultModel}
		}
	}
	return nil
}

// initChecked seeds the activation checkboxes from the curated set. When the
// provider has not yet been curated (no saved activeModels), only the provider's
// configured default model is checked so the user opts in explicitly via
// Space / A before saving — prevents accidental mass-activation of large catalogs.
func (m *Model) initChecked() {
	curated := m.deps.ActiveModels(m.targetID)
	m.checked = make([]bool, len(m.models))
	if len(curated) > 0 {
		set := make(map[string]bool, len(curated))
		for _, c := range curated {
			set[c] = true
		}
		for i, mod := range m.models {
			m.checked[i] = set[mod]
		}
		return
	}
	// Uncurated: check only the configured default (or live) model.
	def := currentValue(m.deps.ProviderSettings(m.targetID), "model")
	if def == "" {
		def = m.deps.CurrentModel(m.targetID)
	}
	for i, mod := range m.models {
		m.checked[i] = def != "" && mod == def
	}
}

func (m Model) handleKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	// Text-entry screens consume keys for the input buffer.
	switch m.screen {
	case screenEditField, screenSetKey, screenModelsAdd:
		return m.handleTextEntryKey(msg)
	case screenAdd:
		return m.handleAddKey(msg)
	case screenRemove:
		return m.handleRemoveKey(msg)
	}

	switch msg.String() {
	case "esc":
		return m.back()
	case "up", "k":
		if m.listLen() > 0 && m.screenUsesListScroll() {
			m.moveListCursor(-1)
		} else if m.listLen() > 0 {
			m.cursor = wrapDec(m.cursor, m.listLen())
		}
		return m, nil
	case "down", "j":
		if m.listLen() > 0 && m.screenUsesListScroll() {
			m.moveListCursor(1)
		} else if m.listLen() > 0 {
			m.cursor = wrapInc(m.cursor, m.listLen())
		}
		return m, nil
	case " ":
		if m.screen == screenModels {
			m.toggleChecked()
			return m, nil
		}
	case "A", "*":
		if m.screen == screenModels && !m.loading && len(m.models) > 0 {
			m.toggleAllChecked()
			return m, nil
		}
	case "a":
		if m.screen == screenEditPick {
			return m.enterAddTemplate(), nil
		}
		if (m.screen == screenModels || m.screen == screenAddModels) && !m.loading {
			return m.enterModelsAdd(), nil
		}
	case "x":
		if m.screen == screenEditPick {
			return m.confirmRemove()
		}
	case "r":
		if m.screen == screenEdit {
			return m.resetHighlightedField()
		}
	case "enter":
		return m.selectCurrent()
	}
	return m, nil
}

// resetHighlightedField clears the override under the cursor on the edit sheet.
func (m Model) resetHighlightedField() (Model, tea.Cmd) {
	if m.cursor < 0 || m.cursor >= len(m.editItems) {
		return m, nil
	}
	item := m.editItems[m.cursor]
	m.errMsg = ""
	m.info = ""
	switch item.kind {
	case editPreset:
		_ = m.deps.ClearSetting(m.ctx, m.targetID, "promptMode")
		if err := m.deps.ClearSetting(m.ctx, m.targetID, "personality"); err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		m.info = "System prompt reset to default."
	case editOverride, editEnum, editToggleBool:
		if err := m.deps.ClearSetting(m.ctx, m.targetID, item.key); err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		m.info = item.key + " reset to default."
	default:
		return m, nil
	}
	m.refreshEditItems()
	return m, nil
}

// toggleChecked flips the activation checkbox under the cursor.
func (m *Model) toggleChecked() {
	if m.cursor < 0 || m.cursor >= len(m.checked) {
		return
	}
	m.checked[m.cursor] = !m.checked[m.cursor]
}

// back navigates one screen toward the provider list, or closes from the root.
func (m Model) back() (Model, tea.Cmd) {
	m.errMsg = ""
	m.info = ""
	switch m.screen {
	case screenEditPick:
		m.done = true
		return m, nil
	case screenRemove:
		m.removeTarget = ""
		m.screen = screenEditPick
		return m, nil
	case screenEdit:
		m.providers = m.deps.ListProviders()
		m.screen = screenEditPick
		m.cursor = 0
		m.listOffset = 0
	case screenEditField, screenEditPicker:
		m.screen = screenEdit
	case screenAddTemplate:
		m.screen = screenEditPick
		m.cursor = 0
		m.listOffset = 0
	case screenAdd:
		// A template-seeded add returns to the template picker; a blank add
		// returns to the provider list.
		if m.add.presetID != "" {
			return m.enterAddTemplate(), nil
		}
		m.providers = m.deps.ListProviders()
		m.screen = screenEditPick
		m.cursor = 0
		m.listOffset = 0
	case screenModelsAdd:
		m.screen = screenModels
		m.syncInputWidth()
	case screenModels:
		if m.modelsFrom == screenEdit {
			return m.returnToEdit(), nil
		}
		m.providers = m.deps.ListProviders()
		m.screen = screenEditPick
		m.cursor = 0
	case screenAddModels:
		// Provider already added; return to provider list.
		m.providers = m.deps.ListProviders()
		m.screen = screenEditPick
		m.cursor = 0
	default:
		m.providers = m.deps.ListProviders()
		m.screen = screenEditPick
		m.cursor = 0
	}
	return m, nil
}

// returnToEdit rebuilds and shows the edit sheet for the current target provider.
// Used to return from the activation screen when it was opened from the edit sheet.
func (m Model) returnToEdit() Model {
	m.providers = m.deps.ListProviders()
	if p, ok := m.findProvider(m.targetID); ok {
		m.editItems = m.buildEditItems(p)
	}
	m.screen = screenEdit
	m.cursor = 0
	m.listOffset = 0
	return m
}

// ---- list lengths --------------------------------------------------------

func (m Model) listLen() int {
	switch m.screen {
	case screenEditPick:
		return len(m.providers)
	case screenEdit:
		return len(m.editItems)
	case screenEditPicker, screenAddTemplate:
		return len(m.pickerOptions)
	case screenRemove:
		return 0 // confirmation dialog; no scrollable list
	case screenAddModels, screenModels:
		if m.loading {
			return 0
		}
		return len(m.models)
	}
	return 0
}

func (m Model) customProviders() []ProviderEntry {
	out := make([]ProviderEntry, 0, len(m.providers))
	for _, p := range m.providers {
		if p.IsCustom {
			out = append(out, p)
		}
	}
	return out
}

// ---- selection dispatch --------------------------------------------------

func (m Model) selectCurrent() (Model, tea.Cmd) {
	switch m.screen {
	case screenEditPick:
		return m.selectEditPick()
	case screenEdit:
		return m.selectEdit()
	case screenEditPicker:
		return m.selectPicker()
	case screenAddTemplate:
		return m.selectTemplate()
	case screenAddModels:
		return m.selectAddModel()
	case screenModels:
		return m.saveActivation()
	}
	return m, nil
}

// saveActivation persists the checked subset as the provider's active models.
func (m Model) saveActivation() (Model, tea.Cmd) {
	if m.loading || len(m.models) == 0 {
		return m, nil
	}
	selected := make([]string, 0, len(m.models))
	for i, mod := range m.models {
		if i < len(m.checked) && m.checked[i] {
			selected = append(selected, mod)
		}
	}
	if len(selected) == 0 {
		m.errMsg = "Select at least one model (Space to toggle) before saving."
		return m, nil
	}
	if err := m.deps.SetActiveModels(m.ctx, m.targetID, selected); err != nil {
		m.errMsg = err.Error()
		return m, nil
	}
	m.status = fmt.Sprintf("Saved %d active model(s) for %s.", len(selected), config.ProviderDisplayID(m.targetID))

	// Keep the live model inside the curated set: when the active provider's
	// current model was just deactivated, switch it to the first checked model so
	// /models and the runner never point at an unchecked model.
	if m.targetID == m.deps.ActiveProviderID() {
		current := m.deps.CurrentModel(m.targetID)
		if current != "" && !containsString(selected, current) {
			if err := m.deps.SetModel(m.ctx, m.targetID, selected[0]); err != nil {
				m.errMsg = err.Error()
				return m, nil
			}
			m.status = fmt.Sprintf("Saved %d active model(s) for %s. Live model -> %s (was unchecked).",
				len(selected), config.ProviderDisplayID(m.targetID), selected[0])
		}
	}

	m.info = m.status
	if m.modelsFrom == screenEdit {
		return m.returnToEdit(), nil
	}
	m.providers = m.deps.ListProviders()
	m.screen = screenEditPick
	m.cursor = 0
	return m, nil
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

// ---- edit ----------------------------------------------------------------

func (m Model) selectEditPick() (Model, tea.Cmd) {
	if m.cursor < 0 || m.cursor >= len(m.providers) {
		return m, nil
	}
	p := m.providers[m.cursor]
	m.targetID = p.ID
	m.targetIsCustom = p.IsCustom
	m.targetWire = p.WireFormat
	m.editItems = m.buildEditItems(p)
	m.screen = screenEdit
	m.cursor = 0
	m.listOffset = 0
	return m, nil
}

// confirmRemove navigates to the delete confirmation screen for the highlighted
// custom provider. Built-ins are rejected with an error.
func (m Model) confirmRemove() (Model, tea.Cmd) {
	if m.cursor < 0 || m.cursor >= len(m.providers) {
		return m, nil
	}
	p := m.providers[m.cursor]
	if !p.IsCustom {
		m.errMsg = "Only custom providers can be removed. Built-ins cannot be deleted."
		return m, nil
	}
	m.removeTarget = p.ID
	m.errMsg = ""
	m.info = ""
	m.screen = screenRemove
	return m, nil
}

// handleRemoveKey handles all key input on the delete confirmation screen.
func (m Model) handleRemoveKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return m.back()
	case "y", "enter":
		return m.doRemove()
	}
	return m, nil
}

// doRemove performs the confirmed provider removal off the Update goroutine so
// the credential/disk delete never blocks the UI loop; a spinner animates while
// it runs.
func (m Model) doRemove() (Model, tea.Cmd) {
	if m.removeTarget == "" {
		return m.back()
	}
	name := m.removeTarget
	for _, p := range m.providers {
		if p.ID == m.removeTarget {
			name = p.DisplayName
			break
		}
	}
	id := m.removeTarget
	ctx := m.ctx
	deps := m.deps
	m.saving = true
	m.errMsg = ""
	m.info = fmt.Sprintf("Removing %s…", name)
	return m, tea.Batch(
		m.spin.Tick,
		func() tea.Msg {
			err := deps.RemoveCustomProvider(ctx, id)
			return removeResultMsg{name: name, err: err}
		},
	)
}

// handleRemoveResult finishes the delete: on success it returns to the provider
// list with the target gone; on failure it surfaces the error on the
// confirmation screen so the user can Esc out.
func (m Model) handleRemoveResult(msg removeResultMsg) (Model, tea.Cmd) {
	m.saving = false
	if msg.err != nil {
		m.errMsg = msg.err.Error()
		m.info = ""
		return m, nil
	}
	m.removeTarget = ""
	m.providers = m.deps.ListProviders()
	m.info = fmt.Sprintf("Removed %s.", msg.name)
	m.status = m.info
	if m.cursor >= len(m.providers) && m.cursor > 0 {
		m.cursor = len(m.providers) - 1
	}
	m.screen = screenEditPick
	return m, nil
}

func (m Model) buildEditItems(p ProviderEntry) []editItem {
	items := []editItem{{label: "Set API key", kind: editAPIKey}}
	items = append(items, editItem{label: "Manage models (activate/deactivate)", kind: editModel, key: "model"})

	if p.IsCustom {
		items = append(items,
			editItem{label: "Provider name", kind: editDefn, key: "displayName"},
			editItem{label: "URL / host", kind: editDefn, key: "hostOrURL"},
			editItem{label: "Port", kind: editDefn, key: "port"},
			editItem{label: "Wire format", kind: editWireDefn, key: "wireFormat"},
			editItem{label: "API key env var", kind: editDefn, key: "apiKeyEnvVar"},
		)
	}

	keys := m.deps.ValidSettingKeys(p.ID)
	// System prompt is a single picker that replaces the personality + promptMode
	// rows; only offer it when the provider exposes the personality knob.
	if containsString(keys, "personality") {
		items = append(items, editItem{label: "System prompt", kind: editPreset, key: "systemPrompt"})
	}
	for _, key := range keys {
		switch key {
		case "model":
			continue // handled by the model activation row above
		case "personality", "promptMode":
			continue // collapsed into the System prompt picker
		}
		if p.IsCustom && key == "baseUrl" {
			continue // edited via the definition row above
		}
		switch key {
		case "enableTools", "showThinking":
			items = append(items, editItem{label: key, kind: editToggleBool, key: key})
		case "toolCallParsing":
			items = append(items, editItem{label: key, kind: editEnum, key: key})
		default:
			items = append(items, editItem{label: key, kind: editOverride, key: key})
		}
	}
	items = append(items, editItem{label: "Reset all settings to defaults", kind: editResetAll})
	items = append(items, editItem{label: "Back", kind: editBack})
	return items
}

func (m Model) selectEdit() (Model, tea.Cmd) {
	if m.cursor < 0 || m.cursor >= len(m.editItems) {
		return m, nil
	}
	item := m.editItems[m.cursor]
	m.errMsg = ""
	m.info = ""
	switch item.kind {
	case editBack:
		m.screen = screenEditPick
		m.cursor = 0
	case editAPIKey:
		return m.enterSetKey(), nil
	case editModel:
		// Open the activation screen for this provider: discover its models and
		// let the user activate/deactivate the curated set. The live model is then
		// chosen from that set via /models (and coerced when deactivated).
		return m.enterModels(m.targetID)
	case editWireDefn:
		// Toggle and apply immediately.
		next := config.WireFormatOpenAIChat
		if m.targetWire == config.WireFormatOpenAIChat {
			next = config.WireFormatOpenAIResponses
		}
		if err := m.deps.UpdateCustomDefinition(m.ctx, m.targetID, "wireFormat", string(next)); err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		m.targetWire = next
		m.providers = m.deps.ListProviders()
		for _, p := range m.providers {
			if p.ID == m.targetID {
				m.editItems = m.buildEditItems(p)
				break
			}
		}
		m.info = "wireFormat → " + string(next)
		return m, nil
	case editToggleBool:
		// Toggle enableTools in place against the effective current value.
		next := "false"
		if currentBool(m.deps.EffectiveProviderSettings(m.targetID), item.key, true) {
			next = "false"
		} else {
			next = "true"
		}
		if err := m.deps.ApplySetting(m.ctx, m.targetID, item.key, next); err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		m.refreshEditItems()
		m.info = fmt.Sprintf("%s → %s", item.key, next)
		return m, nil
	case editPreset:
		return m.enterPreset(), nil
	case editEnum:
		return m.enterEnum(item.key), nil
	case editResetAll:
		if err := m.deps.ResetSettings(m.ctx, m.targetID); err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		m.refreshEditItems()
		m.info = "Provider settings reset to defaults (model and API key kept)."
		return m, nil
	case editDefn, editOverride:
		m.editingKey = item.key
		m.editingKind = item.kind
		m.input = freshInput(item.key + " value")
		m.input.SetValue(currentValue(m.deps.ProviderSettings(m.targetID), item.key))
		m.screen = screenEditField
	}
	return m, nil
}

// refreshEditItems re-reads providers and rebuilds the edit sheet rows in place.
func (m *Model) refreshEditItems() {
	m.providers = m.deps.ListProviders()
	if p, ok := m.findProvider(m.targetID); ok {
		m.editItems = m.buildEditItems(p)
	}
}

// enterPreset opens the system-prompt preset picker.
func (m Model) enterPreset() Model {
	presets := config.SortedSystemPromptPresets()
	opts := make([]pickerOption, 0, len(presets))
	current := m.deps.SystemPromptPresetID(m.targetID)
	m.cursor = 0
	for i, p := range presets {
		opts = append(opts, pickerOption{id: p.ID, label: p.Label})
		if p.ID == current {
			m.cursor = i
		}
	}
	m.pickerKey = "systemPrompt"
	m.pickerTitle = "System prompt for " + config.ProviderDisplayID(m.targetID)
	m.pickerOptions = opts
	m.screen = screenEditPicker
	m.listOffset = 0
	m.ensureListVisible()
	return m
}

// enterEnum opens a fixed-choice picker for a setting key (e.g. toolCallParsing).
func (m Model) enterEnum(key string) Model {
	var opts []pickerOption
	switch key {
	case "toolCallParsing":
		opts = []pickerOption{
			{id: string(config.ToolCallParsingStrict), label: "strict"},
			{id: string(config.ToolCallParsingLenient), label: "lenient"},
			{id: string(config.ToolCallParsingLoose), label: "loose"},
		}
	}
	current := currentValue(m.deps.EffectiveProviderSettings(m.targetID), key)
	m.cursor = 0
	for i, o := range opts {
		if o.id == current {
			m.cursor = i
		}
	}
	m.pickerKey = key
	m.pickerTitle = key + " for " + config.ProviderDisplayID(m.targetID)
	m.pickerOptions = opts
	m.screen = screenEditPicker
	m.listOffset = 0
	m.ensureListVisible()
	return m
}

// selectPicker applies the highlighted picker choice and returns to the edit sheet.
func (m Model) selectPicker() (Model, tea.Cmd) {
	if m.cursor < 0 || m.cursor >= len(m.pickerOptions) {
		return m, nil
	}
	choice := m.pickerOptions[m.cursor]
	if m.pickerKey == "systemPrompt" {
		info, err := m.deps.ApplySystemPromptPreset(m.ctx, m.targetID, choice.id)
		if err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		m.info = info
	} else {
		if err := m.deps.ApplySetting(m.ctx, m.targetID, m.pickerKey, choice.id); err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		m.info = fmt.Sprintf("%s → %s", m.pickerKey, choice.label)
	}
	m.refreshEditItems()
	m.screen = screenEdit
	return m, nil
}

func (m Model) enterSetKey() Model {
	m.input = freshSecretInput()
	m.screen = screenSetKey
	m.syncInputWidth()
	return m
}

func (m Model) enterModels(id string) (Model, tea.Cmd) {
	switch m.screen {
	case screenEdit:
		m.modelsFrom = screenEdit
	case screenAddModels:
		// keep existing modelsFrom (set by caller)
	default:
		m.modelsFrom = screenEditPick
	}
	m.targetID = id
	m.models = nil
	m.checked = nil
	m.resetListScroll()
	if m.screen != screenAddModels {
		m.screen = screenModels
	}
	m.loading = true
	m.modelsErr = ""
	return m, discoverCmd(m.ctx, m.deps, id)
}

func (m Model) enterModelsAdd() Model {
	m.modelsAddReturn = m.screen
	m.input = freshInput("model name (e.g. gemini-2.5-pro)")
	m.screen = screenModelsAdd
	m.syncInputWidth()
	return m
}

func (m Model) findProvider(id string) (ProviderEntry, bool) {
	for _, p := range m.providers {
		if p.ID == id {
			return p, true
		}
	}
	return ProviderEntry{}, false
}

// ---- text entry (edit field + set key) -----------------------------------

func (m Model) handleTextEntryKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		switch m.screen {
		case screenSetKey:
			m.screen = screenEdit
		case screenModelsAdd:
			m.screen = screenModels
		default:
			m.screen = screenEdit
		}
		m.input.Blur()
		m.errMsg = ""
		return m, nil
	case "enter":
		return m.commitTextEntry()
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) commitTextEntry() (Model, tea.Cmd) {
	value := strings.TrimSpace(m.input.Value())
	if m.screen == screenSetKey {
		if value == "" {
			m.errMsg = "API key cannot be empty."
			return m, nil
		}
		if err := m.deps.SetAPIKey(m.ctx, m.targetID, value); err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		m.status = fmt.Sprintf("API key saved for %s.", config.ProviderDisplayID(m.targetID))
		m.info = m.status
		m.screen = screenEdit
		m.cursor = 0
		return m, nil
	}

	// edit field / add model name
	if m.screen == screenModelsAdd {
		return m.commitModelsAdd(value)
	}

	var err error
	switch m.editingKind {
	case editModel:
		err = m.deps.SetModel(m.ctx, m.targetID, value)
	case editDefn:
		err = m.deps.UpdateCustomDefinition(m.ctx, m.targetID, m.editingKey, value)
	default:
		err = m.deps.ApplySetting(m.ctx, m.targetID, m.editingKey, value)
	}
	if err != nil {
		m.errMsg = err.Error()
		return m, nil
	}
	m.info = fmt.Sprintf("%s → %s", m.editingKey, value)
	m.providers = m.deps.ListProviders()
	m.screen = screenEdit
	return m, nil
}

// ---- add: template picker ------------------------------------------------

// enterAddTemplate shows the preset-or-blank picker that precedes the add
// wizard. Selecting a preset seeds the add flow; selecting Blank starts the
// field-by-field flow.
func (m Model) enterAddTemplate() Model {
	opts := make([]pickerOption, 0, len(config.ProviderPresets)+1)
	for _, p := range config.ProviderPresets {
		opts = append(opts, pickerOption{id: p.ID, label: p.DisplayName})
	}
	opts = append(opts, pickerOption{id: "", label: "Custom (blank) — enter a URL manually"})
	m.pickerOptions = opts
	m.cursor = 0
	m.listOffset = 0
	m.errMsg = ""
	m.info = ""
	m.screen = screenAddTemplate
	return m
}

// selectTemplate seeds the add flow from the highlighted preset (jumping to the
// API-key step with URL/wire/env pre-filled) or, for the Blank entry, starts the
// field-by-field flow.
func (m Model) selectTemplate() (Model, tea.Cmd) {
	if m.cursor < 0 || m.cursor >= len(m.pickerOptions) {
		return m, nil
	}
	choice := m.pickerOptions[m.cursor]
	m.errMsg = ""
	m.info = ""
	if choice.id == "" {
		m.add = addState{wire: config.WireFormatOpenAIChat, fieldIdx: addFieldName}
		m.input = freshInput("display name (e.g. Local vLLM)")
		m.screen = screenAdd
		return m, nil
	}
	p, ok := config.LookupProviderPreset(choice.id)
	if !ok {
		m.errMsg = "Unknown template."
		return m, nil
	}
	m.add = addState{
		displayName: p.DisplayName,
		hostOrURL:   p.BaseURL,
		wire:        p.WireFormat,
		envVar:      p.APIKeyEnvVar,
		presetID:    p.ID,
		note:        p.Note,
		fieldIdx:    addFieldAPIKey,
	}
	m.input = freshSecretInput()
	m.screen = screenAdd
	return m, nil
}

// claimProviderID returns preferred, or preferred-N when it collides with an
// existing provider id, using the dialog's current provider list (no network).
func (m Model) claimProviderID(preferred string) string {
	taken := make(map[string]bool, len(m.providers))
	for _, p := range m.providers {
		taken[p.ID] = true
	}
	if !taken[preferred] {
		return preferred
	}
	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s-%d", preferred, i)
		if !taken[candidate] {
			return candidate
		}
	}
}

// ---- add -----------------------------------------------------------------

func (m Model) handleAddKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if m.add.fieldIdx == addFieldWire {
		switch msg.String() {
		case "esc":
			return m.back()
		case "left", "right", " ":
			if m.add.wire == config.WireFormatOpenAIChat {
				m.add.wire = config.WireFormatOpenAIResponses
			} else {
				m.add.wire = config.WireFormatOpenAIChat
			}
			return m, nil
		case "enter":
			m.add.fieldIdx = addFieldEnvVar
			m.input = freshInput("API key env var (optional, e.g. OPENAI_API_KEY)")
			return m, nil
		}
		return m, nil
	}

	switch msg.String() {
	case "esc":
		return m.back()
	case "enter":
		return m.advanceAdd()
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) advanceAdd() (Model, tea.Cmd) {
	value := strings.TrimSpace(m.input.Value())
	switch m.add.fieldIdx {
	case addFieldName:
		if value == "" {
			m.errMsg = "Provider name is required."
			return m, nil
		}
		m.add.displayName = value
		m.add.fieldIdx = addFieldHostOrURL
		m.input = freshInput("URL or host (e.g. http://127.0.0.1:8000 or 127.0.0.1)")
	case addFieldHostOrURL:
		if value == "" {
			m.errMsg = "URL or hostname is required."
			return m, nil
		}
		m.add.hostOrURL = value
		if urlHasPort(value) {
			// Port already in URL; skip port step.
			m.add.fieldIdx = addFieldWire
			m.input.Blur()
		} else {
			m.add.fieldIdx = addFieldPort
			m.input = freshInput("port (default: 8000)")
		}
	case addFieldPort:
		m.add.port = value
		m.add.fieldIdx = addFieldWire
		m.input.Blur()
	case addFieldEnvVar:
		m.add.envVar = value
		m.add.fieldIdx = addFieldAPIKey
		m.input = freshSecretInput()
	case addFieldAPIKey:
		m.add.apiKey = value
		// Show auto-generated id for optional override.
		baseURL, err := composeAddURL(m.add.hostOrURL, m.add.port)
		if err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		// A template-seeded add defaults its id to the preset id (collision
		// de-duplicated); a blank add derives one from the URL host.
		suggested := ""
		if m.add.presetID != "" {
			suggested = m.claimProviderID(m.add.presetID)
		} else {
			suggested = m.deps.GenerateProviderID(baseURL)
		}
		m.add.idOverride = suggested
		m.add.fieldIdx = addFieldIdOverride
		ti := freshInput("provider id (edit or Enter to accept)")
		ti.SetValue(suggested)
		m.input = ti
	case addFieldIdOverride:
		if value == "" {
			m.errMsg = "Provider id cannot be empty."
			return m, nil
		}
		if _, ok := config.LookupBuiltInProvider(value); ok {
			m.errMsg = fmt.Sprintf("%q conflicts with a built-in provider id.", value)
			return m, nil
		}
		m.add.idOverride = value
		return m.submitAdd()
	}
	m.errMsg = ""
	return m, nil
}

func (m Model) submitAdd() (Model, tea.Cmd) {
	baseURL, err := composeAddURL(m.add.hostOrURL, m.add.port)
	if err != nil {
		m.errMsg = err.Error()
		return m, nil
	}
	def := config.CustomProviderDefinition{
		DisplayName:  m.add.displayName,
		BaseURL:      baseURL,
		APIKeyEnvVar: m.add.envVar,
		WireFormat:   m.add.wire,
	}
	id := m.add.idOverride
	apiKey := m.add.apiKey
	ctx := m.ctx
	deps := m.deps
	m.saving = true
	m.errMsg = ""
	m.info = fmt.Sprintf("Adding %s…", m.add.displayName)
	m.input.Blur()
	return m, tea.Batch(
		m.spin.Tick,
		func() tea.Msg {
			err := deps.AddCustomProvider(ctx, id, def, apiKey)
			return addResultMsg{id: id, err: err}
		},
	)
}

// handleAddResult resumes the add flow once the off-Update write completes:
// on success it discovers the new provider's models; on failure it surfaces the
// error and re-focuses the id field so the user can correct and retry.
func (m Model) handleAddResult(msg addResultMsg) (Model, tea.Cmd) {
	m.saving = false
	if msg.err != nil {
		m.errMsg = msg.err.Error()
		m.info = ""
		return m, m.input.Focus()
	}
	m.providers = m.deps.ListProviders()
	m.targetID = msg.id
	m.loading = true
	m.models = nil
	m.modelsErr = ""
	m.cursor = 0
	m.screen = screenAddModels
	return m, discoverCmd(m.ctx, m.deps, msg.id)
}

// urlHasPort reports whether hostOrURL (full URL or bare host) already
// specifies a port so the wizard can skip the port step. A bare "host:port"
// (no scheme) is parsed by assuming http:// so e.g. "127.0.0.1:8000" is
// recognized as already carrying a port.
func urlHasPort(hostOrURL string) bool {
	candidate := strings.TrimSpace(hostOrURL)
	if candidate == "" {
		return false
	}
	if !strings.Contains(candidate, "://") {
		candidate = "http://" + candidate
	}
	u, err := url.Parse(candidate)
	if err != nil {
		return false
	}
	return u.Port() != ""
}

// composeAddURL builds the canonical base URL from wizard hostOrURL + port
// fields using stdlib url so the dialog does not import the provider package.
// A bare host (no scheme) is treated as http://; a bare "host:port" keeps its
// embedded port rather than appending the default, avoiding malformed URLs
// like "http://127.0.0.1:8000:8000".
func composeAddURL(hostOrURL, port string) (string, error) {
	hostOrURL = strings.TrimSpace(hostOrURL)
	port = strings.TrimSpace(port)
	if hostOrURL == "" {
		return "", fmt.Errorf("URL or hostname is required")
	}
	hadScheme := strings.Contains(hostOrURL, "://")
	candidate := hostOrURL
	if !hadScheme {
		candidate = "http://" + hostOrURL
	}
	u, err := url.Parse(candidate)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("URL scheme must be http or https")
	}
	if u.Host == "" {
		return "", fmt.Errorf("URL must include a host")
	}
	if u.Port() == "" {
		p := port
		// A bare host with no explicit port defaults to 8000 (local vLLM);
		// a full URL without a port is left as-is unless the user gave one.
		if p == "" && !hadScheme {
			p = "8000"
		}
		if p != "" {
			u.Host = u.Hostname() + ":" + p
		}
	}
	return u.String(), nil
}

func (m Model) selectAddModel() (Model, tea.Cmd) {
	if m.cursor < 0 || m.cursor >= len(m.models) {
		return m, nil
	}
	model := m.models[m.cursor]
	if err := m.deps.SetModel(m.ctx, m.targetID, model); err != nil {
		m.errMsg = err.Error()
		return m, nil
	}
	if err := m.deps.SwitchProvider(m.ctx, m.targetID); err != nil {
		m.errMsg = err.Error()
		return m, nil
	}
	m.status = fmt.Sprintf("Added %s with model %s.", config.ProviderDisplayID(m.targetID), model)
	m.info = m.status
	m.providers = m.deps.ListProviders()
	m.screen = screenEditPick
	m.cursor = 0
	return m, nil
}

// ---- helpers -------------------------------------------------------------

func (m Model) commitModelsAdd(name string) (Model, tea.Cmd) {
	if name == "" {
		m.errMsg = "Model name cannot be empty."
		return m, nil
	}
	for _, existing := range m.models {
		if existing == name {
			m.errMsg = fmt.Sprintf("Model %q is already in the list.", name)
			return m, nil
		}
	}
	m.models = append(m.models, name)
	m.cursor = len(m.models) - 1
	m.modelsErr = ""
	// The add flow (screenAddModels) picks a single default; the activation
	// screen (screenModels) toggles a curated set.
	if m.modelsAddReturn == screenAddModels {
		m.screen = screenAddModels
		m.info = fmt.Sprintf("Added %q — Enter to use it.", name)
	} else {
		m.checked = append(m.checked, true)
		m.screen = screenModels
		m.info = fmt.Sprintf("Added %q — Space toggles, Enter saves.", name)
	}
	m.ensureListVisible()
	m.syncInputWidth()
	return m, nil
}

func discoverCmd(ctx context.Context, deps Deps, id string) tea.Cmd {
	return func() tea.Msg {
		models, err := deps.DiscoverModels(ctx, id)
		return modelsLoadedMsg{id: id, models: models, err: err}
	}
}

func freshInput(placeholder string) textinput.Model {
	ti := textinput.New()
	ti.CharLimit = 8192
	ti.Prompt = "> "
	ti.Placeholder = placeholder
	ti.EchoMode = textinput.EchoNormal
	ti.SetValue("")
	ti.Focus()
	return ti
}

// freshSecretInput returns a blank password field. Placeholders are omitted
// because EchoPassword mode can leak the first placeholder character as "p".
func freshSecretInput() textinput.Model {
	ti := freshInput("")
	ti.Placeholder = ""
	ti.EchoMode = textinput.EchoPassword
	return ti
}

func currentValue(settings map[string]string, key string) string {
	if settings == nil {
		return ""
	}
	return settings[key]
}

func currentBool(settings map[string]string, key string, def bool) bool {
	v, ok := settings[key]
	if !ok {
		return def
	}
	return v == "true"
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
