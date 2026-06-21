package providersdialog

import (
	"context"
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/undeadindustries/sagittarius/internal/config"
)

type fakeDeps struct {
	providers []ProviderEntry
	active    string

	switched     string
	added        string
	removed      string
	setModel     map[string]string
	applied      []string
	updatedDefn  []string
	keys         map[string]string
	models       []string
	discoverErr  error
	settingsByID map[string]map[string]string
}

func newFakeDeps() *fakeDeps {
	return &fakeDeps{
		providers: []ProviderEntry{
			{ID: "gemini-apikey", DisplayID: "gemini", DisplayName: "Gemini", WireFormat: config.WireFormatGemini, IsActive: true},
			{ID: "openai", DisplayID: "openai", DisplayName: "OpenAI", WireFormat: config.WireFormatOpenAIChat},
			{ID: "my-vllm", DisplayID: "my-vllm", DisplayName: "Local", WireFormat: config.WireFormatOpenAIChat, IsCustom: true},
		},
		active:   "gemini-apikey",
		setModel: map[string]string{},
		keys:     map[string]string{},
	}
}

func (f *fakeDeps) ListProviders() []ProviderEntry { return f.providers }
func (f *fakeDeps) ActiveProviderID() string       { return f.active }
func (f *fakeDeps) SwitchProvider(_ context.Context, id string) error {
	f.switched = id
	f.active = id
	return nil
}
func (f *fakeDeps) SetAPIKey(_ context.Context, id, key string) error {
	f.keys[id] = key
	return nil
}
func (f *fakeDeps) AddCustomProvider(_ context.Context, id string, _ config.CustomProviderDefinition, _ string) error {
	f.added = id
	return nil
}
func (f *fakeDeps) RemoveCustomProvider(_ context.Context, id string) error {
	f.removed = id
	return nil
}
func (f *fakeDeps) DiscoverModels(_ context.Context, _ string) ([]string, error) {
	return f.models, f.discoverErr
}
func (f *fakeDeps) SetModel(_ context.Context, id, model string) error {
	f.setModel[id] = model
	return nil
}
func (f *fakeDeps) ApplySetting(_ context.Context, id, key, value string) error {
	f.applied = append(f.applied, id+"."+key+"="+value)
	return nil
}
func (f *fakeDeps) UpdateCustomDefinition(_ context.Context, id, field, value string) error {
	f.updatedDefn = append(f.updatedDefn, id+"."+field+"="+value)
	return nil
}
func (f *fakeDeps) ProviderSettings(id string) map[string]string {
	if f.settingsByID != nil {
		return f.settingsByID[id]
	}
	return map[string]string{}
}
func (f *fakeDeps) ValidSettingKeys(id string) []string {
	for _, p := range f.providers {
		if p.ID == id {
			return config.ValidSettingKeys(p.WireFormat)
		}
	}
	return nil
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func send(m Model, msgs ...tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	for _, msg := range msgs {
		m, cmd = m.Update(msg)
	}
	return m, cmd
}

func TestMenuOpensSwitchAndSelects(t *testing.T) {
	deps := newFakeDeps()
	m := New(context.Background(), deps)

	// Enter on "Switch active provider" (cursor 0).
	m, _ = send(m, key("enter"))
	if m.screen != screenSwitch {
		t.Fatalf("screen = %d, want switch", m.screen)
	}
	// Move to the second provider (openai) and select it.
	m, _ = send(m, key("down"), key("enter"))
	if deps.switched != "openai" {
		t.Fatalf("switched = %q, want openai", deps.switched)
	}
	if m.screen != screenMenu {
		t.Fatalf("screen = %d, want menu after switch", m.screen)
	}
}

func TestEditPickToEditSheet(t *testing.T) {
	deps := newFakeDeps()
	m := New(context.Background(), deps)

	// menu cursor: 0 switch, 1 edit.
	m, _ = send(m, key("down"), key("enter"))
	if m.screen != screenEditPick {
		t.Fatalf("screen = %d, want editPick", m.screen)
	}
	// Select openai (index 1).
	m, _ = send(m, key("down"), key("enter"))
	if m.screen != screenEdit {
		t.Fatalf("screen = %d, want edit", m.screen)
	}
	if m.targetID != "openai" {
		t.Fatalf("targetID = %q, want openai", m.targetID)
	}
	// Edit sheet must include the openai-chat allowlist (temperature row).
	found := false
	for _, it := range m.editItems {
		if it.key == "temperature" {
			found = true
		}
	}
	if !found {
		t.Fatalf("edit items missing temperature: %+v", m.editItems)
	}
}

func TestAddFlowDiscoversAndPicksModel(t *testing.T) {
	deps := newFakeDeps()
	deps.models = []string{"qwen3", "llama3"}
	m := New(context.Background(), deps)

	// Navigate menu to "Add provider" (index 3).
	m, _ = send(m, key("down"), key("down"), key("down"), key("enter"))
	if m.screen != screenAdd {
		t.Fatalf("screen = %d, want add", m.screen)
	}

	// id
	m.input.SetValue("local-vllm")
	m, _ = send(m, key("enter"))
	// displayName
	m.input.SetValue("Local")
	m, _ = send(m, key("enter"))
	// baseUrl
	m.input.SetValue("http://127.0.0.1:8000/v1/chat/completions")
	m, _ = send(m, key("enter"))
	// wireFormat toggle step → Enter advances
	m, _ = send(m, key("enter"))
	// envVar (optional, leave blank)
	m, _ = send(m, key("enter"))
	// apiKey (optional, blank) → submit
	m, cmd := send(m, key("enter"))

	if deps.added != "local-vllm" {
		t.Fatalf("added = %q, want local-vllm", deps.added)
	}
	if m.screen != screenAddModels {
		t.Fatalf("screen = %d, want addModels", m.screen)
	}
	if cmd == nil {
		t.Fatal("expected a discover command after add")
	}
	// Execute the discover command and feed its message back.
	msg := cmd()
	m, _ = send(m, msg)
	if m.loading {
		t.Fatal("still loading after models delivered")
	}
	if len(m.models) != 2 {
		t.Fatalf("models = %v, want 2", m.models)
	}
	// Pick the first model.
	m, _ = send(m, key("enter"))
	if deps.setModel["local-vllm"] != "qwen3" {
		t.Fatalf("setModel = %v, want qwen3", deps.setModel)
	}
	if deps.switched != "local-vllm" {
		t.Fatalf("switched = %q, want local-vllm", deps.switched)
	}
}

func TestModelsDiscoverError(t *testing.T) {
	deps := newFakeDeps()
	deps.discoverErr = errors.New("connection refused")
	m := New(context.Background(), deps)
	m.targetID = "openai"

	m, cmd := m.enterModels("openai")
	if cmd == nil {
		t.Fatal("expected discover command")
	}
	m, _ = send(m, cmd())
	if m.modelsErr == "" {
		t.Fatal("expected modelsErr to be set")
	}
}

func TestEscFromMenuCloses(t *testing.T) {
	deps := newFakeDeps()
	m := New(context.Background(), deps)
	m, _ = send(m, key("esc"))
	if !m.Done() {
		t.Fatal("esc from menu should close the dialog")
	}
}

func TestRemoveCustomProvider(t *testing.T) {
	deps := newFakeDeps()
	m := New(context.Background(), deps)
	// menu: 0 switch,1 edit,2 setkey,3 add,4 remove
	m, _ = send(m, key("down"), key("down"), key("down"), key("down"), key("enter"))
	if m.screen != screenRemove {
		t.Fatalf("screen = %d, want remove", m.screen)
	}
	// Only my-vllm is custom; select it.
	m, _ = send(m, key("enter"))
	if deps.removed != "my-vllm" {
		t.Fatalf("removed = %q, want my-vllm", deps.removed)
	}
}
