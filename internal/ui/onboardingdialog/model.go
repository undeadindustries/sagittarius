// Package onboardingdialog implements the first-run provider setup overlay for
// the Bubble Tea TUI. It guides the user through choosing an endpoint (Gemini,
// one of the built-in provider presets, or a custom OpenAI-compatible base
// URL), entering credentials, and picking a starting model from a live
// discovery list.
package onboardingdialog

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/ui/theme"
)

type screen int

const (
	screenChoose screen = iota
	screenAPIKey
	screenCustomURL
	screenCustomKey
	screenModels
	screenManualModel
)

type choiceKind int

const (
	choiceKindGemini choiceKind = iota
	choiceKindPreset
	choiceKindCustom
)

// onboardChoice is one row on the connect-method screen: Gemini (native), a
// provider preset, or the custom base-URL flow.
type onboardChoice struct {
	kind     choiceKind
	presetID string
	label    string
	note     string
	// defaultModel lets the model step offer a sensible default when discovery
	// returns nothing (e.g. non-/v1 endpoints like z.ai).
	defaultModel string
}

// buildChoices assembles the ordered connect-method list: Gemini first, then
// every provider preset, then the custom (blank) flow.
func buildChoices() []onboardChoice {
	choices := make([]onboardChoice, 0, len(config.ProviderPresets)+2)
	choices = append(choices, onboardChoice{
		kind:  choiceKindGemini,
		label: "Gemini — Google AI API key",
	})
	for _, p := range config.ProviderPresets {
		choices = append(choices, onboardChoice{
			kind:         choiceKindPreset,
			presetID:     p.ID,
			label:        fmt.Sprintf("%s — %s", p.DisplayName, string(p.WireFormat)),
			note:         p.Note,
			defaultModel: p.DefaultModel,
		})
	}
	choices = append(choices, onboardChoice{
		kind:  choiceKindCustom,
		label: "Custom — OpenAI-compatible endpoint (base URL + key)",
	})
	return choices
}

// modelsLoadedMsg is delivered when async DiscoverModels completes.
type modelsLoadedMsg struct {
	id     string
	models []string
	err    error
}

// Model is the first-run onboarding overlay.
type Model struct {
	deps Deps
	ctx  context.Context
	th   theme.Theme

	width  int
	height int

	screen screen
	done   bool
	status string

	errMsg string
	info   string

	choices   []onboardChoice
	choice    onboardChoice
	cursor    int
	targetID  string
	customURL string
	// defaultModel carries the chosen preset's default so the model step can
	// offer it when discovery returns nothing.
	defaultModel string
	loading      bool
	models       []string
	modelsErr    string
	listOffset   int

	input textinput.Model
}

// New constructs the onboarding dialog at the provider-choice screen.
func New(ctx context.Context, deps Deps) Model {
	ti := textinput.New()
	ti.CharLimit = 8192
	ti.Prompt = "> "
	return Model{
		deps:    deps,
		ctx:     ctx,
		th:      theme.Default(),
		screen:  screenChoose,
		choices: buildChoices(),
		input:   ti,
	}
}

func (m Model) Done() bool     { return m.done }
func (m Model) Status() string { return m.status }

func (m Model) SetSize(w, h int) Model {
	m.width = w
	m.height = h
	m.syncInputWidth()
	m.ensureListVisible()
	return m
}

func (m Model) SetTheme(th theme.Theme) Model {
	m.th = th
	return m
}

func (m *Model) syncInputWidth() {
	if m.width <= 0 {
		return
	}
	w := m.contentWidth() - 2
	if w < 1 {
		w = 1
	}
	m.input.Width = w
}

func (m Model) contentWidth() int {
	w := m.width - 4
	if w < 20 {
		return 20
	}
	return w
}

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
		return m
	}
	m.loading = false
	if msg.err != nil {
		m.modelsErr = msg.err.Error()
		m.models = nil
	} else {
		m.modelsErr = ""
		m.models = msg.models
	}
	// When discovery returns nothing (empty list or error), fall back to the
	// chosen preset's default model so non-/v1 endpoints (e.g. z.ai) are still
	// usable; the user can also press 'm' to type a model name.
	if len(m.models) == 0 && m.defaultModel != "" {
		m.models = []string{m.defaultModel}
		m.modelsErr = ""
	}
	m.cursor = 0
	m.listOffset = 0
	return m
}

func (m Model) handleKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		return m.moveCursor(-1), nil
	case "down", "j":
		return m.moveCursor(1), nil
	case "esc":
		return m.back(), nil
	case "m":
		if m.screen == screenModels && !m.loading {
			return m.enterManualModel(), nil
		}
	case "enter":
		return m.activate()
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// enterManualModel opens a text field to type a model name when discovery does
// not surface the model the user wants (or returns nothing).
func (m Model) enterManualModel() Model {
	m.screen = screenManualModel
	m.input = freshInput("model name (e.g. gpt-4o-mini)")
	m.errMsg = ""
	m.info = "Type the exact model id to use with this endpoint."
	m.syncInputWidth()
	return m
}

func (m Model) moveCursor(delta int) Model {
	switch m.screen {
	case screenChoose:
		n := len(m.choices)
		if n == 0 {
			return m
		}
		m.cursor = (m.cursor + delta%n + n) % n
		m.ensureListVisible()
	case screenModels:
		if len(m.models) == 0 {
			return m
		}
		m.cursor = (m.cursor + delta%len(m.models) + len(m.models)) % len(m.models)
		m.ensureListVisible()
	}
	return m
}

func (m Model) back() Model {
	switch m.screen {
	case screenAPIKey, screenCustomURL:
		m.screen = screenChoose
		m.errMsg = ""
		m.input.Blur()
	case screenCustomKey:
		m.screen = screenCustomURL
		m.errMsg = ""
		m.input = freshInput("base URL (e.g. http://127.0.0.1:8000/v1/chat/completions)")
		m.input.SetValue(m.customURL)
		m.syncInputWidth()
	case screenModels:
		m.screen = screenChoose
		m.models = nil
		m.loading = false
		m.errMsg = ""
	case screenManualModel:
		m.screen = screenModels
		m.errMsg = ""
		m.input.Blur()
	}
	return m
}

func (m Model) activate() (Model, tea.Cmd) {
	switch m.screen {
	case screenChoose:
		if m.cursor < 0 || m.cursor >= len(m.choices) {
			return m, nil
		}
		m.choice = m.choices[m.cursor]
		m.defaultModel = m.choice.defaultModel
		m.errMsg = ""
		switch m.choice.kind {
		case choiceKindGemini:
			m.screen = screenAPIKey
			m.input = freshSecretInput()
			m.info = "Paste your Gemini API key (from Google AI Studio)."
		case choiceKindPreset:
			m.screen = screenAPIKey
			m.input = freshSecretInput()
			m.info = m.presetKeyInfo()
		case choiceKindCustom:
			m.screen = screenCustomURL
			m.input = freshInput("base URL (e.g. http://127.0.0.1:8000/v1/chat/completions)")
			m.info = "Enter the chat-completions URL for your OpenAI-compatible server."
		}
		m.syncInputWidth()
		return m, nil
	case screenAPIKey:
		return m.submitAPIKey()
	case screenCustomURL:
		url := strings.TrimSpace(m.input.Value())
		if url == "" {
			m.errMsg = "Base URL is required."
			return m, nil
		}
		m.customURL = url
		m.screen = screenCustomKey
		m.input = freshSecretInput()
		m.info = "Paste the bearer token or API key for this endpoint."
		m.errMsg = ""
		m.syncInputWidth()
		return m, nil
	case screenCustomKey:
		return m.submitCustomKey()
	case screenModels:
		return m.selectModel()
	case screenManualModel:
		return m.submitManualModel()
	}
	return m, nil
}

func (m Model) submitManualModel() (Model, tea.Cmd) {
	name := strings.TrimSpace(m.input.Value())
	if name == "" {
		m.errMsg = "Model name is required."
		return m, nil
	}
	if err := m.deps.CompleteSetup(m.ctx, m.targetID, name); err != nil {
		m.errMsg = err.Error()
		return m, nil
	}
	m.status = fmt.Sprintf("Connected to %s with model %s.", m.targetID, name)
	m.done = true
	return m, nil
}

// presetKeyInfo builds the API-key prompt for the chosen preset, appending its
// caveat note when present.
func (m Model) presetKeyInfo() string {
	name := m.choice.label
	if p, ok := config.LookupProviderPreset(m.choice.presetID); ok {
		name = p.DisplayName
	}
	info := fmt.Sprintf("Paste your %s API key.", name)
	if m.choice.note != "" {
		info += " Note: " + m.choice.note
	}
	return info
}

func (m Model) submitAPIKey() (Model, tea.Cmd) {
	key := strings.TrimSpace(m.input.Value())
	var id string
	var err error
	switch m.choice.kind {
	case choiceKindGemini:
		id, err = m.deps.PrepareGemini(m.ctx, key)
	case choiceKindPreset:
		id, err = m.deps.PreparePreset(m.ctx, m.choice.presetID, key)
	default:
		m.errMsg = "unexpected provider choice"
		return m, nil
	}
	if err != nil {
		m.errMsg = err.Error()
		return m, nil
	}
	return m.beginDiscover(id)
}

func (m Model) submitCustomKey() (Model, tea.Cmd) {
	key := strings.TrimSpace(m.input.Value())
	id, err := m.deps.PrepareCustom(m.ctx, m.customURL, key)
	if err != nil {
		m.errMsg = err.Error()
		return m, nil
	}
	return m.beginDiscover(id)
}

func (m Model) beginDiscover(id string) (Model, tea.Cmd) {
	m.targetID = id
	m.screen = screenModels
	m.loading = true
	m.models = nil
	m.modelsErr = ""
	m.cursor = 0
	m.listOffset = 0
	m.input.Blur()
	m.errMsg = ""
	m.info = "Discovering models from your endpoint…"
	return m, discoverCmd(m.ctx, m.deps, id)
}

func (m Model) selectModel() (Model, tea.Cmd) {
	if m.loading || len(m.models) == 0 {
		return m, nil
	}
	if m.cursor < 0 || m.cursor >= len(m.models) {
		return m, nil
	}
	model := m.models[m.cursor]
	if err := m.deps.CompleteSetup(m.ctx, m.targetID, model); err != nil {
		m.errMsg = err.Error()
		return m, nil
	}
	m.status = fmt.Sprintf("Connected to %s with model %s.", m.targetID, model)
	m.done = true
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
	ti.Placeholder = placeholder
	ti.CharLimit = 8192
	ti.Prompt = "> "
	ti.Focus()
	return ti
}

func freshSecretInput() textinput.Model {
	ti := freshInput("API key")
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '•'
	return ti
}

func (m *Model) ensureListVisible() {
	var n int
	switch m.screen {
	case screenChoose:
		n = len(m.choices)
	case screenModels:
		n = len(m.models)
	default:
		return
	}
	if n == 0 {
		return
	}
	visible := m.visibleListRows()
	if m.cursor < m.listOffset {
		m.listOffset = m.cursor
	}
	if m.cursor >= m.listOffset+visible {
		m.listOffset = m.cursor - visible + 1
	}
}

func (m Model) visibleListRows() int {
	if m.height <= 0 {
		return 12
	}
	rows := m.height - 12
	if rows < 4 {
		return 4
	}
	return rows
}
