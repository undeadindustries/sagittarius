// Package scopedialog provides reusable UI primitives for dual-scope settings
// (Global ~/.sagittarius vs Project .sagittarius/) across overlay dialogs.
//
// The three exports are:
//   - ScopeSelector: a keyboard-navigable radio widget for choosing where to save.
//   - ScopeBadge: renders a dim "[global]" or "[project]" label suffix for list rows.
//   - ScopeHint: renders a one-line dim hint (e.g. "Inherited from Global").
//
// These primitives use lipgloss for rendering and are safe to embed in any
// Bubble Tea overlay model. They do not import bubbletea themselves — they
// receive tea.Msg in Update and return (ScopeSelector, tea.Cmd) so the parent
// model controls focus.
package scopedialog

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/ui/theme"
)

// ScopeChangedMsg is emitted by ScopeSelector when the user changes the scope.
// The parent model can listen for this to update its save target.
type ScopeChangedMsg struct {
	Scope config.SettingScope
}

// ScopeSelector is a keyboard-navigable two-option radio for choosing
// config.ScopeGlobal vs config.ScopeProject. Embed it in an overlay model
// and call Update / View as part of the parent's update/render cycle.
type ScopeSelector struct {
	Scope   config.SettingScope
	Focused bool
	// GlobalLabel is the text shown for the global option. Defaults to
	// "Global (~/.sagittarius)".
	GlobalLabel string
	// ProjectLabel is the text shown for the project option. Defaults to
	// "Project (.sagittarius/)".
	ProjectLabel string
	// Disabled hides the selector entirely when there is no project file (e.g.
	// no working directory or home directory is the workspace).
	Disabled bool
}

// NewScopeSelector returns a ScopeSelector with the given default scope and
// standard labels.
func NewScopeSelector(defaultScope config.SettingScope) ScopeSelector {
	return ScopeSelector{
		Scope:        defaultScope,
		GlobalLabel:  "Global (~/.sagittarius)",
		ProjectLabel: "Project (.sagittarius/)",
	}
}

// Update handles keyboard input when the selector is focused. The parent is
// responsible for focusing/unfocusing via Focus/Blur based on Tab navigation.
// Returns the updated selector and an optional command (ScopeChangedMsg).
func (s ScopeSelector) Update(msg tea.Msg) (ScopeSelector, tea.Cmd) {
	if !s.Focused || s.Disabled {
		return s, nil
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return s, nil
	}
	switch km.String() {
	case "left", "h":
		if s.Scope != config.ScopeGlobal {
			s.Scope = config.ScopeGlobal
			return s, func() tea.Msg { return ScopeChangedMsg{Scope: s.Scope} }
		}
	case "right", "l", " ":
		if s.Scope != config.ScopeProject {
			s.Scope = config.ScopeProject
			return s, func() tea.Msg { return ScopeChangedMsg{Scope: s.Scope} }
		}
	}
	return s, nil
}

// Focus marks the selector as focused so it will handle keyboard input.
func (s *ScopeSelector) Focus() { s.Focused = true }

// Blur removes keyboard focus.
func (s *ScopeSelector) Blur() { s.Focused = false }

// View renders the "Apply to: ○ Global  ● Project" row using the given theme.
// Returns an empty string when Disabled is true.
func (s ScopeSelector) View(th theme.Theme) string {
	if s.Disabled {
		return ""
	}

	label := th.Dim.Render("Apply to: ")
	globalStr := s.renderOption(config.ScopeGlobal, s.GlobalLabel, th)
	projectStr := s.renderOption(config.ScopeProject, s.ProjectLabel, th)

	row := label + globalStr + th.Dim.Render("  ") + projectStr
	if s.Focused {
		// Wrap the whole row in a faint focus indicator.
		indicator := th.Accent.Render("›") + " "
		return indicator + row
	}
	return "  " + row // indent to match the indicator width
}

func (s ScopeSelector) renderOption(opt config.SettingScope, label string, th theme.Theme) string {
	radio := "○ "
	if s.Scope == opt {
		radio = "● "
	}
	text := radio + label
	if s.Scope == opt {
		return th.Accent.Render(text)
	}
	return th.Dim.Render(text)
}

// Rows returns the number of rows the View output occupies (always 1 when
// not disabled, 0 when disabled).
func (s ScopeSelector) Rows() int {
	if s.Disabled {
		return 0
	}
	return 1
}

// ─── ScopeBadge ──────────────────────────────────────────────────────────────

// ScopeBadge renders a dim scope tag for list rows, e.g. " [project]" or
// " [global]". Returns an empty string when scope is ScopeGlobal and
// showGlobal is false (global is the default — only show the badge when
// something lives explicitly in a specific scope).
func ScopeBadge(scope config.SettingScope, showGlobal bool, th theme.Theme) string {
	switch scope {
	case config.ScopeProject:
		return th.Dim.Render(" [project]")
	case config.ScopeGlobal:
		if showGlobal {
			return th.Dim.Render(" [global]")
		}
	}
	return ""
}

// ─── ScopeHint ───────────────────────────────────────────────────────────────

// ScopeHint renders a dim hint line under an edited field, e.g.
// "(Inherited from Global)" or "(Also modified in Global)". Returns an empty
// string when hint is "".
func ScopeHint(hint string, th theme.Theme) string {
	hint = strings.TrimSpace(hint)
	if hint == "" {
		return ""
	}
	return th.Dim.Render(hint)
}

// ─── SaveToLabel ─────────────────────────────────────────────────────────────

// SaveToLabel returns a short human-readable description of where a setting
// will be saved, for footer hints in save-capable overlays.
func SaveToLabel(scope config.SettingScope, globalPath, projectPath string) string {
	switch scope {
	case config.ScopeProject:
		if projectPath != "" {
			return "Saving to: " + projectPath
		}
		return "Saving to: .sagittarius/settings.json"
	default:
		if globalPath != "" {
			return "Saving to: " + globalPath
		}
		return "Saving to: ~/.sagittarius/settings.json"
	}
}

// ─── KeyHint ─────────────────────────────────────────────────────────────────

// KeyHint returns the keyboard hint appended to the dialog footer when the
// scope selector is available. E.g. "Tab · scope".
func KeyHint() string {
	return "Tab · scope"
}

