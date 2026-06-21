// Package onboardingdialog implements the first-run provider setup overlay for
// the Bubble Tea TUI. It guides the user through choosing an endpoint (Gemini,
// OpenRouter, or custom OpenAI-compatible), entering credentials, and picking a
// starting model from a live discovery list.
package onboardingdialog

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/undeadindustries/sagittarius/internal/ui/theme"
)

type screen int

const (
	screenChoose screen = iota
	screenAPIKey
	screenCustomURL
	screenCustomKey
	screenModels
)

type providerChoice int

const (
	choiceGemini providerChoice = iota
	choiceOpenRouter
	choiceCustom
)

var choiceLabels = []string{
	"Gemini — Google AI API key",
	"OpenRouter — hosted models API key",
	"Custom — OpenAI-compatible endpoint (base URL + key)",
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

	choice     providerChoice
	cursor     int
	targetID   string
	customURL  string
	loading    bool
	models     []string
	modelsErr  string
	listOffset int

	input textinput.Model
}

// New constructs the onboarding dialog at the provider-choice screen.
func New(ctx context.Context, deps Deps) Model {
	ti := textinput.New()
	ti.CharLimit = 8192
	ti.Prompt = "> "
	return Model{
		deps:   deps,
		ctx:    ctx,
		th:     theme.Default(),
		screen: screenChoose,
		input:  ti,
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
	case "enter":
		return m.activate()
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) moveCursor(delta int) Model {
	switch m.screen {
	case screenChoose:
		n := len(choiceLabels)
		if n == 0 {
			return m
		}
		m.cursor = (m.cursor + delta%n + n) % n
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
	}
	return m
}

func (m Model) activate() (Model, tea.Cmd) {
	switch m.screen {
	case screenChoose:
		if m.cursor < 0 || m.cursor >= len(choiceLabels) {
			return m, nil
		}
		m.choice = providerChoice(m.cursor)
		m.errMsg = ""
		switch m.choice {
		case choiceGemini:
			m.screen = screenAPIKey
			m.input = freshSecretInput()
			m.info = "Paste your Gemini API key (from Google AI Studio)."
		case choiceOpenRouter:
			m.screen = screenAPIKey
			m.input = freshSecretInput()
			m.info = "Paste your OpenRouter API key."
		case choiceCustom:
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
	}
	return m, nil
}

func (m Model) submitAPIKey() (Model, tea.Cmd) {
	key := strings.TrimSpace(m.input.Value())
	var id string
	var err error
	switch m.choice {
	case choiceGemini:
		id, err = m.deps.PrepareGemini(m.ctx, key)
	case choiceOpenRouter:
		id, err = m.deps.PrepareOpenRouter(m.ctx, key)
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
	if m.screen != screenModels || len(m.models) == 0 {
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
