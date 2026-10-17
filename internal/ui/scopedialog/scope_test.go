package scopedialog

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/ui/theme"
)

var testTheme = theme.Default()

func TestNewScopeSelector_DefaultGlobal(t *testing.T) {
	s := NewScopeSelector(config.ScopeGlobal)
	if s.Scope != config.ScopeGlobal {
		t.Fatalf("default scope = %v, want ScopeGlobal", s.Scope)
	}
}

func TestScopeSelector_HandleKey(t *testing.T) {
	tab := tea.KeyMsg{Type: tea.KeyTab}
	other := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")}
	tests := []struct {
		name      string
		start     config.SettingScope
		disabled  bool
		key       tea.KeyMsg
		want      config.SettingScope
		wantTaken bool
	}{
		{"tab global to project", config.ScopeGlobal, false, tab, config.ScopeProject, true},
		{"tab project to global", config.ScopeProject, false, tab, config.ScopeGlobal, true},
		{"other key ignored", config.ScopeGlobal, false, other, config.ScopeGlobal, false},
		{"disabled ignores tab", config.ScopeGlobal, true, tab, config.ScopeGlobal, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewScopeSelector(tt.start)
			s.Disabled = tt.disabled
			got, cmd, taken := s.HandleKey(tt.key)
			if got.Scope != tt.want || taken != tt.wantTaken {
				t.Fatalf("HandleKey = (%v, taken=%v), want (%v, %v)", got.Scope, taken, tt.want, tt.wantTaken)
			}
			if !tt.wantTaken {
				if cmd != nil {
					t.Fatal("unhandled key returned a command")
				}
				return
			}
			if msg, ok := cmd().(ScopeChangedMsg); !ok || msg.Scope != tt.want {
				t.Fatalf("cmd() = %v, want ScopeChangedMsg{%v}", msg, tt.want)
			}
		})
	}
}

func TestScopeSelector_DisabledHides(t *testing.T) {
	s := NewScopeSelector(config.ScopeProject)
	s.Disabled = true

	if s.View(testTheme) != "" {
		t.Fatal("disabled selector should render empty string")
	}
	if s.Rows() != 0 {
		t.Fatalf("disabled selector Rows() = %d, want 0", s.Rows())
	}
}

func TestScopeSelector_ViewContainsLabels(t *testing.T) {
	s := NewScopeSelector(config.ScopeGlobal)
	view := s.View(testTheme)
	if !strings.Contains(view, "Global") {
		t.Fatalf("view does not contain 'Global': %q", view)
	}
	if !strings.Contains(view, "Project") {
		t.Fatalf("view does not contain 'Project': %q", view)
	}
}

func TestScopeSelector_ViewNamesToggleKey(t *testing.T) {
	view := NewScopeSelector(config.ScopeGlobal).View(testTheme)
	if !strings.Contains(view, "Tab switches") {
		t.Fatalf("view does not say how to switch: %q", view)
	}
}

func TestScopeBadge_Project(t *testing.T) {
	badge := ScopeBadge(config.ScopeProject, false, testTheme)
	if !strings.Contains(badge, "project") {
		t.Fatalf("project badge = %q, want 'project'", badge)
	}
}

func TestScopeBadge_GlobalHidden(t *testing.T) {
	badge := ScopeBadge(config.ScopeGlobal, false, testTheme)
	if badge != "" {
		t.Fatalf("global badge with showGlobal=false should be empty, got %q", badge)
	}
}

func TestScopeBadge_GlobalShown(t *testing.T) {
	badge := ScopeBadge(config.ScopeGlobal, true, testTheme)
	if !strings.Contains(badge, "global") {
		t.Fatalf("global badge with showGlobal=true = %q, want 'global'", badge)
	}
}

func TestScopeHint_Empty(t *testing.T) {
	if got := ScopeHint("", testTheme); got != "" {
		t.Fatalf("empty hint should return empty string, got %q", got)
	}
}

func TestScopeHint_NonEmpty(t *testing.T) {
	hint := ScopeHint("(Inherited from Global)", testTheme)
	if !strings.Contains(hint, "Inherited from Global") {
		t.Fatalf("hint = %q, want 'Inherited from Global'", hint)
	}
}

func TestSaveToLabel_Project(t *testing.T) {
	label := SaveToLabel(config.ScopeProject, "~/.sag/settings.json", ".sag/settings.json")
	if !strings.Contains(label, ".sag/settings.json") || !strings.Contains(label, "Saving") {
		t.Fatalf("project label = %q", label)
	}
}

func TestSaveToLabel_Global(t *testing.T) {
	label := SaveToLabel(config.ScopeGlobal, "~/.sag/settings.json", "")
	if !strings.Contains(label, "~/.sag/settings.json") {
		t.Fatalf("global label = %q", label)
	}
}
