package modelpickdialog

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

// View renders the global model picker overlay.
func (m Model) View() string {
	dim := m.th.Dim
	var b strings.Builder
	b.WriteString(m.th.Title.Render("Select Model") + "\n\n")
	b.WriteString(m.body())

	if m.info != "" {
		line := m.info
		if m.applying {
			line = m.spin.View() + " " + line
		}
		b.WriteString("\n\n" + dim.Render(m.wrap(line)))
	}
	if m.errMsg != "" {
		b.WriteString("\n\n" + m.th.Error.Render(m.wrap("✗ "+m.errMsg)))
	}
	if !m.scopeSel.Disabled {
		b.WriteString("\n\n" + m.scopeSel.View(m.th))
	}
	footerHint := "↑/↓ move • Enter select • Esc close"
	if !m.scopeSel.Disabled {
		footerHint += " • Tab · scope"
	}
	b.WriteString("\n\n" + dim.Render(footerHint))

	box := m.boxStyle()
	body := b.String()
	if m.width > 0 {
		return box.Width(m.contentWidth()).Render(body)
	}
	return box.Render(body)
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

func (m Model) body() string {
	dim := m.th.Dim
	var b strings.Builder
	if len(m.entries) == 0 {
		b.WriteString(dim.Render("No active models. Open /providers and activate some first."))
		return b.String()
	}
	for i, e := range m.entries {
		label := fmt.Sprintf("%s/%s", e.DisplayID, e.Model)
		if e.ProviderID == m.curProvider && e.Model == m.curModel {
			label += dim.Render("  — current")
		}
		b.WriteString(m.renderRow(label, i == m.cursor) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) renderRow(label string, selected bool) string {
	if selected {
		return m.th.Accent.Render("> " + label)
	}
	return "  " + label
}
