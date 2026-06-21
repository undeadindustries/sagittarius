package modelsdialog

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

type fakeDeps struct {
	active   string
	label    string
	models   []string
	current  string
	setModel map[string]string
}

func (f *fakeDeps) ActiveProviderID() string    { return f.active }
func (f *fakeDeps) ActiveProviderLabel() string { return f.label }
func (f *fakeDeps) ActiveModels(string) []string {
	return f.models
}
func (f *fakeDeps) CurrentModel(string) string { return f.current }
func (f *fakeDeps) SetModel(_ context.Context, id, model string) error {
	if f.setModel == nil {
		f.setModel = map[string]string{}
	}
	f.setModel[id] = model
	return nil
}

func keyMsg(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

func TestSelectSetsActiveModel(t *testing.T) {
	deps := &fakeDeps{
		active:  "openai",
		label:   "openai",
		models:  []string{"gpt-4o", "gpt-4o-mini", "o3"},
		current: "gpt-4o",
	}
	m := New(context.Background(), deps)
	// Cursor starts on the current model (gpt-4o, index 0).
	if m.cursor != 0 {
		t.Fatalf("cursor = %d, want 0 (current model)", m.cursor)
	}
	// Move to o3 (index 2) and select it.
	m, _ = m.Update(keyMsg(tea.KeyDown))
	m, _ = m.Update(keyMsg(tea.KeyDown))
	m, _ = m.Update(keyMsg(tea.KeyEnter))

	if deps.setModel["openai"] != "o3" {
		t.Fatalf("setModel = %v, want o3", deps.setModel)
	}
	if m.current != "o3" {
		t.Fatalf("current = %q, want o3", m.current)
	}
	if m.Done() {
		t.Fatal("selecting should not close the picker")
	}
}

func TestEscCloses(t *testing.T) {
	deps := &fakeDeps{active: "openai", label: "openai", models: []string{"gpt-4o"}}
	m := New(context.Background(), deps)
	m, _ = m.Update(keyMsg(tea.KeyEsc))
	if !m.Done() {
		t.Fatal("esc should close the picker")
	}
}

func TestNoActiveProvider(t *testing.T) {
	deps := &fakeDeps{active: ""}
	m := New(context.Background(), deps)
	if m.errMsg == "" {
		t.Fatal("expected an error message when no provider is active")
	}
	if m.View() == "" {
		t.Fatal("view should render the no-provider message")
	}
}
