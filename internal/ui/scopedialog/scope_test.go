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

func TestScopeSelector_RightArrowSwitchesToProject(t *testing.T) {
	s := NewScopeSelector(config.ScopeGlobal)
	s.Focused = true

	s2, cmd := s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	if s2.Scope != config.ScopeProject {
		t.Fatalf("scope after 'l' = %v, want ScopeProject", s2.Scope)
	}
	if cmd == nil {
		t.Fatal("expected ScopeChangedMsg command")
	}
	msg := cmd()
	if changed, ok := msg.(ScopeChangedMsg); !ok || changed.Scope != config.ScopeProject {
		t.Fatalf("cmd() = %T %v, want ScopeChangedMsg{ScopeProject}", msg, msg)
	}
}

func TestScopeSelector_NotFocusedIgnoresKeys(t *testing.T) {
	s := NewScopeSelector(config.ScopeGlobal)
	s.Focused = false

	s2, cmd := s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	if s2.Scope != config.ScopeGlobal {
		t.Fatalf("unfocused selector should not change scope")
	}
	if cmd != nil {
		t.Fatal("unfocused selector should not emit commands")
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

func TestScopeSelector_FocusedIndicator(t *testing.T) {
	s := NewScopeSelector(config.ScopeGlobal)
	s.Focused = true
	view := s.View(testTheme)
	// Focused view should contain the "›" indicator.
	if !strings.Contains(view, "›") {
		t.Fatalf("focused view missing indicator '›': %q", view)
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
