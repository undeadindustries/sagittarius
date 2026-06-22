package providersdialog

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
	effective    map[string]map[string]string
	activeModels map[string][]string
	currentModel map[string]string
	presetID     map[string]string
	appliedPwith []string // "id:presetID" for ApplySystemPromptPreset
	cleared      []string // "id.key" for ClearSetting
	resetIDs     []string // ids passed to ResetSettings
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
func (f *fakeDeps) CurrentModel(id string) string {
	if f.currentModel == nil {
		return ""
	}
	return f.currentModel[id]
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
func (f *fakeDeps) ActiveModels(id string) []string { return f.activeModels[id] }
func (f *fakeDeps) SetActiveModels(_ context.Context, id string, models []string) error {
	if f.activeModels == nil {
		f.activeModels = map[string][]string{}
	}
	f.activeModels[id] = models
	return nil
}
func (f *fakeDeps) EffectiveProviderSettings(id string) map[string]string {
	if f.effective != nil {
		return f.effective[id]
	}
	return map[string]string{}
}
func (f *fakeDeps) SystemPromptPresetID(id string) string {
	if f.presetID != nil {
		return f.presetID[id]
	}
	return ""
}
func (f *fakeDeps) ApplySystemPromptPreset(_ context.Context, id, presetID string) (string, error) {
	f.appliedPwith = append(f.appliedPwith, id+":"+presetID)
	if f.presetID == nil {
		f.presetID = map[string]string{}
	}
	f.presetID[id] = presetID
	return "applied " + presetID, nil
}
func (f *fakeDeps) ClearSetting(_ context.Context, id, key string) error {
	f.cleared = append(f.cleared, id+"."+key)
	return nil
}
func (f *fakeDeps) ResetSettings(_ context.Context, id string) error {
	f.resetIDs = append(f.resetIDs, id)
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

// TestDialogOpensAtProviderList verifies the dialog opens directly at the
// provider list (screenEditPick), not at a top-level menu.
func TestDialogOpensAtProviderList(t *testing.T) {
	deps := newFakeDeps()
	m := New(context.Background(), deps)
	if m.screen != screenEditPick {
		t.Fatalf("initial screen = %d, want screenEditPick", m.screen)
	}
}

func TestEditPickToEditSheet(t *testing.T) {
	deps := newFakeDeps()
	m := New(context.Background(), deps)

	// Provider list starts at index 0 (gemini). Move to openai (index 1) and enter.
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

	// Press 'a' to add a new provider from the provider list.
	m, _ = send(m, key("a"))
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

func TestToggleAllChecked(t *testing.T) {
	deps := newFakeDeps()
	deps.active = "openai"
	deps.models = []string{"a", "b", "c"}
	// Pre-curate so all three start checked.
	deps.activeModels = map[string][]string{"openai": {"a", "b", "c"}}
	m := New(context.Background(), deps)

	// Use enterModels directly so the test focuses on toggle behavior, not navigation.
	m, cmd := m.enterModels("openai")
	m, _ = send(m, cmd())
	if len(m.checked) != 3 || !m.checked[0] || !m.checked[1] || !m.checked[2] {
		t.Fatalf("expected all checked (from curated set), got %v", m.checked)
	}

	m, _ = send(m, key("A"))
	for i, c := range m.checked {
		if c {
			t.Fatalf("checked[%d] = true after deselect all", i)
		}
	}

	m, _ = send(m, key("A"))
	for i, c := range m.checked {
		if !c {
			t.Fatalf("checked[%d] = false after select all", i)
		}
	}
}

func viewLineCount(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

func TestListScrollWindow(t *testing.T) {
	deps := newFakeDeps()
	deps.active = "openai"
	models := make([]string, 40)
	for i := range models {
		models[i] = fmt.Sprintf("model-%02d", i)
	}
	deps.models = models
	m := New(context.Background(), deps)

	// Use enterModels directly; scroll/window behavior is independent of navigation path.
	m, cmd := m.enterModels("openai")
	m, _ = send(m, cmd())
	m = m.SetSize(80, 20)

	view := m.View()
	if !strings.Contains(view, "more below") {
		t.Fatalf("expected scroll indicator in view:\n%s", view)
	}
	if strings.Contains(view, "model-39") {
		t.Fatal("last model should not render when windowed")
	}

	for i := 0; i < 50; i++ {
		m, _ = send(m, key("down"))
	}
	view = m.View()
	if !strings.Contains(view, "more above") {
		t.Fatalf("expected above indicator after scrolling down:\n%s", view)
	}
	if strings.Contains(view, "model-00") {
		t.Fatal("first model should not render when scrolled down")
	}
}

func TestActivationViewFitsTerminal(t *testing.T) {
	deps := newFakeDeps()
	deps.active = "openai"
	models := make([]string, 40)
	for i := range models {
		models[i] = fmt.Sprintf("gemini-model-%02d-with-a-long-name", i)
	}
	deps.models = models
	m := New(context.Background(), deps)

	// Use enterModels directly; terminal-fit behavior is independent of navigation path.
	m, cmd := m.enterModels("openai")
	m, _ = send(m, cmd())
	m = m.SetSize(97, 35)

	view := m.View()
	lines := viewLineCount(view)
	if lines > 35 {
		t.Fatalf("view has %d lines, want <= 35:\n%s", lines, view)
	}
}

func TestManageModelsActivationSaves(t *testing.T) {
	deps := newFakeDeps()
	deps.active = "openai"
	deps.models = []string{"qwen3", "llama3", "mistral"}
	// Pre-curate so initChecked uses the curated set (all three checked).
	deps.activeModels = map[string][]string{"openai": {"qwen3", "llama3", "mistral"}}
	m := New(context.Background(), deps)

	// Provider list → pick openai (index 1) → edit sheet.
	m, _ = send(m, key("down"), key("enter"))
	if m.screen != screenEdit || m.targetID != "openai" {
		t.Fatalf("screen = %d target = %q, want edit/openai", m.screen, m.targetID)
	}
	// Move to "Manage models…" row on the edit sheet and select it.
	if !cursorTo(&m, editModel, "") {
		t.Fatal("Manage models row missing from edit sheet")
	}
	m, cmd := send(m, key("enter"))
	if m.screen != screenModels {
		t.Fatalf("screen = %d, want models after selecting Manage models", m.screen)
	}
	if cmd == nil {
		t.Fatal("expected discover command")
	}
	m, _ = send(m, cmd())
	// Curated → all three checked.
	for i, c := range m.checked {
		if !c {
			t.Fatalf("model %d not checked (curated set)", i)
		}
	}
	// Uncheck the second model (llama3), then save.
	m, _ = send(m, key("down"), key(" "), key("enter"))
	got := deps.activeModels["openai"]
	if len(got) != 2 || got[0] != "qwen3" || got[1] != "mistral" {
		t.Fatalf("saved activeModels = %v, want [qwen3 mistral]", got)
	}
	// After save from edit sheet flow, returns to edit sheet.
	if m.screen != screenEdit {
		t.Fatalf("screen = %d, want edit after save", m.screen)
	}
}

// TestManageModelsUncuratedDefaultsToSingleModel verifies that when a provider
// has no curated activeModels, initChecked checks only the configured default
// model rather than every discovered model.
func TestManageModelsUncuratedDefaultsToSingleModel(t *testing.T) {
	deps := newFakeDeps()
	deps.models = []string{"gemini-2.5-flash", "gemini-2.5-pro", "gemini-1.5-pro"}
	deps.settingsByID = map[string]map[string]string{
		"gemini-apikey": {"model": "gemini-2.5-pro"},
	}
	m := New(context.Background(), deps)

	// Directly enter the activation screen for gemini.
	m, cmd := m.enterModels("gemini-apikey")
	m, _ = send(m, cmd())

	// Only gemini-2.5-pro (index 1) should be checked; others unchecked.
	want := []bool{false, true, false}
	for i, got := range m.checked {
		if got != want[i] {
			t.Fatalf("checked[%d] = %v, want %v (model %q)", i, got, want[i], m.models[i])
		}
	}
}

// TestActivationSwitchesLiveModelWhenDeactivated verifies that deactivating the
// live model on the active provider re-points it at the first checked model.
func TestActivationSwitchesLiveModelWhenDeactivated(t *testing.T) {
	deps := newFakeDeps()
	deps.active = "openai"
	deps.models = []string{"qwen3", "llama3", "mistral"}
	deps.currentModel = map[string]string{"openai": "qwen3"}
	// Pre-curate so all three start checked.
	deps.activeModels = map[string][]string{"openai": {"qwen3", "llama3", "mistral"}}
	m := New(context.Background(), deps)

	// Directly enter the activation screen (bypasses main-menu picker for simplicity).
	m, cmd := m.enterModels("openai")
	m, _ = send(m, cmd())

	// Uncheck the live model (qwen3, index 0), then save.
	m, _ = send(m, key(" "), key("enter"))

	got := deps.activeModels["openai"]
	if len(got) != 2 || got[0] != "llama3" || got[1] != "mistral" {
		t.Fatalf("saved activeModels = %v, want [llama3 mistral]", got)
	}
	if deps.setModel["openai"] != "llama3" {
		t.Fatalf("live model = %q, want llama3 (first checked)", deps.setModel["openai"])
	}
}

// TestActivationKeepsLiveModelWhenStillChecked verifies that the live model is
// untouched when it remains in the curated set.
func TestActivationKeepsLiveModelWhenStillChecked(t *testing.T) {
	deps := newFakeDeps()
	deps.active = "openai"
	deps.models = []string{"qwen3", "llama3", "mistral"}
	deps.currentModel = map[string]string{"openai": "qwen3"}
	// Pre-curate so all three start checked.
	deps.activeModels = map[string][]string{"openai": {"qwen3", "llama3", "mistral"}}
	m := New(context.Background(), deps)

	// Directly enter the activation screen (bypasses main-menu picker for simplicity).
	m, cmd := m.enterModels("openai")
	m, _ = send(m, cmd())

	// Uncheck mistral (index 2), leaving the live model qwen3 checked.
	m, _ = send(m, key("down"), key("down"), key(" "), key("enter"))

	if _, ok := deps.setModel["openai"]; ok {
		t.Fatalf("SetModel should not be called when live model stays checked, got %v", deps.setModel)
	}
}

// TestEditModelOpensActivationAndReturns verifies the edit sheet's model row
// opens the activation screen for the edited provider and returns to the edit
// sheet after saving (not the top menu).
func TestEditModelOpensActivationAndReturns(t *testing.T) {
	deps := newFakeDeps()
	deps.models = []string{"gemini-2.5-pro", "gemini-2.5-flash"}
	// Pre-curate so both start checked.
	deps.activeModels = map[string][]string{"gemini-apikey": {"gemini-2.5-pro", "gemini-2.5-flash"}}
	m := New(context.Background(), deps)

	// Provider list: index 0 = gemini → edit sheet.
	m, _ = send(m, key("enter"))
	if m.screen != screenEdit || m.targetID != "gemini-apikey" {
		t.Fatalf("screen = %d target = %q, want edit/gemini-apikey", m.screen, m.targetID)
	}
	// Move to "Manage models…" row on edit sheet and select.
	if !cursorTo(&m, editModel, "") {
		t.Fatal("Manage models row missing")
	}
	m, cmd := send(m, key("enter"))
	if m.screen != screenModels {
		t.Fatalf("screen = %d, want models after selecting Manage models", m.screen)
	}
	if m.modelsFrom != screenEdit {
		t.Fatalf("modelsFrom = %d, want screenEdit", m.modelsFrom)
	}
	if cmd == nil {
		t.Fatal("expected discover command")
	}
	m, _ = send(m, cmd())
	// Both curated → both checked. Deactivate the second model, then save.
	m, _ = send(m, key("down"), key(" "), key("enter"))
	got := deps.activeModels["gemini-apikey"]
	if len(got) != 1 || got[0] != "gemini-2.5-pro" {
		t.Fatalf("saved activeModels = %v, want [gemini-2.5-pro]", got)
	}
	if m.screen != screenEdit {
		t.Fatalf("screen = %d, want edit after save (returned from activation)", m.screen)
	}
}

// TestEditModelActivationEscReturnsToEdit verifies Esc from the activation
// screen returns to the edit sheet when opened from there.
func TestEditModelActivationEscReturnsToEdit(t *testing.T) {
	deps := newFakeDeps()
	deps.models = []string{"gemini-2.5-pro"}
	m := New(context.Background(), deps)

	// editPick → gemini edit sheet (index 0, enter).
	m, _ = send(m, key("enter"))
	if m.screen != screenEdit {
		t.Fatalf("screen = %d, want edit", m.screen)
	}
	// Move to "Manage models…" and select.
	if !cursorTo(&m, editModel, "") {
		t.Fatal("Manage models row missing")
	}
	m, cmd := send(m, key("enter"))
	if m.screen != screenModels {
		t.Fatalf("screen = %d, want models", m.screen)
	}
	m, _ = send(m, cmd())
	m, _ = send(m, key("esc"))
	if m.screen != screenEdit {
		t.Fatalf("screen = %d, want edit after esc from activation", m.screen)
	}
}

func TestManageModelsGeminiStartsDiscovery(t *testing.T) {
	deps := newFakeDeps() // active is gemini-apikey (WireFormatGemini)
	m := New(context.Background(), deps)

	// Provider list at index 0 = gemini; Enter → edit sheet.
	m, _ = send(m, key("enter"))
	if m.screen != screenEdit || m.targetID != "gemini-apikey" {
		t.Fatalf("screen = %d target = %q, want edit/gemini-apikey", m.screen, m.targetID)
	}
	// Move to "Manage models…" row and select it → screenModels.
	if !cursorTo(&m, editModel, "") {
		t.Fatal("Manage models row missing from edit sheet")
	}
	m, cmd := send(m, key("enter"))
	if m.screen != screenModels {
		t.Fatalf("screen = %d, want models", m.screen)
	}
	if !m.loading {
		t.Fatal("expected loading=true while discovering Gemini models")
	}
	if cmd == nil {
		t.Fatal("expected discover command for Gemini (models.list API)")
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
	// Provider list: 0=gemini, 1=openai, 2=my-vllm (custom). Navigate to my-vllm.
	m, _ = send(m, key("down"), key("down"))
	if m.screen != screenEditPick {
		t.Fatalf("screen = %d, want editPick", m.screen)
	}
	// Press 'x' to remove the selected custom provider (my-vllm).
	m, _ = send(m, key("x"))
	if deps.removed != "my-vllm" {
		t.Fatalf("removed = %q, want my-vllm", deps.removed)
	}
	// Should remain at the provider list.
	if m.screen != screenEditPick {
		t.Fatalf("screen = %d, want editPick after remove", m.screen)
	}
}

// gotoOpenAIEdit drives the dialog to the openai-chat edit sheet.
// The dialog opens directly at screenEditPick; openai is at index 1.
func gotoOpenAIEdit(t *testing.T, deps *fakeDeps) Model {
	t.Helper()
	m := New(context.Background(), deps)
	m, _ = send(m, key("down"), key("enter")) // editPick: move to openai (index 1), enter → screenEdit
	if m.screen != screenEdit || m.targetID != "openai" {
		t.Fatalf("not on openai edit sheet: screen=%d target=%q", m.screen, m.targetID)
	}
	return m
}

// cursorTo positions the edit-sheet cursor on the first row matching kind (and
// key when non-empty), returning false when no such row exists.
func cursorTo(m *Model, kind editKind, k string) bool {
	for i, it := range m.editItems {
		if it.kind == kind && (k == "" || it.key == k) {
			m.cursor = i
			return true
		}
	}
	return false
}

func TestPersonalityRowsCollapsedIntoPicker(t *testing.T) {
	deps := newFakeDeps()
	m := gotoOpenAIEdit(t, deps)
	for _, it := range m.editItems {
		if it.key == "personality" || it.key == "promptMode" {
			t.Fatalf("row %q should be collapsed into the System prompt picker", it.key)
		}
	}
	if !cursorTo(&m, editPreset, "") {
		t.Fatal("System prompt picker row missing")
	}
}

func TestSystemPromptPickerAppliesPreset(t *testing.T) {
	deps := newFakeDeps()
	m := gotoOpenAIEdit(t, deps)
	if !cursorTo(&m, editPreset, "") {
		t.Fatal("System prompt row missing")
	}
	m, _ = send(m, key("enter"))
	if m.screen != screenEditPicker {
		t.Fatalf("screen = %d, want picker", m.screen)
	}
	if len(m.pickerOptions) != len(config.SystemPromptPresets) {
		t.Fatalf("picker options = %d, want %d", len(m.pickerOptions), len(config.SystemPromptPresets))
	}
	// Move to the second preset and apply it.
	m, _ = send(m, key("down"), key("enter"))
	if m.screen != screenEdit {
		t.Fatalf("screen = %d, want edit after applying preset", m.screen)
	}
	want := "openai:" + config.SystemPromptPresets[1].ID
	if len(deps.appliedPwith) != 1 || deps.appliedPwith[0] != want {
		t.Fatalf("applied preset = %v, want [%s]", deps.appliedPwith, want)
	}
}

func TestEnableToolsTogglesInPlace(t *testing.T) {
	deps := newFakeDeps()
	deps.effective = map[string]map[string]string{"openai": {"enableTools": "true"}}
	m := gotoOpenAIEdit(t, deps)
	if !cursorTo(&m, editToggleBool, "enableTools") {
		t.Fatal("enableTools row missing")
	}
	m, _ = send(m, key("enter"))
	want := "openai.enableTools=false"
	if len(deps.applied) == 0 || deps.applied[len(deps.applied)-1] != want {
		t.Fatalf("applied = %v, want last %s", deps.applied, want)
	}
	if m.screen != screenEdit {
		t.Fatalf("toggle should stay on edit sheet, got %d", m.screen)
	}
}

func TestResetAllSettings(t *testing.T) {
	deps := newFakeDeps()
	m := gotoOpenAIEdit(t, deps)
	if !cursorTo(&m, editResetAll, "") {
		t.Fatal("Reset all row missing")
	}
	m, _ = send(m, key("enter"))
	if len(deps.resetIDs) != 1 || deps.resetIDs[0] != "openai" {
		t.Fatalf("resetIDs = %v, want [openai]", deps.resetIDs)
	}
}

func TestResetFieldWithR(t *testing.T) {
	deps := newFakeDeps()
	m := gotoOpenAIEdit(t, deps)
	if !cursorTo(&m, editOverride, "temperature") {
		t.Fatal("temperature row missing")
	}
	m, _ = send(m, key("r"))
	want := "openai.temperature"
	if len(deps.cleared) == 0 || deps.cleared[len(deps.cleared)-1] != want {
		t.Fatalf("cleared = %v, want last %s", deps.cleared, want)
	}
}

func TestResetSystemPromptWithRClearsBothKeys(t *testing.T) {
	deps := newFakeDeps()
	m := gotoOpenAIEdit(t, deps)
	if !cursorTo(&m, editPreset, "") {
		t.Fatal("System prompt row missing")
	}
	m, _ = send(m, key("r"))
	// Resetting the system prompt clears both personality and promptMode.
	joined := strings.Join(deps.cleared, ",")
	if !strings.Contains(joined, "openai.personality") || !strings.Contains(joined, "openai.promptMode") {
		t.Fatalf("cleared = %v, want both personality and promptMode", deps.cleared)
	}
}
