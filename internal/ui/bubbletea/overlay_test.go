package bubbletea

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/ui"
	"github.com/undeadindustries/sagittarius/internal/ui/mcpdialog"
	"github.com/undeadindustries/sagittarius/internal/ui/modelsdialog"
	"github.com/undeadindustries/sagittarius/internal/ui/providersdialog"
	"github.com/undeadindustries/sagittarius/internal/ui/toolsdialog"
)

// dialogApp is a quitApp that can also supply providers-, models-, MCP-, and
// tools-dialog dependencies.
type dialogApp struct {
	quitApp
	deps       providersdialog.Deps
	modelsDeps modelsdialog.Deps
}

func (d dialogApp) ProviderDialogDeps() providersdialog.Deps { return d.deps }
func (d dialogApp) ModelsDialogDeps() modelsdialog.Deps      { return d.modelsDeps }
func (d dialogApp) MCPDialogDeps() mcpdialog.Deps            { return stubMCPDeps{} }
func (d dialogApp) ToolsDialogDeps() toolsdialog.Deps        { return stubToolsDeps{} }

type stubMCPDeps struct{}

func (stubMCPDeps) ListServers() []mcpdialog.ServerEntry { return nil }
func (stubMCPDeps) GetServer(string) (mcpdialog.ServerForm, bool) {
	return mcpdialog.ServerForm{}, false
}
func (stubMCPDeps) SaveServer(context.Context, string, mcpdialog.ServerForm) error { return nil }
func (stubMCPDeps) RemoveServer(context.Context, string) error                     { return nil }
func (stubMCPDeps) SetDisabled(context.Context, string, bool) error                { return nil }
func (stubMCPDeps) Reload(context.Context) (string, error)                         { return "", nil }

type stubToolsDeps struct{}

func (stubToolsDeps) BuiltinTools() []toolsdialog.BuiltinTool                    { return nil }
func (stubToolsDeps) ServerTools(context.Context) []toolsdialog.ServerGroup      { return nil }
func (stubToolsDeps) SetToolEnabled(context.Context, string, string, bool) error { return nil }
func (stubToolsDeps) ReloadTools(context.Context) error                          { return nil }

type stubModelsDeps struct{}

func (stubModelsDeps) ListAllActiveModels() []modelsdialog.ModelEntry {
	return []modelsdialog.ModelEntry{
		{ProviderID: "openai", ProviderLabel: "openai", Model: "gpt-4o"},
		{ProviderID: "openai", ProviderLabel: "openai", Model: "gpt-4o-mini"},
	}
}
func (stubModelsDeps) GetModelSettings(string, string) map[string]string { return nil }
func (stubModelsDeps) SetModelSetting(context.Context, string, string, string, string) error {
	return nil
}
func (stubModelsDeps) ClearModelSetting(context.Context, string, string, string) error { return nil }

type stubDialogDeps struct{}

func (stubDialogDeps) ListProviders() []providersdialog.ProviderEntry {
	return []providersdialog.ProviderEntry{
		{ID: "openai", DisplayID: "openai", DisplayName: "OpenAI", WireFormat: config.WireFormatOpenAIChat, IsActive: true},
	}
}
func (stubDialogDeps) ActiveProviderID() string                        { return "openai" }
func (stubDialogDeps) SwitchProvider(context.Context, string) error    { return nil }
func (stubDialogDeps) SetAPIKey(context.Context, string, string) error { return nil }
func (stubDialogDeps) AddCustomProvider(context.Context, string, config.CustomProviderDefinition, string) error {
	return nil
}
func (stubDialogDeps) RemoveCustomProvider(context.Context, string) error { return nil }
func (stubDialogDeps) DiscoverModels(context.Context, string) ([]string, error) {
	return nil, nil
}
func (stubDialogDeps) SetModel(context.Context, string, string) error             { return nil }
func (stubDialogDeps) CurrentModel(string) string                                 { return "" }
func (stubDialogDeps) ApplySetting(context.Context, string, string, string) error { return nil }
func (stubDialogDeps) UpdateCustomDefinition(context.Context, string, string, string) error {
	return nil
}
func (stubDialogDeps) ProviderSettings(string) map[string]string { return map[string]string{} }
func (stubDialogDeps) ValidSettingKeys(string) []string {
	return config.ValidSettingKeys(config.WireFormatOpenAIChat)
}
func (stubDialogDeps) ActiveModels(string) []string { return nil }
func (stubDialogDeps) SetActiveModels(context.Context, string, []string) error {
	return nil
}
func (stubDialogDeps) EffectiveProviderSettings(string) map[string]string {
	return map[string]string{}
}
func (stubDialogDeps) SystemPromptPresetID(string) string { return "" }
func (stubDialogDeps) ApplySystemPromptPreset(context.Context, string, string) (string, error) {
	return "", nil
}
func (stubDialogDeps) ClearSetting(context.Context, string, string) error { return nil }
func (stubDialogDeps) ResetSettings(context.Context, string) error        { return nil }
func (stubDialogDeps) GenerateProviderID(string) string                   { return "" }

func newDialogModel() *model {
	app := dialogApp{deps: stubDialogDeps{}, modelsDeps: stubModelsDeps{}}
	m := newModel(ui.Options{}, app, NewTerminal(ui.Options{}))
	m.ctx = context.Background()
	m.width = 80
	m.height = 24
	return m
}

func TestOpenDialogEventOpensOverlay(t *testing.T) {
	t.Parallel()
	m := newDialogModel()
	m.handleStream(ui.StreamEvent{Type: ui.StreamOpenDialog, Dialog: ui.DialogProviders})
	if m.overlay == nil {
		t.Fatal("expected overlay to be opened by StreamOpenDialog")
	}
	if m.View() == "" {
		t.Fatal("overlay view should render")
	}
}

func TestOverlayEscClosesAndRestoresInput(t *testing.T) {
	t.Parallel()
	m := newDialogModel()
	m.handleStream(ui.StreamEvent{Type: ui.StreamOpenDialog, Dialog: ui.DialogProviders})
	if m.overlay == nil {
		t.Fatal("overlay not opened")
	}
	// Esc at the menu closes the dialog.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	mm := updated.(*model)
	if mm.overlay != nil {
		t.Fatal("overlay should be cleared after esc at menu")
	}
}

func TestOverlayRoutesStreamDone(t *testing.T) {
	t.Parallel()
	m := newDialogModel()
	m.handleStream(ui.StreamEvent{Type: ui.StreamOpenDialog, Dialog: ui.DialogProviders})
	// A StreamDone arriving while the overlay is open must still reset busy
	// state rather than being swallowed by the overlay.
	m.busy = true
	m.activeStreamGen = 1
	updated, _ := m.Update(streamEventMsg{gen: 1, event: ui.StreamEvent{Type: ui.StreamDone}})
	mm := updated.(*model)
	if mm.busy {
		t.Fatal("StreamDone should clear busy even with overlay open")
	}
	if mm.overlay == nil {
		t.Fatal("overlay should remain open after StreamDone")
	}
}

func TestOpenDialogUnavailableWithoutHost(t *testing.T) {
	t.Parallel()
	// quitApp does not implement providerDialogHost.
	m := newModel(ui.Options{}, quitApp{}, NewTerminal(ui.Options{}))
	m.ctx = context.Background()
	m.handleStream(ui.StreamEvent{Type: ui.StreamOpenDialog, Dialog: ui.DialogProviders})
	if m.overlay != nil {
		t.Fatal("overlay must not open without a dialog host")
	}
}

func TestOpenModelsDialogOpensOverlay(t *testing.T) {
	t.Parallel()
	m := newDialogModel()
	m.handleStream(ui.StreamEvent{Type: ui.StreamOpenDialog, Dialog: ui.DialogModels})
	if m.modelsOverlay == nil {
		t.Fatal("expected models overlay to be opened by StreamOpenDialog")
	}
	if m.overlay != nil {
		t.Fatal("providers overlay must not open for DialogModels")
	}
	if m.View() == "" {
		t.Fatal("models overlay view should render")
	}
}

func TestModelsOverlayEscCloses(t *testing.T) {
	t.Parallel()
	m := newDialogModel()
	m.handleStream(ui.StreamEvent{Type: ui.StreamOpenDialog, Dialog: ui.DialogModels})
	if m.modelsOverlay == nil {
		t.Fatal("models overlay not opened")
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	mm := updated.(*model)
	if mm.modelsOverlay != nil {
		t.Fatal("models overlay should be cleared after esc")
	}
}

func TestModelsDialogUnavailableWithoutHost(t *testing.T) {
	t.Parallel()
	m := newModel(ui.Options{}, quitApp{}, NewTerminal(ui.Options{}))
	m.ctx = context.Background()
	m.handleStream(ui.StreamEvent{Type: ui.StreamOpenDialog, Dialog: ui.DialogModels})
	if m.modelsOverlay != nil {
		t.Fatal("models overlay must not open without a dialog host")
	}
}

func TestOpenMCPDialogOpensOverlay(t *testing.T) {
	t.Parallel()
	m := newDialogModel()
	m.handleStream(ui.StreamEvent{Type: ui.StreamOpenDialog, Dialog: ui.DialogMCP})
	if m.mcpOverlay == nil {
		t.Fatal("expected MCP overlay to be opened by StreamOpenDialog")
	}
	if m.View() == "" {
		t.Fatal("MCP overlay view should render")
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.(*model).mcpOverlay != nil {
		t.Fatal("MCP overlay should be cleared after esc")
	}
}

func TestOpenToolsDialogOpensOverlay(t *testing.T) {
	t.Parallel()
	m := newDialogModel()
	m.handleStream(ui.StreamEvent{Type: ui.StreamOpenDialog, Dialog: ui.DialogTools})
	if m.toolsOverlay == nil {
		t.Fatal("expected tools overlay to be opened by StreamOpenDialog")
	}
	if m.View() == "" {
		t.Fatal("tools overlay view should render")
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.(*model).toolsOverlay != nil {
		t.Fatal("tools overlay should be cleared after esc")
	}
}

func TestMCPAndToolsDialogsUnavailableWithoutHost(t *testing.T) {
	t.Parallel()
	m := newModel(ui.Options{}, quitApp{}, NewTerminal(ui.Options{}))
	m.ctx = context.Background()
	m.handleStream(ui.StreamEvent{Type: ui.StreamOpenDialog, Dialog: ui.DialogMCP})
	m.handleStream(ui.StreamEvent{Type: ui.StreamOpenDialog, Dialog: ui.DialogTools})
	if m.mcpOverlay != nil || m.toolsOverlay != nil {
		t.Fatal("MCP/tools overlays must not open without a dialog host")
	}
}
