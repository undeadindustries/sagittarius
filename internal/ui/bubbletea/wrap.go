package bubbletea

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

// wrapText breaks long lines at spaces so viewport content is not clipped at
// the right edge. Existing newlines are preserved. Segments are trimmed: this
// is the prose path. Verbatim content (code, diffs, command output) must use
// wrapVerbatim instead — ui.WrapText is not ANSI-aware and TrimSpace destroys
// indentation.
func wrapText(text string, width int) string {
	return ui.WrapText(text, width)
}

// wrapVerbatim wraps content whose leading whitespace carries meaning — code,
// diffs, command output — so no character is lost at the right edge. Unlike
// wrapText (prose), segments are not trimmed and no synthetic indent is added to
// continuation rows: spaces we invent would corrupt a paste of a Python line or
// a shell command. ansi.Wrap breaks at spaces, hard-breaks a token longer than
// the line, and measures cells rather than bytes, so a CJK or emoji run cannot
// overflow. Tabs are expanded first because ansi.Wrap bills a tab as one cell
// while the terminal advances to the next 8-column stop.
//
// Leading spaces are temporarily rewritten as non-breaking spaces so ansi.Wrap
// cannot discard them when the first token after the indent is itself longer
// than the line (it resets its space buffer on a forced newline). They are
// restored after wrapping.
func wrapVerbatim(text string, width int) []string {
	cleaned := protectLeadingSpaces(sanitizeDisplayText(text))
	rows := strings.Split(ansi.Wrap(cleaned, max(width, 1), ""), "\n")
	for i := range rows {
		rows[i] = unprotectLeadingSpaces(rows[i])
	}
	return rows
}

const wrapNBSP = '\u00A0'

// protectLeadingSpaces rewrites spaces at the start of each line to NBSP so
// ansi.Wrap treats them as content rather than a discardable space buffer.
func protectLeadingSpaces(s string) string {
	if !strings.Contains(s, " ") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	atBOL := true
	for _, r := range s {
		if r == '\n' {
			b.WriteByte('\n')
			atBOL = true
			continue
		}
		if atBOL && r == ' ' {
			b.WriteRune(wrapNBSP)
			continue
		}
		atBOL = false
		b.WriteRune(r)
	}
	return b.String()
}

func unprotectLeadingSpaces(s string) string {
	return strings.ReplaceAll(s, string(wrapNBSP), " ")
}

// wrapVerbatimStyled wraps verbatim text then applies style to each resulting row.
func wrapVerbatimStyled(text string, width int, style lipgloss.Style) []string {
	rows := wrapVerbatim(text, width)
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, style.Render(row))
	}
	return out
}

// padOrTruncate visual-pads a styled line with spaces to exactly width cells,
// and truncates safely with ansi.Truncate if it exceeds width.
func padOrTruncate(line string, width int) string {
	w := lipgloss.Width(line)
	if w < width {
		return line + strings.Repeat(" ", width-w)
	}
	if w > width {
		return ansi.Truncate(line, width, "")
	}
	return line
}

// sanitizeDisplayText normalizes text for display inside borders by stripping
// raw carriage returns, expanding tabs to 8 spaces, and stripping ANSI so
// unprintable/color sequences don't corrupt the box layout width.
func sanitizeDisplayText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "")
	s = ansi.Strip(s)

	if !strings.Contains(s, "\t") {
		return s
	}

	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if !strings.Contains(line, "\t") {
			continue
		}
		var b strings.Builder
		col := 0
		for _, r := range line {
			if r == '\t' {
				spaces := 8 - (col % 8)
				b.WriteString(strings.Repeat(" ", spaces))
				col += spaces
			} else {
				b.WriteRune(r)
				col += lipgloss.Width(string(r))
			}
		}
		lines[i] = b.String()
	}
	return strings.Join(lines, "\n")
}
