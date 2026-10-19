package bubbletea

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/undeadindustries/sagittarius/internal/mdfence"
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
	if leftBudget == 0 {
		return "", right
	}
	return truncateAtSeparator(left, leftBudget), right
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
// the footer settings. Sagittarius does not have gemini-cli's Shift+Tab
// approval cycling, so the label states the policy rather than a cycle hint.
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

// scrollShortcutHints is the compact legend for the composer status row.
// Full screen shows the scroll keys and the mouse toggle. Inline mode shows
// /screen, because page keys and mouse capture do nothing there.
func (m *model) scrollShortcutHints() string {
	if m != nil && m.inline {
		return inlineShortcuts
	}
	return fullscreenShortcuts
}

// Status-row key hints, ordered by how much a user needs them: the layout and
// thinking keys come first, so a narrow terminal drops the mouse and scroll
// keys before them (see truncateAtSeparator). The mouse and scroll hints name
// both keyboards. The process OS is the server's, so a Mac over SSH would
// otherwise be told to press Page Up. Words, not icons: no single-width glyph
// for "mouse"/"thinking" renders reliably across terminal fonts.
const (
	hintScreen    = "/screen display mode"
	hintThinking  = "Ctrl+T show thinking"
	hintReasoning = "Alt+R or ⌥R effort"
	hintModel     = "Ctrl+/ model"
	hintYolo      = "Alt+Y or ⌥Y yolo"
	hintMouse     = "Alt+M or ⌥M mouse"
	hintScroll    = "Pg↑ Pg↓ or Fn↑ Fn↓"

	// sessionHints are the per-turn controls shared by both screen modes.
	sessionHints = hintScreen + " · " + hintThinking + " · " + hintReasoning + " · " + hintModel + " · " + hintYolo

	fullscreenShortcuts = sessionHints + " · " + hintMouse + " · " + hintScroll

	// readOnlyLiftHint leads the key hints while the read-only posture is on.
	readOnlyLiftHint = "/readonly off to lift"
)

// toolPolicyLabel is the footer's tool-approval entry: the read-only posture
// when it is on, else the approval mode. Empty when the app reports no
// composer status (headless demos, tests), so the entry is simply omitted.
func (m *model) toolPolicyLabel() string {
	cs, ok := m.composerStatus()
	if !ok {
		return ""
	}
	if cs.ReadOnlyPosture {
		return "Tools: read-only"
	}
	return approvalHint(cs.ApprovalMode)
}

// footerSettings is the current-state group at the start of the footer detail
// line: the tool policy, the layout, the thinking-box state, when skills are
// turned off, and who owns the mouse. It reads live model state, so it changes
// the moment /screen, Ctrl+T, /settings, or Alt+M does. Inline mode never
// captures the mouse, so it reports the terminal as owner. The order is the
// drop order on a narrow terminal, last entry first, so "skills: off" sits
// ahead of "mouse: …" (displacing mouse when width runs short) and the tool
// policy is the last of these to go.
func (m *model) footerSettings() string {
	screen, mouse := "full", "off"
	switch {
	case m.inline:
		screen, mouse = "inline", "terminal"
	case m.mouseEnabled:
		mouse = "on"
	}
	thinking := "hidden"
	if m.effectiveShowThinking() {
		thinking = "shown"
	}
	entries := []string{"screen: " + screen, "thinking: " + thinking}
	if cs, ok := m.composerStatus(); ok {
		if cs.ReasoningEffort != "" {
			entries = append(entries, "effort: "+cs.ReasoningEffort)
		}
		if cs.SkillsDisabled {
			entries = append(entries, "skills: off")
		}
	}
	entries = append(entries, "mouse: "+mouse)
	if tools := m.toolPolicyLabel(); tools != "" {
		entries = append([]string{tools}, entries...)
	}
	return strings.Join(entries, " · ")
}

// truncateAtSeparator shortens a plain status line to width cells without
// cutting an entry in half. Entries are split by a "·" with spaces around it.
// When the line does not fit, whole entries are dropped from the end. If even
// the first entry overflows, it is cut at width. It never wraps, because a
// wrapped status line corrupts Bubble Tea's frame accounting.
func truncateAtSeparator(s string, width int) string {
	width = max(width, 1)
	if ansi.StringWidth(s) <= width {
		return s
	}
	cut := ansi.Truncate(s, width, "")
	// The cut landed exactly on an entry boundary, so the kept entries are whole.
	if rest := s[len(cut):]; strings.HasPrefix(rest, " ") && strings.HasPrefix(strings.TrimLeft(rest, " "), "·") {
		return strings.TrimRight(cut, " ")
	}
	if i := strings.LastIndex(cut, "·"); i > 0 {
		return strings.TrimRight(cut[:i], " ")
	}
	return cut
}

// statusRowParts builds the unstyled (left, right) halves of the composer status
// row. The left side is only keys to press; current settings, including the
// tool policy, live in the footer (see footerSettings). AGENTS.md/skill counts
// stay on the right.
func (m *model) statusRowParts() (left, right string) {
	cs, ok := m.composerStatus()
	left = m.scrollShortcutHints()
	// The read-only posture itself is a setting (footer); the key that lifts it
	// is a hint, so it leads this row while the posture is on.
	if ok && cs.ReadOnlyPosture {
		left = readOnlyLiftHint + "  ·  " + left
	}

	if m.pastes.expanded != nil {
		left = left + "  ·  Ctrl+O collapse paste"
	} else if len(m.pastes.content) > 0 || pastePlaceholderRe.MatchString(m.input.Value()) {
		left = left + "  ·  Ctrl+O expand paste"
	}

	// Latch the auto-title announcement the first time the app surfaces it so
	// the status-row line persists across frames (the provider peeks, never
	// consumes). Dismissal is local (titleDismissed).
	if ok && cs.TitleAnnouncementText != "" && !m.titleDismissed {
		m.titleAnnouncement = cs.TitleAnnouncementText
	}

	right = contextSummary(len(m.opts.LoadedMemoryFiles), 0)
	if ok {
		right = contextSummary(len(m.opts.LoadedMemoryFiles), cs.SkillCount)
		right = prependStatus(right, cs.GoalStatusText)
		right = prependStatus(right, cs.GrillStatusText)
		if !m.titleDismissed {
			right = prependStatus(right, m.titleAnnouncement)
		}
	}
	// Prepend last so the copy hint is the leftmost segment of the right side:
	// fitLeftRight truncates the right segment from its right edge when it
	// overflows, so the hint survives the narrowest terminals.
	right = prependStatus(right, m.copyHint)

	nWorking := 0
	waiting := false
	for _, c := range m.cardByID {
		if isSubagentTool(c.toolName) && c.phase == toolRunning {
			nWorking++
		}
		if c.toolName == wireWaitUntil && c.phase == toolRunning {
			waiting = true
		}
	}
	if nWorking > 0 {
		right = prependStatus(right, fmt.Sprintf("%d Working", nWorking))
	}
	if waiting {
		right = prependStatus(right, "Ask while waiting")
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

// lastResponseCopyHint builds the status-row copy hint for the most recent
// assistant reply: "/copy reply" always, plus "· /copy code 1-N" when the reply
// holds fenced code blocks. It runs once per turn (on StreamDone) rather than
// per frame, and returns "" when the reply has no visible text (e.g. a
// reasoning-only turn). Block numbering matches /copy code exactly because both
// go through mdfence.Parse (empty fences dropped, nested fences counted once).
func lastResponseCopyHint(blocks []scrollBlock) string {
	for i := len(blocks) - 1; i >= 0; i-- {
		if blocks[i].role != roleResponse {
			continue
		}
		text := strings.TrimSpace(blocks[i].text)
		if text == "" {
			return ""
		}
		_, fenced := mdfence.Parse(text)
		switch len(fenced) {
		case 0:
			return "/copy reply"
		case 1:
			return "/copy reply · /copy code"
		default:
			return fmt.Sprintf("/copy reply · /copy code 1-%d", len(fenced))
		}
	}
	return ""
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
	// fitLeftRight already leaves a gap whenever a left side remains. With the
	// left side dropped the right side may fill the row, so the gap can be 0.
	gap := max(width-lipgloss.Width(left)-lipgloss.Width(right), 0)
	return th.Dim.Render(left) + strings.Repeat(" ", gap) + th.Dim.Render(right)
}

// statusRowRows is the height (always 1) the composer status row occupies, so the
// scrollback viewport can shrink to make room for scroll shortcut hints.
func (m *model) statusRowRows() int {
	return 1
}
