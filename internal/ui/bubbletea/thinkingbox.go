package bubbletea

import (
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"

	"github.com/undeadindustries/sagittarius/internal/ui/theme"
)

// thinkingBoxInnerRows is the fixed number of reasoning lines shown in the box.
// The newest lines stay visible while older ones scroll out of view, keeping the
// box a small, stable height so it does not dominate the terminal.
const thinkingBoxInnerRows = 5

// thinkingBoxVisible reports whether the reasoning box should render: the user
// (or the resolved per-model/global setting) enabled it. While a turn is in
// flight, show the spinner shell even before reasoning tokens arrive; after the
// turn ends the box hides unless there is buffered reasoning to read. Suppressed
// during tool confirmation so the confirm band stands alone.
func (m *model) thinkingBoxVisible() bool {
	if m.confirmReply != nil || !m.effectiveShowThinking() {
		return false
	}
	if strings.TrimSpace(m.thinking) != "" {
		return true
	}
	return m.busy
}

// thinkingBoxRows is the rendered height of the reasoning box (the inner rows
// plus the top and bottom border) so the scrollback viewport shrinks to make
// room; 0 when hidden.
func (m *model) thinkingBoxRows() int {
	if !m.thinkingBoxVisible() {
		return 0
	}
	return thinkingBoxInnerRows + 2
}

// renderThinkingBox draws the reasoning box, or "" when hidden.
func (m *model) renderThinkingBox() string {
	if !m.thinkingBoxVisible() {
		return ""
	}
	text := m.thinking
	if strings.TrimSpace(text) == "" && m.busy {
		text = "Listening for reasoning from the model."
	}
	return renderThinkingBox(m.spin, text, m.th, m.width)
}

// renderThinkingBox draws a rounded box whose top edge carries the working
// spinner and a "Thinking" label, wrapping the last few lines of the streamed
// reasoning. The spinner in the border doubles as the activity indicator, so the
// separate working line is suppressed while the box is shown.
func renderThinkingBox(s spinner.Model, thinking string, th theme.Theme, width int) string {
	if width < 8 {
		width = 8
	}
	inner := width - 4 // "│ " + content + " │"
	if inner < 1 {
		inner = 1
	}

	border := lipgloss.NewStyle().Foreground(th.BorderColor)
	lines := tailLines(wrapText(strings.TrimRight(thinking, "\n"), inner), thinkingBoxInnerRows)
	for len(lines) < thinkingBoxInnerRows {
		lines = append(lines, "")
	}

	var b strings.Builder
	b.WriteString(thinkingBoxTop(s, th, border, width))
	b.WriteString("\n")
	pad := lipgloss.NewStyle().Width(inner)
	for _, line := range lines {
		content := pad.Render(th.Secondary.Render(line))
		b.WriteString(border.Render("│") + " " + content + " " + border.Render("│"))
		b.WriteString("\n")
	}
	b.WriteString(border.Render("╰" + strings.Repeat("─", width-2) + "╯"))
	return b.String()
}

// thinkingBoxTop builds the top border row with the spinner and label embedded:
// "╭─ ⠋ Thinking ─────╮", padded with dashes to width.
func thinkingBoxTop(s spinner.Model, th theme.Theme, border lipgloss.Style, width int) string {
	label := s.View() + " " + th.Dim.Render("Thinking")
	// Visible columns consumed: "╭─ " (3) + label + " " (1) + "╮" (1).
	fill := width - 5 - lipgloss.Width(label)
	if fill < 0 {
		fill = 0
	}
	return border.Render("╭─") + " " + label + " " + border.Render(strings.Repeat("─", fill)+"╮")
}

// tailLines returns the last n lines of text (split on "\n"), preserving order.
func tailLines(text string, n int) []string {
	lines := strings.Split(text, "\n")
	if len(lines) <= n {
		return lines
	}
	return lines[len(lines)-n:]
}
