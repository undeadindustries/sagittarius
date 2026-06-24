package toolsdialog

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

// View renders the tool inventory overlay.
func (m Model) View() string {
	dim := m.th.Dim
	var b strings.Builder
	b.WriteString(m.th.Title.Render("Tools") + "\n\n")
	b.WriteString(m.body())

	if m.info != "" {
		b.WriteString("\n\n" + dim.Render(m.wrap(m.info)))
	}
	if m.errMsg != "" {
		b.WriteString("\n\n" + m.th.Error.Render(m.wrap("✗ "+m.errMsg)))
	}
	b.WriteString("\n\n" + dim.Render("↑/↓ move • Space toggle MCP tool • Enter activate • r reload • Esc close"))

	box := m.boxStyle()
	body := b.String()
	if m.width > 0 {
		return box.Width(m.contentWidth()).Render(body)
	}
	return box.Render(body)
}

func (m Model) body() string {
	dim := m.th.Dim
	total := len(m.rows)
	if total == 0 {
		return ""
	}
	start, end := m.listWindow(total)
	var b strings.Builder
	if start > 0 {
		b.WriteString(dim.Render(fmt.Sprintf("  … %d more above", start)) + "\n")
	}
	for i := start; i < end; i++ {
		r := m.rows[i]
		switch r.kind {
		case rowSectionHeader:
			if i > start {
				b.WriteString("\n")
			}
			b.WriteString(m.th.Accent.Render(r.text) + "\n")
		case rowServerHeader:
			b.WriteString(dim.Render("  "+r.text) + "\n")
		case rowNote:
			b.WriteString(dim.Render("  "+r.text) + "\n")
		case rowBuiltin:
			label := lockGlyph + " " + r.text
			b.WriteString(m.renderRow(label, i == m.cursor) + "\n")
		case rowMCPTool:
			box := "[ ]"
			if r.enabled {
				box = "[x]"
			}
			b.WriteString(m.renderRow(box+" "+r.text, i == m.cursor) + "\n")
		case rowAction:
			if i > start {
				b.WriteString("\n")
			}
			b.WriteString(m.renderRow(r.text, i == m.cursor) + "\n")
		}
	}
	if end < total {
		b.WriteString(dim.Render(fmt.Sprintf("  … %d more below", total-end)) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

const lockGlyph = "·"

func (m Model) renderRow(label string, selected bool) string {
	if selected {
		return m.th.Accent.Render("> " + label)
	}
	return "  " + label
}

func (m Model) boxStyle() lipgloss.Style {
	s := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	if m.th.Colored {
		s = s.BorderForeground(m.th.FocusBorderColor)
	}
	return s
}

func (m Model) wrap(s string) string {
	return ui.WrapText(s, m.contentWidth())
}

func (m Model) contentWidth() int {
	w := m.width - 4
	if w < 20 {
		return 20
	}
	return w
}
