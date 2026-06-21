package modelsdialog

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

// View renders the models picker.
func (m Model) View() string {
	dim := m.th.Dim
	var b strings.Builder
	b.WriteString(m.th.Title.Render("Models") + "\n\n")
	b.WriteString(m.body())

	if m.info != "" {
		b.WriteString("\n\n" + dim.Render(m.wrap(m.info)))
	}
	if m.errMsg != "" {
		b.WriteString("\n\n" + m.th.Error.Render(m.wrap("✗ "+m.errMsg)))
	}
	b.WriteString("\n\n" + dim.Render("↑/↓ move • Enter select • Esc close"))

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
	if m.providerID == "" {
		return dim.Render("(no active provider)")
	}
	var b strings.Builder
	b.WriteString(dim.Render("Active provider: "+m.providerLabel) + "\n\n")
	if len(m.models) == 0 {
		b.WriteString(dim.Render("No active models. Open /providers → Manage models to activate some."))
		return b.String()
	}
	for i, model := range m.models {
		label := model
		if model == m.current {
			label += dim.Render("  — current")
		}
		b.WriteString(m.renderRow(label, i == m.cursor) + "\n")
	}
	b.WriteString("\n" + dim.Render("Activate more models in /providers → Manage models."))
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) renderRow(label string, selected bool) string {
	if selected {
		return m.th.Accent.Render("> " + label)
	}
	return "  " + label
}
