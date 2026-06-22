package mcpdialog

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

type fakeDeps struct {
	servers []ServerEntry
	forms   map[string]ServerForm
	saved   []ServerForm
	removed []string
	reloads int
}

func (f *fakeDeps) ListServers() []ServerEntry { return f.servers }

func (f *fakeDeps) GetServer(name string) (ServerForm, bool) {
	form, ok := f.forms[name]
	return form, ok
}

func (f *fakeDeps) SaveServer(_ context.Context, _ string, form ServerForm) error {
	f.saved = append(f.saved, form)
	return nil
}

func (f *fakeDeps) RemoveServer(_ context.Context, name string) error {
	f.removed = append(f.removed, name)
	f.servers = nil
	return nil
}

func (f *fakeDeps) SetDisabled(_ context.Context, _ string, _ bool) error { return nil }

func (f *fakeDeps) Reload(context.Context) (string, error) {
	f.reloads++
	return "reloaded", nil
}

func keyRunes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func newFake() *fakeDeps {
	return &fakeDeps{
		servers: []ServerEntry{
			{Name: "demo", Transport: TransportStdio, Editable: true, Source: "settings"},
			{Name: "ext", Transport: TransportHTTP, Editable: false, Source: "extension"},
		},
		forms: map[string]ServerForm{
			"demo": {Name: "demo", Transport: TransportStdio, Command: "echo"},
		},
	}
}

func TestEscClosesFromList(t *testing.T) {
	m := New(context.Background(), newFake())
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !m.Done() {
		t.Fatal("esc should close the wizard from the list")
	}
}

func TestAddOpensForm(t *testing.T) {
	m := New(context.Background(), newFake())
	m, _ = m.Update(keyRunes("a"))
	if m.screen != screenForm {
		t.Fatalf("screen = %v, want screenForm", m.screen)
	}
	if !m.adding || m.form.Transport != TransportStdio {
		t.Fatalf("add form not initialized: adding=%v transport=%q", m.adding, m.form.Transport)
	}
}

func TestEnterEditsSettingsServer(t *testing.T) {
	m := New(context.Background(), newFake())
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.screen != screenForm || m.adding {
		t.Fatalf("expected edit form, got screen=%v adding=%v", m.screen, m.adding)
	}
	if m.form.Command != "echo" {
		t.Fatalf("edit form command = %q, want echo", m.form.Command)
	}
}

func TestExtensionServerNotEditable(t *testing.T) {
	m := New(context.Background(), newFake())
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown}) // move to "ext"
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.screen != screenList {
		t.Fatalf("extension server should not open an edit form; screen=%v", m.screen)
	}
}

func TestTransportToggleSwapsFields(t *testing.T) {
	m := New(context.Background(), newFake())
	m, _ = m.Update(keyRunes("a")) // add form, stdio
	// fields[1] is fTransport; move cursor to it and toggle.
	m.fieldCursor = 1
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.form.Transport != TransportHTTP {
		t.Fatalf("transport = %q, want http after toggle", m.form.Transport)
	}
	// The HTTP form should expose a URL field.
	if !containsField(m.fields, fURL) {
		t.Fatal("http transport should expose the URL field")
	}
	if containsField(m.fields, fCommand) {
		t.Fatal("http transport should not expose the command field")
	}
}

func TestDeleteFlow(t *testing.T) {
	deps := newFake()
	m := New(context.Background(), deps)
	m, _ = m.Update(keyRunes("x"))
	if m.screen != screenDelete {
		t.Fatalf("screen = %v, want screenDelete", m.screen)
	}
	m, _ = m.Update(keyRunes("y"))
	if len(deps.removed) != 1 || deps.removed[0] != "demo" {
		t.Fatalf("removed = %v, want [demo]", deps.removed)
	}
}

func TestReloadFromList(t *testing.T) {
	deps := newFake()
	m := New(context.Background(), deps)
	m, _ = m.Update(keyRunes("r"))
	if deps.reloads != 1 {
		t.Fatalf("reloads = %d, want 1", deps.reloads)
	}
}

func TestViewToolsCrossLink(t *testing.T) {
	m := New(context.Background(), newFake())
	m, _ = m.Update(keyRunes("t"))
	if !m.Done() || !m.OpenTools() {
		t.Fatal("t should close the wizard and request the /tools inventory")
	}
}

func containsField(fields []fieldID, id fieldID) bool {
	for _, f := range fields {
		if f == id {
			return true
		}
	}
	return false
}
