package bubbletea

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/undeadindustries/sagittarius/internal/ui"
	"github.com/undeadindustries/sagittarius/internal/ui/theme"
)

// fitLeftRight shrinks a two-part status line so that
// width(left) + 1 (minimum gap) + width(right) <= width. When the parts are too
// wide it truncates the LEFT segment first, preserving the right-aligned segment
// (token counts / metrics / context%) which carries the more important live
// state. If the right segment alone overflows, it is truncated and the left is
// dropped. This prevents the line from exceeding the terminal width, which would
// soft-wrap and corrupt Bubble Tea's frame accounting (ghost rows).
func fitLeftRight(left, right string, width int) (string, string) {
	if width < 1 {
		width = 1
	}
	if lipgloss.Width(left)+lipgloss.Width(right)+1 <= width {
		return left, right
	}
	rw := lipgloss.Width(right)
	if rw >= width {
		return "", ansi.Truncate(right, width, "")
	}
	leftBudget := max(width-rw-1, 0)
	return ansi.Truncate(left, leftBudget, ""), right
}

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

// scrollShortcutHints is the compact legend for scrollback keys on the composer
// status row. Page keys differ by OS; Alt+M toggles mouse-wheel scrolling on all.
func scrollShortcutHints() string {
	return scrollShortcutHintsForGOOS(runtime.GOOS)
}

// toggleHints labels the two live toggles after the scroll keys: Alt+M / ⌥M
// toggles mouse-wheel scrolling (both PC and Mac keyboards are shown so Mac
// users SSH'd into Linux still see the key they press), and Ctrl+T toggles the
// reasoning ("thinking") view. Each key group is followed by a plain-word label
// rather than an icon: no single-width glyph for "mouse"/"thinking" renders
// reliably across terminal fonts, so words stay terminal-safe.
const toggleHints = "Alt+M or ⌥M mouse · Ctrl+T thinking"

func scrollShortcutHintsForGOOS(goos string) string {
	switch goos {
	case "darwin":
		return "Fn↑ Fn↓ · " + toggleHints
	default:
		return "Pg↑ Pg↓ · " + toggleHints
	}
}

// statusRowLine builds the unstyled (left, right) halves of the composer status
// row. Scroll shortcuts sit on the left (beside the tool-policy hint) so they
// stay visible on narrow terminals; AGENTS.md/skill counts stay on the right.
func (m *model) statusRowParts() (left, right string) {
	cs, ok := m.composerStatus()
	hints := scrollShortcutHints()
	if ok {
		left = approvalHint(cs.ApprovalMode)
	}
	if left != "" {
		left = left + "  ·  " + hints
	} else {
		left = hints
	}

	right = contextSummary(len(m.opts.LoadedMemoryFiles), 0)
	if ok {
		right = contextSummary(len(m.opts.LoadedMemoryFiles), cs.SkillCount)
		right = prependStatus(right, cs.GoalStatusText)
		right = prependStatus(right, cs.GrillStatusText)
	}
	return left, right
}

// prependStatus joins a status-row segment (goal/grill indicator) ahead of the
// existing right side, omitting the separator when either half is empty.
func prependStatus(right, status string) string {
	if status == "" {
		return right
	}
	if right == "" {
		return status
	}
	return status + "  ·  " + right
}

// renderStatusRow draws the composer status row shown between the scrollback and
// the input box. Returns "" when there is nothing to display so the row (and
// its reserved height) collapses entirely.
func (m *model) renderStatusRow() string {
	left, right := m.statusRowParts()
	return renderStatusRowLine(left, right, m.th, m.width)
}

func renderStatusRowLine(left, right string, th theme.Theme, width int) string {
	if width < 1 {
		width = 1
	}
	left, right = fitLeftRight(left, right, width)
	gap := max(width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	return th.Dim.Render(left) + strings.Repeat(" ", gap) + th.Dim.Render(right)
}

// statusRowRows is the height (always 1) the composer status row occupies, so the
// scrollback viewport can shrink to make room for scroll shortcut hints.
func (m *model) statusRowRows() int {
	return 1
}
