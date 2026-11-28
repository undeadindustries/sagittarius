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

func TestNewScopeSelectorFor(t *testing.T) {
	tests := []struct {
		name             string
		defaultScope     config.SettingScope
		projectAvailable bool
		wantScope        config.SettingScope
		wantDisabled     bool
	}{
		{"project default with project tier", config.ScopeProject, true, config.ScopeProject, false},
		{"project default without project tier", config.ScopeProject, false, config.ScopeGlobal, true},
		{"global default with project tier", config.ScopeGlobal, true, config.ScopeGlobal, false},
		{"global default without project tier", config.ScopeGlobal, false, config.ScopeGlobal, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewScopeSelectorFor(tt.defaultScope, tt.projectAvailable)
			if s.Scope != tt.wantScope || s.EffectiveScope() != tt.wantScope {
				t.Fatalf("scope = (%v, effective=%v), want %v", s.Scope, s.EffectiveScope(), tt.wantScope)
			}
			if s.Disabled != tt.wantDisabled {
				t.Fatalf("Disabled = %v, want %v", s.Disabled, tt.wantDisabled)
			}
		})
	}
}

func TestScopeSelector_WithScopeAndEffectiveScope(t *testing.T) {
	disabled := NewScopeSelectorFor(config.ScopeProject, false)
	afterReset := disabled.WithScope(config.ScopeProject)
	if afterReset.Scope != config.ScopeGlobal || afterReset.EffectiveScope() != config.ScopeGlobal {
		t.Fatalf("disabled WithScope(ScopeProject) = (%v, effective=%v), want ScopeGlobal", afterReset.Scope, afterReset.EffectiveScope())
	}

	// Even if a caller mutates Disabled directly on a ScopeProject selector,
	// EffectiveScope and Toggle coerce back to ScopeGlobal.
	manual := ScopeSelector{Scope: config.ScopeProject, Disabled: true}
	if got := manual.EffectiveScope(); got != config.ScopeGlobal {
		t.Fatalf("manual disabled EffectiveScope() = %v, want ScopeGlobal", got)
	}
	toggled, cmd := manual.Toggle()
	if toggled.Scope != config.ScopeGlobal || cmd != nil {
		t.Fatalf("manual disabled Toggle() = (%v, cmd=%v), want (ScopeGlobal, nil)", toggled.Scope, cmd)
	}

	enabled := NewScopeSelectorFor(config.ScopeGlobal, true).WithScope(config.ScopeProject)
	if enabled.Scope != config.ScopeProject || enabled.EffectiveScope() != config.ScopeProject {
		t.Fatalf("enabled WithScope(ScopeProject) = (%v, effective=%v), want ScopeProject", enabled.Scope, enabled.EffectiveScope())
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
