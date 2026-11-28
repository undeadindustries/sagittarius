package modelpickdialog

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/undeadindustries/sagittarius/internal/config"
)

type fakeModelPickDeps struct {
	projectAvailable bool
	entries          []ModelEntry
	curProvider      string
	curModel         string
	selectedProvider string
	selectedModel    string
	selectedScope    config.SettingScope
	selectCalls      int
}

func (f *fakeModelPickDeps) AllActiveModels() []ModelEntry { return f.entries }
func (f *fakeModelPickDeps) CurrentProviderID() string     { return f.curProvider }
func (f *fakeModelPickDeps) CurrentModel() string          { return f.curModel }
func (f *fakeModelPickDeps) ProjectAvailable() bool        { return f.projectAvailable }
func (f *fakeModelPickDeps) ContextFitNotice() string      { return "" }
func (f *fakeModelPickDeps) ModelSwitchWarning(string, string) string {
	return ""
}

func (f *fakeModelPickDeps) SelectCurrentModel(_ context.Context, providerID, model string, scope config.SettingScope) error {
	f.selectCalls++
	f.selectedProvider = providerID
	f.selectedModel = model
	f.selectedScope = scope
	return nil
}

func runBatchMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, sub := range batch {
			out = append(out, runBatchMsgs(sub)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func TestModelPickScopeByProjectAvailability(t *testing.T) {
	tests := []struct {
		name             string
		projectAvailable bool
		pressTab         bool
		wantScope        config.SettingScope
		wantScopeVisible bool
	}{
		{
			name:             "home directory hides scope bar and saves global",
			projectAvailable: false,
			pressTab:         true, // Tab must be ignored when disabled
			wantScope:        config.ScopeGlobal,
			wantScopeVisible: false,
		},
		{
			name:             "project directory defaults to project scope",
			projectAvailable: true,
			pressTab:         false,
			wantScope:        config.ScopeProject,
			wantScopeVisible: true,
		},
		{
			name:             "project directory Tab switches to global scope",
			projectAvailable: true,
			pressTab:         true,
			wantScope:        config.ScopeGlobal,
			wantScopeVisible: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := &fakeModelPickDeps{
				projectAvailable: tt.projectAvailable,
				curProvider:      "gemini-apikey",
				curModel:         "gemini-flash-latest",
				entries: []ModelEntry{
					{ProviderID: "gemini-apikey", DisplayID: "gemini", Model: "gemini-4-neon"},
					{ProviderID: "gemini-apikey", DisplayID: "gemini", Model: "gemini-flash-latest"},
				},
			}
			m := New(context.Background(), deps).SetSize(80, 24)
			view := m.View()
			if gotVisible := strings.Contains(view, "Save to:"); gotVisible != tt.wantScopeVisible {
				t.Fatalf("scope bar visible = %v, want %v; view:\n%s", gotVisible, tt.wantScopeVisible, view)
			}

			if tt.pressTab {
				var cmd tea.Cmd
				m, cmd = m.Update(tea.KeyMsg{Type: tea.KeyTab})
				for _, msg := range runBatchMsgs(cmd) {
					m, _ = m.Update(msg)
				}
			}

			// Move from gemini-flash-latest (index 1) up to gemini-4-neon (index 0) and press Enter.
			m, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
			var cmd tea.Cmd
			m, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			for _, msg := range runBatchMsgs(cmd) {
				if _, ok := msg.(applyResultMsg); ok {
					m, _ = m.Update(msg)
				}
			}

			if deps.selectCalls != 1 {
				t.Fatalf("SelectCurrentModel calls = %d, want 1", deps.selectCalls)
			}
			if deps.selectedScope != tt.wantScope {
				t.Fatalf("selectedScope = %v, want %v", deps.selectedScope, tt.wantScope)
			}
			if deps.selectedModel != "gemini-4-neon" {
				t.Fatalf("selectedModel = %q, want gemini-4-neon", deps.selectedModel)
			}
			if m.errMsg != "" {
				t.Fatalf("unexpected errMsg = %q", m.errMsg)
			}
		})
	}
}
