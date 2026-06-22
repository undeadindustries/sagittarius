package bubbletea

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/ui"
	"github.com/undeadindustries/sagittarius/internal/ui/modelsdialog"
	"github.com/undeadindustries/sagittarius/internal/ui/providersdialog"
)

// dialogApp is a quitApp that can also supply providers- and models-dialog
// dependencies.
type dialogApp struct {
	quitApp
	deps       providersdialog.Deps
	modelsDeps modelsdialog.Deps
}

func (d dialogApp) ProviderDialogDeps() providersdialog.Deps { return d.deps }
func (d dialogApp) ModelsDialogDeps() modelsdialog.Deps       { return d.modelsDeps }

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
	updated, _ := m.Update(streamEventMsg{event: ui.StreamEvent{Type: ui.StreamDone}})
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
