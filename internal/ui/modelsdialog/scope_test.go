package modelsdialog

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// press delivers k and then any message its command produces, the way the
// Bubble Tea runtime would.
func press(m Model, k tea.KeyMsg) Model {
	m, cmd := m.Update(k)
	if cmd != nil {
		m, _ = m.Update(cmd())
	}
	return m
}

func TestProjectScopeEditsProjectFile(t *testing.T) {
	deps := &fakeDeps{
		project:  true,
		entries:  []ModelEntry{{ProviderID: "openai", ProviderLabel: "openai", Model: "gpt-4o"}},
		settings: map[string]string{"openai/gpt-4o/temperature": "0.7"},
	}
	m := New(context.Background(), deps).SetSize(100, 40)
	m = press(m, keyMsg(tea.KeyEnter))
	m = press(m, tea.KeyMsg{Type: tea.KeyTab})

	view := m.View()
	if !strings.Contains(view, "(global: 0.7)") {
		t.Errorf("project view should show the inherited global value:\n%s", view)
	}

	m.cursor = 1 // contextLimit
	m = press(m, keyMsg(tea.KeyEnter))
	m.input.SetValue("32000")
	m = press(m, keyMsg(tea.KeyEnter))
	if got := deps.projectSettings["openai/gpt-4o/contextLimit"]; got != "32000" {
		t.Errorf("project contextLimit = %q, want 32000", got)
	}
	if _, leaked := deps.settings["openai/gpt-4o/contextLimit"]; leaked {
		t.Error("project-scope edit was written to the global file")
	}
}

func TestViewFitsShortTerminal(t *testing.T) {
	entries := make([]ModelEntry, 40)
	for i := range entries {
		entries[i] = ModelEntry{ProviderID: "openai", ProviderLabel: "openai", Model: "model-" + string(rune('a'+i%26))}
	}
	const height = 12
	for _, cursor := range []int{0, 39} {
		m := New(context.Background(), &fakeDeps{project: true, entries: entries}).SetSize(80, height)
		m.cursor = cursor
		view := m.View()
		if got := strings.Count(view, "\n") + 1; got > height {
			t.Errorf("cursor %d: view is %d lines, want <= %d", cursor, got, height)
		}
		for _, want := range []string{"Model Settings", "Save to:", "Esc close"} {
			if !strings.Contains(view, want) {
				t.Errorf("cursor %d: view lost %q:\n%s", cursor, want, view)
			}
		}
	}
}
