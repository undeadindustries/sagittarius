// Package scopedialog provides reusable UI primitives for dual-scope settings
// (Global ~/.sagittarius vs Project .sagittarius/) across overlay dialogs.
//
// The three exports are:
//   - ScopeSelector: a Global/Project switch, flipped with Tab, for choosing where to save.
//   - ScopeBadge: renders a dim "[global]" or "[project]" label suffix for list rows.
//   - ScopeHint: renders a one-line dim hint (e.g. "Inherited from Global").
//
// These primitives use lipgloss for rendering and are safe to embed in any
// Bubble Tea overlay model. ScopeSelector.HandleKey takes the parent's key
// press and reports whether it consumed it, so the parent decides on which
// screens Tab switches scope (text inputs keep Tab for themselves).
package scopedialog

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/ui/overlay"
	"github.com/undeadindustries/sagittarius/internal/ui/theme"
)

// ScopeChangedMsg is emitted by ScopeSelector when the user changes the scope.
// The parent model can listen for this to update its save target.
type ScopeChangedMsg struct {
	Scope config.SettingScope
}

// ToggleKey flips the scope in every dialog that embeds a ScopeSelector. One
// key, no focus step: choosing where a change lands should never take longer
// than making the change.
const ToggleKey = "tab"

// ScopeSelector is a two-option switch between config.ScopeGlobal and
// config.ScopeProject. Embed it in an overlay model, pass key presses to
// HandleKey, and render View in the dialog head so it stays visible when the
// body is windowed to the terminal height.
type ScopeSelector struct {
	Scope config.SettingScope
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

// Toggle flips between Global and Project and returns a command that emits
// ScopeChangedMsg so the parent can reload scope-dependent rows. A disabled
// selector stays on its scope and returns a nil command.
func (s ScopeSelector) Toggle() (ScopeSelector, tea.Cmd) {
	if s.Disabled {
		return s, nil
	}
	if s.Scope == config.ScopeProject {
		s.Scope = config.ScopeGlobal
	} else {
		s.Scope = config.ScopeProject
	}
	scope := s.Scope
	return s, func() tea.Msg { return ScopeChangedMsg{Scope: scope} }
}

// HandleKey toggles on ToggleKey and reports whether it consumed the key, so
// a parent can call it first and fall through to its own bindings otherwise.
func (s ScopeSelector) HandleKey(km tea.KeyMsg) (ScopeSelector, tea.Cmd, bool) {
	if s.Disabled || km.String() != ToggleKey {
		return s, nil, false
	}
	s, cmd := s.Toggle()
	return s, cmd, true
}

// View renders "Save to: ● Global (~/.sagittarius)  ○ Project (.sagittarius/)
// · Tab switches". Returns an empty string when Disabled is true.
func (s ScopeSelector) View(th theme.Theme) string {
	if s.Disabled {
		return ""
	}
	label := th.Dim.Render("Save to: ")
	globalStr := s.renderOption(config.ScopeGlobal, s.GlobalLabel, th)
	projectStr := s.renderOption(config.ScopeProject, s.ProjectLabel, th)
	return label + globalStr + th.Dim.Render("  ") + projectStr + th.Dim.Render("  · Tab switches")
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
	return overlay.Hints(th, hint)
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
// scope selector is available.
func KeyHint() string {
	return "Tab · switch scope"
}
