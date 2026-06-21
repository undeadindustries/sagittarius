package providersdialog

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/undeadindustries/sagittarius/internal/config"
)

type screen int

const (
	screenMenu screen = iota
	screenSwitch
	screenEditPick
	screenEdit
	screenEditField
	screenSetKey
	screenAdd
	screenAddModels
	screenRemove
	screenModels
)

// editKind classifies an editable row on the edit sheet.
type editKind int

const (
	editAPIKey   editKind = iota // opens Set API key entry
	editModel                    // opens model picker (or text for gemini)
	editOverride                 // providers.<id>.<key> instance override
	editDefn                     // custom definition field
	editWireDefn                 // custom definition wireFormat toggle
	editBack
)

type editItem struct {
	label string
	kind  editKind
	key   string // setting/definition key for override/defn kinds
}

// addState holds the multi-field add-provider wizard buffers.
type addState struct {
	fieldIdx    int
	id          string
	displayName string
	baseURL     string
	envVar      string
	apiKey      string
	wire        config.WireFormat
}

const (
	addFieldID = iota
	addFieldName
	addFieldBaseURL
	addFieldWire
	addFieldEnvVar
	addFieldAPIKey
	addFieldCount
)

// modelsLoadedMsg is delivered when an async DiscoverModels call completes.
type modelsLoadedMsg struct {
	id     string
	models []string
	err    error
}

// Model is the providers wizard overlay. It is driven by the parent Bubble Tea
// model: the parent forwards messages to Update while the dialog is active and
// renders View; when Done reports true the parent removes the overlay.
type Model struct {
	deps Deps
	ctx  context.Context

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

	input textinput.Model

	add addState

	loading   bool
	models    []string
	modelsErr string
}

// New constructs the wizard at the menu screen with the current provider list.
func New(ctx context.Context, deps Deps) Model {
	ti := textinput.New()
	ti.CharLimit = 8192
	ti.Prompt = "> "

	m := Model{
		deps:      deps,
		ctx:       ctx,
		screen:    screenMenu,
		input:     ti,
		add:       addState{wire: config.WireFormatOpenAIChat},
		providers: deps.ListProviders(),
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

// Update advances the dialog state machine for one message.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case modelsLoadedMsg:
		return m.handleModelsLoaded(msg), nil
	case tea.KeyMsg:
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
		m.models = nil
		return m
	}
	m.modelsErr = ""
	m.models = msg.models
	m.cursor = 0
	return m
}

func (m Model) handleKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	// Text-entry screens consume keys for the input buffer.
	switch m.screen {
	case screenEditField, screenSetKey:
		return m.handleTextEntryKey(msg)
	case screenAdd:
		return m.handleAddKey(msg)
	}

	switch msg.String() {
	case "esc":
		return m.back()
	case "up", "k":
		m.cursor = wrapDec(m.cursor, m.listLen())
		return m, nil
	case "down", "j":
		m.cursor = wrapInc(m.cursor, m.listLen())
		return m, nil
	case "enter":
		return m.selectCurrent()
	}
	return m, nil
}

// back navigates one screen toward the menu, or closes from the menu.
func (m Model) back() (Model, tea.Cmd) {
	m.errMsg = ""
	m.info = ""
	switch m.screen {
	case screenMenu:
		m.done = true
		return m, nil
	case screenEdit:
		m.screen = screenEditPick
		m.cursor = 0
	case screenEditField:
		m.screen = screenEdit
	case screenAddModels:
		// Provider already added; just return to menu.
		m.providers = m.deps.ListProviders()
		m.screen = screenMenu
		m.cursor = 0
	default:
		m.screen = screenMenu
		m.cursor = 0
	}
	return m, nil
}

// ---- list lengths --------------------------------------------------------

func (m Model) listLen() int {
	switch m.screen {
	case screenMenu:
		return len(m.menuItems())
	case screenSwitch, screenEditPick:
		return len(m.providers)
	case screenEdit:
		return len(m.editItems)
	case screenRemove:
		return len(m.customProviders())
	case screenAddModels, screenModels:
		if m.loading || m.modelsErr != "" {
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
	case screenMenu:
		return m.selectMenu()
	case screenSwitch:
		return m.selectSwitch()
	case screenEditPick:
		return m.selectEditPick()
	case screenEdit:
		return m.selectEdit()
	case screenRemove:
		return m.selectRemove()
	case screenAddModels:
		return m.selectAddModel()
	case screenModels:
		return m, nil // browse only
	}
	return m, nil
}

// ---- menu ----------------------------------------------------------------

type menuItem struct {
	label string
	id    string
}

func (m Model) menuItems() []menuItem {
	return []menuItem{
		{"Switch active provider", "switch"},
		{"Edit a provider", "edit"},
		{"Set API key", "setkey"},
		{"Add provider", "add"},
		{"Remove provider", "remove"},
		{"Browse models", "models"},
		{"Close", "close"},
	}
}

func (m Model) selectMenu() (Model, tea.Cmd) {
	items := m.menuItems()
	if m.cursor < 0 || m.cursor >= len(items) {
		return m, nil
	}
	m.errMsg = ""
	m.info = ""
	switch items[m.cursor].id {
	case "switch":
		m.screen = screenSwitch
		m.cursor = 0
	case "edit":
		m.screen = screenEditPick
		m.cursor = 0
	case "setkey":
		m.targetID = m.deps.ActiveProviderID()
		if m.targetID == "" {
			m.errMsg = "No active provider. Switch to one first."
			return m, nil
		}
		return m.enterSetKey(), nil
	case "add":
		m.add = addState{wire: config.WireFormatOpenAIChat}
		m.input = freshInput("provider id (e.g. local-vllm)")
		m.screen = screenAdd
	case "remove":
		m.screen = screenRemove
		m.cursor = 0
	case "models":
		id := m.deps.ActiveProviderID()
		if id == "" {
			m.errMsg = "No active provider to browse models for."
			return m, nil
		}
		return m.enterModels(id)
	case "close":
		m.done = true
	}
	return m, nil
}

// ---- switch --------------------------------------------------------------

func (m Model) selectSwitch() (Model, tea.Cmd) {
	if m.cursor < 0 || m.cursor >= len(m.providers) {
		return m, nil
	}
	id := m.providers[m.cursor].ID
	if err := m.deps.SwitchProvider(m.ctx, id); err != nil {
		m.errMsg = err.Error()
		return m, nil
	}
	m.status = fmt.Sprintf("Active provider → %s.", config.ProviderDisplayID(id))
	m.providers = m.deps.ListProviders()
	m.screen = screenMenu
	m.cursor = 0
	m.info = m.status
	return m, nil
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
	return m, nil
}

func (m Model) buildEditItems(p ProviderEntry) []editItem {
	items := []editItem{{label: "Set API key", kind: editAPIKey}}
	items = append(items, editItem{label: "model", kind: editModel, key: "model"})

	if p.IsCustom {
		items = append(items,
			editItem{label: "displayName (definition)", kind: editDefn, key: "displayName"},
			editItem{label: "baseUrl (definition)", kind: editDefn, key: "baseUrl"},
			editItem{label: "wireFormat (definition)", kind: editWireDefn, key: "wireFormat"},
			editItem{label: "apiKeyEnvVar (definition)", kind: editDefn, key: "apiKeyEnvVar"},
		)
	}

	for _, key := range m.deps.ValidSettingKeys(p.ID) {
		if key == "model" {
			continue // handled by the model picker row above
		}
		if p.IsCustom && key == "baseUrl" {
			continue // edited via the definition row above
		}
		items = append(items, editItem{label: key, kind: editOverride, key: key})
	}
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
		if m.targetWire == config.WireFormatGemini {
			// Gemini has no model-discovery endpoint; type the name.
			m.editingKey = "model"
			m.editingKind = editModel
			m.input = freshInput("gemini model name (e.g. gemini-2.5-pro)")
			m.input.SetValue(currentValue(m.deps.ProviderSettings(m.targetID), "model"))
			m.screen = screenEditField
			return m, nil
		}
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
	case editDefn, editOverride:
		m.editingKey = item.key
		m.editingKind = item.kind
		m.input = freshInput(item.key + " value")
		m.input.SetValue(currentValue(m.deps.ProviderSettings(m.targetID), item.key))
		m.screen = screenEditField
	}
	return m, nil
}

func (m Model) enterSetKey() Model {
	m.input = freshInput("paste API key, then Enter")
	m.input.EchoMode = textinput.EchoPassword
	m.screen = screenSetKey
	return m
}

func (m Model) enterModels(id string) (Model, tea.Cmd) {
	m.targetID = id
	m.loading = true
	m.models = nil
	m.modelsErr = ""
	m.cursor = 0
	if m.screen != screenAddModels {
		m.screen = screenModels
	}
	return m, discoverCmd(m.ctx, m.deps, id)
}

// ---- text entry (edit field + set key) -----------------------------------

func (m Model) handleTextEntryKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		if m.screen == screenSetKey {
			m.screen = screenMenu
		} else {
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
		m.screen = screenMenu
		m.cursor = 0
		return m, nil
	}

	// edit field
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
	case addFieldID:
		if value == "" {
			m.errMsg = "Provider id is required."
			return m, nil
		}
		if _, ok := config.LookupBuiltInProvider(value); ok {
			m.errMsg = fmt.Sprintf("%q is a built-in provider id. Choose a different id.", value)
			return m, nil
		}
		m.add.id = value
		m.add.fieldIdx = addFieldName
		m.input = freshInput("display name (optional)")
	case addFieldName:
		m.add.displayName = value
		m.add.fieldIdx = addFieldBaseURL
		m.input = freshInput("base URL (e.g. http://127.0.0.1:8000/v1/chat/completions)")
	case addFieldBaseURL:
		if value == "" {
			m.errMsg = "Base URL is required."
			return m, nil
		}
		m.add.baseURL = value
		m.add.fieldIdx = addFieldWire
		m.input.Blur()
	case addFieldEnvVar:
		m.add.envVar = value
		m.add.fieldIdx = addFieldAPIKey
		m.input = freshInput("API key (optional, stored in keychain)")
		m.input.EchoMode = textinput.EchoPassword
	case addFieldAPIKey:
		m.add.apiKey = value
		return m.submitAdd()
	}
	m.errMsg = ""
	return m, nil
}

func (m Model) submitAdd() (Model, tea.Cmd) {
	def := config.CustomProviderDefinition{
		DisplayName:  m.add.displayName,
		BaseURL:      m.add.baseURL,
		APIKeyEnvVar: m.add.envVar,
		WireFormat:   m.add.wire,
	}
	if err := m.deps.AddCustomProvider(m.ctx, m.add.id, def, m.add.apiKey); err != nil {
		m.errMsg = err.Error()
		return m, nil
	}
	m.providers = m.deps.ListProviders()
	// Immediately connect and discover models so the user can pick a default.
	m.targetID = m.add.id
	m.loading = true
	m.models = nil
	m.modelsErr = ""
	m.cursor = 0
	m.screen = screenAddModels
	return m, discoverCmd(m.ctx, m.deps, m.add.id)
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
	m.status = fmt.Sprintf("Added %s with model %s and switched to it.", config.ProviderDisplayID(m.targetID), model)
	m.info = m.status
	m.providers = m.deps.ListProviders()
	m.screen = screenMenu
	m.cursor = 0
	return m, nil
}

// ---- remove --------------------------------------------------------------

func (m Model) selectRemove() (Model, tea.Cmd) {
	customs := m.customProviders()
	if m.cursor < 0 || m.cursor >= len(customs) {
		return m, nil
	}
	id := customs[m.cursor].ID
	if err := m.deps.RemoveCustomProvider(m.ctx, id); err != nil {
		m.errMsg = err.Error()
		return m, nil
	}
	m.status = fmt.Sprintf("Removed custom provider %q.", id)
	m.info = m.status
	m.providers = m.deps.ListProviders()
	m.cursor = 0
	if len(m.customProviders()) == 0 {
		m.screen = screenMenu
	}
	return m, nil
}

// ---- helpers -------------------------------------------------------------

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
	ti.Focus()
	return ti
}

func currentValue(settings map[string]string, key string) string {
	if settings == nil {
		return ""
	}
	return settings[key]
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
