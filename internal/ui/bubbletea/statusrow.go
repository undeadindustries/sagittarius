package bubbletea

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/undeadindustries/sagittarius/internal/ui"
	"github.com/undeadindustries/sagittarius/internal/ui/theme"
)

// composerStatus reads the optional composer status (approval policy + skill
// count) from the app. The second return is false when the app does not
// implement ui.ComposerStatusProvider.
func (m *model) composerStatus() (ui.ComposerStatus, bool) {
	p, ok := m.app.(ui.ComposerStatusProvider)
	if !ok {
		return ui.ComposerStatus{}, false
	}
	return p.ComposerStatus(), true
}

// approvalHint maps a tool-approval policy to a short, plain-language label for
// the left side of the status row. Sagittarius does not have gemini-cli's
// Shift+Tab approval cycling, so the label states the policy rather than a
// cycle hint.
func approvalHint(mode string) string {
	switch strings.TrimSpace(strings.ToLower(mode)) {
	case "autoedit", "auto_edit":
		return "Tools: auto-accept edits"
	case "yolo":
		return "Tools: auto-approve all (yolo)"
	case "default", "":
		return "Tools: confirm before run"
	default:
		return "Tools: " + mode
	}
}

// contextSummary builds the right side of the status row: counts of loaded
// AGENTS.md files and available skills, omitting zero counts (mirrors
// gemini-cli's ContextSummaryDisplay). Returns "" when there is nothing to show.
func contextSummary(memoryFiles, skillCount int) string {
	var parts []string
	if memoryFiles > 0 {
		parts = append(parts, fmt.Sprintf("%d AGENTS.md %s", memoryFiles, plural(memoryFiles, "file", "files")))
	}
	if skillCount > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", skillCount, plural(skillCount, "skill", "skills")))
	}
	return strings.Join(parts, " · ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// statusRowLine builds the unstyled (left, right) halves of the composer status
// row. Either half may be empty.
func (m *model) statusRowParts() (left, right string) {
	cs, ok := m.composerStatus()
	if ok {
		left = approvalHint(cs.ApprovalMode)
	}
	right = contextSummary(len(m.opts.LoadedMemoryFiles), cs.SkillCount)
	return left, right
}

// renderStatusRow draws the composer status row shown between the scrollback and
// the input box. Returns "" when there is nothing to display so the row (and
// its reserved height) collapses entirely.
func (m *model) renderStatusRow() string {
	left, right := m.statusRowParts()
	if left == "" && right == "" {
		return ""
	}
	return renderStatusRowLine(left, right, m.th, m.width)
}

func renderStatusRowLine(left, right string, th theme.Theme, width int) string {
	if width < 1 {
		width = 1
	}
	gap := max(width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	return th.Dim.Render(left) + strings.Repeat(" ", gap) + th.Dim.Render(right)
}

// statusRowRows is the height (0 or 1) the composer status row occupies, so the
// scrollback viewport can shrink to make room for it.
func (m *model) statusRowRows() int {
	left, right := m.statusRowParts()
	if left == "" && right == "" {
		return 0
	}
	return 1
}
