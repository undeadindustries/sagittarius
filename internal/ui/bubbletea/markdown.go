package bubbletea

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/undeadindustries/sagittarius/internal/ui/theme"
)

// Lightweight markdown for assistant responses. This is deliberately a small
// in-house subset (headings, lists, fenced code, inline bold/italic/code) rather
// than a full CommonMark renderer or a glamour dependency — enough to make typical
// model output readable in the TUI. Inline styling is applied per wrapped line,
// so a marker that straddles a wrap boundary degrades to literal text.
var (
	mdHeading    = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	mdBullet     = regexp.MustCompile(`^(\s*)[-*+]\s+(.*)$`)
	mdInlineCode = regexp.MustCompile("`([^`]+)`")
	mdBoldStar   = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	mdBoldUnder  = regexp.MustCompile(`__([^_]+)__`)
	mdItalicStar = regexp.MustCompile(`\*([^*]+)\*`)
	mdItalicUnd  = regexp.MustCompile(`_([^_]+)_`)
)

var (
	mdBoldStyle   = lipgloss.NewStyle().Bold(true)
	mdItalicStyle = lipgloss.NewStyle().Italic(true)
)

// renderMarkdown converts assistant text into styled, width-wrapped lines.
func renderMarkdown(text string, width int, th theme.Theme) []string {
	if width < 1 {
		width = 1
	}
	var out []string
	inCode := false
	for _, raw := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(raw), "```") {
			inCode = !inCode
			continue // hide the fence markers themselves
		}
		if inCode {
			out = append(out, renderCodeLine(raw, width, th)...)
			continue
		}
		out = append(out, renderProseLine(raw, width, th)...)
	}
	return out
}

// renderCodeLine renders a verbatim code line with a left bar, truncated (not
// wrapped) so indentation is preserved.
func renderCodeLine(line string, width int, th theme.Theme) []string {
	const bar = "│ "
	avail := max(width-lipgloss.Width(bar), 1)
	if lipgloss.Width(line) > avail {
		line = truncateVisible(line, avail)
	}
	return []string{th.Code.Render(bar + line)}
}

// renderProseLine handles headings, bullets, and paragraphs with inline styling.
func renderProseLine(line string, width int, th theme.Theme) []string {
	if m := mdHeading.FindStringSubmatch(line); m != nil {
		return wrapStyled(m[2], width, th.Title)
	}
	prefix := ""
	body := line
	if m := mdBullet.FindStringSubmatch(line); m != nil {
		prefix = m[1] + "• "
		body = m[2]
	}
	pw := lipgloss.Width(prefix)
	wrapped := strings.Split(wrapText(body, max(width-pw, 1)), "\n")
	out := make([]string, 0, len(wrapped))
	for i, w := range wrapped {
		styled := styleInline(w, th)
		if i == 0 && prefix != "" {
			out = append(out, th.Accent.Render(prefix)+styled)
		} else if prefix != "" {
			out = append(out, strings.Repeat(" ", pw)+styled)
		} else {
			out = append(out, styled)
		}
	}
	return out
}

// wrapStyled wraps plain text and applies a single style to each line.
func wrapStyled(text string, width int, style lipgloss.Style) []string {
	wrapped := strings.Split(wrapText(text, width), "\n")
	out := make([]string, 0, len(wrapped))
	for _, w := range wrapped {
		out = append(out, style.Render(w))
	}
	return out
}

// styleInline applies inline code, bold, then italic styling to one line. Code
// is processed first so emphasis markers inside code spans are left literal.
func styleInline(line string, th theme.Theme) string {
	line = mdInlineCode.ReplaceAllStringFunc(line, func(m string) string {
		return th.Code.Render(mdInlineCode.FindStringSubmatch(m)[1])
	})
	line = mdBoldStar.ReplaceAllStringFunc(line, func(m string) string {
		return mdBoldStyle.Render(mdBoldStar.FindStringSubmatch(m)[1])
	})
	line = mdBoldUnder.ReplaceAllStringFunc(line, func(m string) string {
		return mdBoldStyle.Render(mdBoldUnder.FindStringSubmatch(m)[1])
	})
	line = mdItalicStar.ReplaceAllStringFunc(line, func(m string) string {
		return mdItalicStyle.Render(mdItalicStar.FindStringSubmatch(m)[1])
	})
	line = mdItalicUnd.ReplaceAllStringFunc(line, func(m string) string {
		return mdItalicStyle.Render(mdItalicUnd.FindStringSubmatch(m)[1])
	})
	return line
}

// truncateVisible cuts a string to at most width visible columns. Code lines
// have no ANSI codes at this point, so a rune-count cut is sufficient.
func truncateVisible(s string, width int) string {
	if lipgloss.Width(s) <= width {
		return s
	}
	runes := []rune(s)
	if width > len(runes) {
		width = len(runes)
	}
	return string(runes[:max(width, 0)])
}
