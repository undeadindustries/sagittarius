package modesdialog

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/undeadindustries/sagittarius/internal/config"
)

type fakeModesDeps struct {
	projectAvailable bool
	modes            []ModeEntry
	models           []ModelEntry
	setScopes        []config.SettingScope
	clearScopes      []config.SettingScope
	resetScopes      []config.SettingScope
}

func (f *fakeModesDeps) ListModes() []ModeEntry        { return f.modes }
func (f *fakeModesDeps) AllActiveModels() []ModelEntry { return f.models }
func (f *fakeModesDeps) ProjectAvailable() bool        { return f.projectAvailable }

func (f *fakeModesDeps) SetModeOverride(_ context.Context, _, _, _ string, scope config.SettingScope) error {
	f.setScopes = append(f.setScopes, scope)
	return nil
}

func (f *fakeModesDeps) ClearModeOverride(_ context.Context, _ string, scope config.SettingScope) error {
	f.clearScopes = append(f.clearScopes, scope)
	return nil
}

func (f *fakeModesDeps) ResetAllOverrides(_ context.Context, scope config.SettingScope) (string, error) {
	f.resetScopes = append(f.resetScopes, scope)
	return "reset", nil
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

func drainApply(m Model, cmd tea.Cmd) Model {
	for _, msg := range runBatchMsgs(cmd) {
		if _, ok := msg.(applyResultMsg); ok {
			m, _ = m.Update(msg)
		}
	}
	return m
}

func TestModesDialogScopeByProjectAvailability(t *testing.T) {
	tests := []struct {
		name             string
		projectAvailable bool
		wantScope        config.SettingScope
		wantScopeVisible bool
	}{
		{
			name:             "without project tier saves clear and reset to global",
			projectAvailable: false,
			wantScope:        config.ScopeGlobal,
			wantScopeVisible: false,
		},
		{
			name:             "with project tier defaults to project",
			projectAvailable: true,
			wantScope:        config.ScopeProject,
			wantScopeVisible: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := &fakeModesDeps{
				projectAvailable: tt.projectAvailable,
				modes: []ModeEntry{
					{Mode: "agent"},
					{Mode: "plan"},
				},
				models: []ModelEntry{
					{ProviderID: "gemini-apikey", DisplayID: "gemini", Model: "gemini-4-carbon"},
				},
			}
			m := NewWithOptions(context.Background(), deps, SubagentsOptions()).SetSize(80, 24)
			view := m.View()
			if gotVisible := strings.Contains(view, "Save to:"); gotVisible != tt.wantScopeVisible {
				t.Fatalf("scope bar visible = %v, want %v", gotVisible, tt.wantScopeVisible)
			}

			// 1. Set override on first row: Enter opens picker (cursor 0 is clear sentinel), Down selects model, Enter applies.
			m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
			var cmd tea.Cmd
			m, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			m = drainApply(m, cmd)

			// 2. Clear override with 'r'.
			m, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
			m = drainApply(m, cmd)

			// 3. Reset all overrides with 'R' twice.
			m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("R")})
			if !strings.Contains(m.info, tt.wantScope.String()+" settings") {
				t.Fatalf("confirm prompt = %q, want %s settings", m.info, tt.wantScope)
			}
			m, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("R")})
			_ = drainApply(m, cmd)

			if len(deps.setScopes) != 1 || deps.setScopes[0] != tt.wantScope {
				t.Fatalf("setScopes = %v, want [%v]", deps.setScopes, tt.wantScope)
			}
			if len(deps.clearScopes) != 1 || deps.clearScopes[0] != tt.wantScope {
				t.Fatalf("clearScopes = %v, want [%v]", deps.clearScopes, tt.wantScope)
			}
			if len(deps.resetScopes) != 1 || deps.resetScopes[0] != tt.wantScope {
				t.Fatalf("resetScopes = %v, want [%v]", deps.resetScopes, tt.wantScope)
			}
		})
	}
}
