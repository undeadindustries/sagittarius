package providersdialog

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/undeadindustries/sagittarius/internal/config"
)

var tabKey = tea.KeyMsg{Type: tea.KeyTab}

// openEditSheet opens the edit sheet for the provider at index i.
func openEditSheet(t *testing.T, m Model, i int) Model {
	t.Helper()
	for range i {
		m, _ = send(m, key("down"))
	}
	m, _ = send(m, key("enter"))
	if m.screen != screenEdit {
		t.Fatalf("screen = %v, want edit sheet", m.screen)
	}
	return m
}

func TestScopeToggleOnlyWithProjectFile(t *testing.T) {
	tests := []struct {
		name    string
		project bool
		want    config.SettingScope
	}{
		{name: "project file available", project: true, want: config.ScopeProject},
		{name: "no project file", project: false, want: config.ScopeGlobal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := newFakeDeps()
			deps.project = tt.project
			m := New(context.Background(), deps)
			if m.scope() != config.ScopeGlobal {
				t.Fatalf("initial scope = %v, want global", m.scope())
			}
			m, _ = send(m, tabKey)
			if m.scope() != tt.want {
				t.Errorf("scope after Tab = %v, want %v", m.scope(), tt.want)
			}
			if got := strings.Contains(m.View(), "Save to:"); got != tt.project {
				t.Errorf("scope line shown = %v, want %v", got, tt.project)
			}
		})
	}
}

func TestProjectScopeWritesCarryScope(t *testing.T) {
	deps := newFakeDeps()
	deps.project = true
	m := New(context.Background(), deps)
	m, _ = send(m, tabKey)
	m = openEditSheet(t, m, 1) // openai

	idx := -1
	for i, item := range m.editItems {
		if item.kind == editToggleBool {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("no toggle row on the openai edit sheet")
	}
	m.cursor = idx
	m, _ = send(m, key("enter"))
	if len(deps.scopes) != 1 || deps.scopes[0] != config.ScopeProject {
		t.Fatalf("write scopes = %v, want [project]", deps.scopes)
	}
}

func TestEditSheetLabelsScopeSources(t *testing.T) {
	deps := newFakeDeps()
	deps.project = true
	deps.settingsByID = map[string]map[string]string{"openai": {"temperature": "0.7"}}
	deps.projectByID = map[string]map[string]string{"openai": {"contextLimit": "32000"}}
	m := New(context.Background(), deps).SetSize(120, 60)
	m, _ = send(m, tabKey)
	m = openEditSheet(t, m, 1)

	view := m.View()
	for _, want := range []string{"(global: 0.7)", "= 32000", "[global only]", "always save to the global file"} {
		if !strings.Contains(view, want) {
			t.Errorf("project-scope edit sheet missing %q:\n%s", want, view)
		}
	}

	m, _ = send(m, tabKey)
	view = m.View()
	if strings.Contains(view, "[global only]") {
		t.Errorf("global-scope edit sheet still tags global-only rows:\n%s", view)
	}
	if !strings.Contains(view, "= 0.7") {
		t.Errorf("global-scope edit sheet missing the global value:\n%s", view)
	}
}

func TestTabTypesIntoTextEntry(t *testing.T) {
	deps := newFakeDeps()
	deps.project = true
	m := New(context.Background(), deps)
	m = openEditSheet(t, m, 1)
	m, _ = send(m, key("enter")) // "Set API key" opens the secret entry
	if m.screen != screenSetKey {
		t.Fatalf("screen = %v, want set-key entry", m.screen)
	}
	m, _ = send(m, tabKey)
	if m.scope() != config.ScopeGlobal {
		t.Errorf("Tab on a text entry switched scope to %v", m.scope())
	}
}

func TestEditSheetFitsShortTerminalWithScopeLine(t *testing.T) {
	const height = 14
	deps := newFakeDeps()
	deps.project = true
	m := New(context.Background(), deps).SetSize(80, height)
	m, _ = send(m, tabKey)
	m = openEditSheet(t, m, 1)
	for range len(m.editItems) {
		view := m.View()
		if got := strings.Count(view, "\n") + 1; got > height {
			t.Fatalf("cursor %d: view is %d lines, want <= %d:\n%s", m.cursor, got, height, view)
		}
		if !strings.Contains(view, "Providers") || !strings.Contains(view, "Save to:") {
			t.Fatalf("cursor %d: title or scope line scrolled away:\n%s", m.cursor, view)
		}
		m, _ = send(m, key("down"))
	}
}
