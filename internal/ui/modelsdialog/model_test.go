package modelsdialog

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

type fakeDeps struct {
	entries  []ModelEntry
	settings map[string]string // key "providerID/model/key" -> value
}

func (f *fakeDeps) ListAllActiveModels() []ModelEntry { return f.entries }

func (f *fakeDeps) GetModelSettings(providerID, model string) map[string]string {
	out := map[string]string{}
	prefix := providerID + "/" + model + "/"
	for k, v := range f.settings {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			out[k[len(prefix):]] = v
		}
	}
	return out
}

func (f *fakeDeps) SetModelSetting(_ context.Context, providerID, model, key, value string) error {
	if f.settings == nil {
		f.settings = map[string]string{}
	}
	f.settings[providerID+"/"+model+"/"+key] = value
	return nil
}

func (f *fakeDeps) ClearModelSetting(_ context.Context, providerID, model, key string) error {
	if f.settings != nil {
		delete(f.settings, providerID+"/"+model+"/"+key)
	}
	return nil
}

func keyMsg(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

// TestEscClosesFromModelList verifies Esc closes the dialog from the top-level
// model list screen.
func TestEscClosesFromModelList(t *testing.T) {
	deps := &fakeDeps{
		entries: []ModelEntry{
			{ProviderID: "gemini-apikey", ProviderLabel: "gemini", Model: "gemini-2.5-pro"},
		},
	}
	m := New(context.Background(), deps)
	m, _ = m.Update(keyMsg(tea.KeyEsc))
	if !m.Done() {
		t.Fatal("esc should close the settings editor from the model list")
	}
}

// TestEnterOpensSettingsSubmenu verifies that Enter on a model entry navigates
// to the settings submenu.
func TestEnterOpensSettingsSubmenu(t *testing.T) {
	deps := &fakeDeps{
		entries: []ModelEntry{
			{ProviderID: "gemini-apikey", ProviderLabel: "gemini", Model: "gemini-2.5-pro"},
		},
	}
	m := New(context.Background(), deps)
	if m.screen != screenList {
		t.Fatalf("initial screen = %v, want screenList", m.screen)
	}
	m, _ = m.Update(keyMsg(tea.KeyEnter))
	if m.screen != screenSetting {
		t.Fatalf("after Enter screen = %v, want screenSetting", m.screen)
	}
}

// TestNoActiveModels verifies the dialog renders an error when there are no models.
func TestNoActiveModels(t *testing.T) {
	deps := &fakeDeps{}
	m := New(context.Background(), deps)
	if m.errMsg == "" {
		t.Fatal("expected an error message when there are no active models")
	}
	if m.View() == "" {
		t.Fatal("view should render even when empty")
	}
}
