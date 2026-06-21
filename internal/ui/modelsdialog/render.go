package modelsdialog

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true)
	selectedStyle = lipgloss.NewStyle().Bold(true)
	dimStyle      = lipgloss.NewStyle().Faint(true)
	errStyle      = lipgloss.NewStyle().Bold(true)
	boxStyle      = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			Padding(0, 1)
)

// View renders the models picker.
func (m Model) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Models") + "\n\n")
	b.WriteString(m.body())

	if m.info != "" {
		b.WriteString("\n\n" + dimStyle.Render(m.wrap(m.info)))
	}
	if m.errMsg != "" {
		b.WriteString("\n\n" + errStyle.Render(m.wrap("✗ "+m.errMsg)))
	}
	b.WriteString("\n\n" + dimStyle.Render("↑/↓ move • Enter select • Esc close"))

	body := b.String()
	if m.width > 0 {
		return boxStyle.Width(m.width).Render(body)
	}
	return boxStyle.Render(body)
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
	if m.providerID == "" {
		return dimStyle.Render("(no active provider)")
	}
	var b strings.Builder
	b.WriteString(dimStyle.Render("Active provider: "+m.providerLabel) + "\n\n")
	if len(m.models) == 0 {
		b.WriteString(dimStyle.Render("No active models. Open /providers → Manage models to activate some."))
		return b.String()
	}
	for i, model := range m.models {
		label := model
		if model == m.current {
			label += dimStyle.Render("  — current")
		}
		b.WriteString(renderRow(label, i == m.cursor) + "\n")
	}
	b.WriteString("\n" + dimStyle.Render("Activate more models in /providers → Manage models."))
	return strings.TrimRight(b.String(), "\n")
}

func renderRow(label string, selected bool) string {
	if selected {
		return selectedStyle.Render("> " + label)
	}
	return "  " + label
}
