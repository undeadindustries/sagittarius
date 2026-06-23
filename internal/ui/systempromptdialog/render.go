package systempromptdialog

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

// View renders the project system-prompt picker.
func (m Model) View() string {
	dim := m.th.Dim
	var b strings.Builder
	b.WriteString(m.th.Title.Render("System Prompt") + "\n\n")
	b.WriteString(dim.Render("Project-wide personality for this workspace (.sagittarius/settings.json)") + "\n\n")
	for i, p := range m.options {
		label := p.label
		if i == m.cursor {
			b.WriteString(m.th.Accent.Render("> "+label) + "\n")
		} else {
			b.WriteString("  " + label + "\n")
		}
	}
	if m.info != "" {
		b.WriteString("\n" + dim.Render(m.wrap(m.info)))
	}
	if m.errMsg != "" {
		b.WriteString("\n" + m.th.Error.Render(m.wrap("✗ "+m.errMsg)))
	}
	b.WriteString("\n\n" + dim.Render("↑/↓ move • Enter apply • Esc close"))

	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	if m.th.Colored {
		box = box.BorderForeground(m.th.FocusBorderColor)
	}
	body := b.String()
	if m.width > 0 {
		return box.Width(m.contentWidth()).Render(body)
	}
	return box.Render(body)
}

func (m Model) contentWidth() int {
	w := m.width - 4
	if w < 20 {
		return 20
	}
	return w
}

func (m Model) wrap(s string) string {
	return ui.WrapText(s, m.contentWidth())
}
