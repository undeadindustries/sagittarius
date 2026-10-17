package settingsdialog

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/ui/scopedialog"
)

func manyBoolEntries(n int) []SettingEntry {
	out := make([]SettingEntry, n)
	for i := range out {
		out[i] = SettingEntry{Key: fmt.Sprintf("k%02d", i), Label: fmt.Sprintf("setting %02d", i), Kind: KindBool}
	}
	return out
}

// TestViewFitsShortTerminal is the "can't see the top of /settings" bug: the
// title and scope line must survive a terminal shorter than the list, with
// the selected row on screen wherever the cursor is.
func TestViewFitsShortTerminal(t *testing.T) {
	const height = 12
	for _, cursor := range []int{0, 39} {
		t.Run(fmt.Sprintf("cursor %d", cursor), func(t *testing.T) {
			m, _ := New(context.Background(), &fakeSettingsDeps{entries: manyBoolEntries(40), project: true})
			m = m.SetSize(80, height)
			m.cursor = cursor
			view := m.View()
			if h := lipgloss.Height(view); h > height {
				t.Fatalf("view is %d lines, terminal is %d:\n%s", h, height, view)
			}
			for _, want := range []string{"Settings", "Save to:", fmt.Sprintf("setting %02d", cursor)} {
				if !strings.Contains(view, want) {
					t.Errorf("view missing %q:\n%s", want, view)
				}
			}
		})
	}
}

func TestTabFlipsScopeAndReloads(t *testing.T) {
	deps := &fakeSettingsDeps{entries: manyBoolEntries(2), project: true}
	m, _ := New(context.Background(), deps)
	if m.scopeSel.Scope != config.ScopeGlobal {
		t.Fatalf("settings opened on %v, want Global", m.scopeSel.Scope)
	}
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.scopeSel.Scope != config.ScopeProject || cmd == nil {
		t.Fatalf("Tab: scope %v, cmd nil=%v; want Project and a reload command", m.scopeSel.Scope, cmd == nil)
	}
	msg, ok := cmd().(scopedialog.ScopeChangedMsg)
	if !ok {
		t.Fatalf("Tab command produced %T, want ScopeChangedMsg", msg)
	}
	m, _ = m.Update(msg)
	if last := deps.listedScopes[len(deps.listedScopes)-1]; last != config.ScopeProject {
		t.Errorf("rows reloaded for %v, want Project", last)
	}
}
