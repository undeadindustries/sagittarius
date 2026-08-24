package modelsdialog

import (
	"context"
	"strings"
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

func (f *fakeDeps) ReasoningCapabilityHint(providerID, model string) string {
	return f.settings[providerID+"/"+model+"/reasoningHint"]
}

func (f *fakeDeps) ReasoningOptions(providerID, model string) (efforts []string, defaultEffort string, known bool) {
	key := providerID + "/" + model + "/reasoningOptions"
	raw := f.settings[key]
	if raw == "" {
		return nil, "", false
	}
	// Format: "known|default|effort1,effort2" or "unknown"
	if raw == "unknown" {
		return nil, "", false
	}
	parts := strings.SplitN(raw, "|", 3)
	if len(parts) < 3 || parts[0] != "known" {
		return nil, "", false
	}
	defaultEffort = parts[1]
	if parts[2] != "" {
		efforts = strings.Split(parts[2], ",")
	}
	return efforts, defaultEffort, true
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

// TestSettingsScreenShowsReasoningCapabilityHint verifies the read-only
// capability hint line appears in the settings submenu view when Deps
// supplies one, and is absent when it does not.
func TestSettingsScreenShowsReasoningCapabilityHint(t *testing.T) {
	deps := &fakeDeps{
		entries: []ModelEntry{
			{ProviderID: "gemini-apikey", ProviderLabel: "gemini", Model: "gemini-3-pro"},
		},
		settings: map[string]string{
			"gemini-apikey/gemini-3-pro/reasoningHint": "adaptive (dynamic thinking) — decides depth per turn",
		},
	}
	m := New(context.Background(), deps)
	m, _ = m.Update(keyMsg(tea.KeyEnter))
	if m.screen != screenSetting {
		t.Fatalf("screen = %v, want screenSetting", m.screen)
	}
	view := m.View()
	if !strings.Contains(view, "Reasoning:") || !strings.Contains(view, "adaptive") {
		t.Fatalf("view missing reasoning capability hint: %s", view)
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

func openReasoningPicker(t *testing.T, deps *fakeDeps) Model {
	t.Helper()
	m := New(context.Background(), deps)
	m, _ = m.Update(keyMsg(tea.KeyEnter)) // list → settings
	// settingsMenu: temperature(0), contextLimit(1), reasoningEffort(2)
	m, _ = m.Update(keyMsg(tea.KeyDown))
	m, _ = m.Update(keyMsg(tea.KeyDown))
	m, _ = m.Update(keyMsg(tea.KeyEnter))
	if m.screen != screenPickValue {
		t.Fatalf("screen = %v, want screenPickValue", m.screen)
	}
	return m
}

func TestReasoningPickerRendersKnownOptions(t *testing.T) {
	deps := &fakeDeps{
		entries: []ModelEntry{
			{ProviderID: "openrouter", ProviderLabel: "openrouter", Model: "anthropic/claude-4"},
		},
		settings: map[string]string{
			"openrouter/anthropic/claude-4/reasoningOptions": "known|medium|low,medium,high",
		},
	}
	m := openReasoningPicker(t, deps)
	view := m.View()
	if !strings.Contains(view, "Reasoning effort for anthropic/claude-4") {
		t.Fatalf("view missing title: %s", view)
	}
	for _, want := range []string{"default (inherit)", "low", "medium (model default)", "high"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q: %s", want, view)
		}
	}
	if strings.Contains(view, "custom…") {
		t.Fatalf("known model should not show custom row: %s", view)
	}
}

func TestReasoningPickerShowsCustomWhenUnknown(t *testing.T) {
	deps := &fakeDeps{
		entries: []ModelEntry{
			{ProviderID: "local", ProviderLabel: "local", Model: "qwen3"},
		},
		settings: map[string]string{
			"local/qwen3/reasoningOptions": "unknown",
		},
	}
	m := openReasoningPicker(t, deps)
	view := m.View()
	if !strings.Contains(view, "default (inherit)") {
		t.Fatalf("view missing inherit row: %s", view)
	}
	if !strings.Contains(view, "custom…") {
		t.Fatalf("unknown model should show custom row: %s", view)
	}
}

func TestReasoningPickerSelectInheritClears(t *testing.T) {
	deps := &fakeDeps{
		entries: []ModelEntry{
			{ProviderID: "openrouter", ProviderLabel: "openrouter", Model: "anthropic/claude-4"},
		},
		settings: map[string]string{
			"openrouter/anthropic/claude-4/reasoningEffort":  "high",
			"openrouter/anthropic/claude-4/reasoningOptions": "known|medium|low,medium,high",
		},
	}
	m := openReasoningPicker(t, deps)
	m, _ = m.Update(keyMsg(tea.KeyEnter)) // select default (inherit)
	if m.screen != screenSetting {
		t.Fatalf("screen = %v, want screenSetting", m.screen)
	}
	if _, ok := deps.settings["openrouter/anthropic/claude-4/reasoningEffort"]; ok {
		t.Fatal("inherit should ClearModelSetting reasoningEffort")
	}
}

func TestReasoningPickerSelectLevelSets(t *testing.T) {
	deps := &fakeDeps{
		entries: []ModelEntry{
			{ProviderID: "openrouter", ProviderLabel: "openrouter", Model: "anthropic/claude-4"},
		},
		settings: map[string]string{
			"openrouter/anthropic/claude-4/reasoningOptions": "known|medium|low,medium,high",
		},
	}
	m := openReasoningPicker(t, deps)
	m, _ = m.Update(keyMsg(tea.KeyDown)) // skip inherit
	m, _ = m.Update(keyMsg(tea.KeyDown)) // skip low → medium
	_, _ = m.Update(keyMsg(tea.KeyEnter))
	if got := deps.settings["openrouter/anthropic/claude-4/reasoningEffort"]; got != "medium" {
		t.Fatalf("reasoningEffort = %q, want medium", got)
	}
}

func TestReasoningPickerEscReturnsToSettings(t *testing.T) {
	deps := &fakeDeps{
		entries: []ModelEntry{
			{ProviderID: "local", ProviderLabel: "local", Model: "qwen3"},
		},
		settings: map[string]string{
			"local/qwen3/reasoningOptions": "unknown",
		},
	}
	m := openReasoningPicker(t, deps)
	m, _ = m.Update(keyMsg(tea.KeyEsc))
	if m.screen != screenSetting {
		t.Fatalf("esc from picker screen = %v, want screenSetting", m.screen)
	}
	if m.Done() {
		t.Fatal("esc from picker must not close the dialog")
	}
}
