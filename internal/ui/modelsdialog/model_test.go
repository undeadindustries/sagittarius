package modelsdialog

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

type selected struct {
	provider string
	model    string
}

type fakeDeps struct {
	entries  []ModelEntry
	active   string
	current  string
	selected *selected
}

func (f *fakeDeps) ListActiveModels() []ModelEntry { return f.entries }
func (f *fakeDeps) ActiveProviderID() string        { return f.active }
func (f *fakeDeps) CurrentModel() string            { return f.current }
func (f *fakeDeps) SelectModel(_ context.Context, providerID, model string) error {
	f.selected = &selected{provider: providerID, model: model}
	return nil
}

func keyMsg(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

func TestSelectSwitchesProviderAndModel(t *testing.T) {
	deps := &fakeDeps{
		entries: []ModelEntry{
			{ProviderID: "gemini-apikey", ProviderLabel: "gemini", Model: "gemini-2.5-pro"},
			{ProviderID: "openai", ProviderLabel: "openai", Model: "gpt-4o"},
			{ProviderID: "openrouter", ProviderLabel: "openrouter", Model: "google/gemma-3"},
		},
		active:  "gemini-apikey",
		current: "gemini-2.5-pro",
	}
	m := New(context.Background(), deps)
	// Cursor starts on the current entry (gemini/gemini-2.5-pro, index 0).
	if m.cursor != 0 {
		t.Fatalf("cursor = %d, want 0 (current entry)", m.cursor)
	}
	// Move to openrouter/google/gemma-3 (index 2) and select it.
	m, _ = m.Update(keyMsg(tea.KeyDown))
	m, _ = m.Update(keyMsg(tea.KeyDown))
	m, _ = m.Update(keyMsg(tea.KeyEnter))

	if deps.selected == nil {
		t.Fatal("SelectModel was not called")
	}
	if deps.selected.provider != "openrouter" || deps.selected.model != "google/gemma-3" {
		t.Fatalf("selected = %+v, want openrouter/google/gemma-3", deps.selected)
	}
	if m.curProvider != "openrouter" || m.curModel != "google/gemma-3" {
		t.Fatalf("current = %s/%s, want openrouter/google/gemma-3", m.curProvider, m.curModel)
	}
	if m.Done() {
		t.Fatal("selecting should not close the picker")
	}
}

func TestEscCloses(t *testing.T) {
	deps := &fakeDeps{
		entries: []ModelEntry{{ProviderID: "openai", ProviderLabel: "openai", Model: "gpt-4o"}},
		active:  "openai",
		current: "gpt-4o",
	}
	m := New(context.Background(), deps)
	m, _ = m.Update(keyMsg(tea.KeyEsc))
	if !m.Done() {
		t.Fatal("esc should close the picker")
	}
}

func TestNoActiveModels(t *testing.T) {
	deps := &fakeDeps{}
	m := New(context.Background(), deps)
	if m.errMsg == "" {
		t.Fatal("expected an error message when there are no active models")
	}
	if m.View() == "" {
		t.Fatal("view should render the no-models message")
	}
}
