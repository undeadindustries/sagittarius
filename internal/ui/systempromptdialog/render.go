package systempromptdialog

import (
	"strings"

	"github.com/undeadindustries/sagittarius/internal/ui"
	"github.com/undeadindustries/sagittarius/internal/ui/overlay"
)

// View renders the project system-prompt picker.
func (m Model) View() string {
	dim := m.th.Dim
	var b strings.Builder
	b.WriteString(m.th.Title.Render("System Prompt") + "\n\n")
	b.WriteString(dim.Render("Project-wide personality for this workspace (.sagittarius/settings.json)") + "\n\n")
	for i, p := range m.options {
		b.WriteString(overlay.Row(m.th, p.label, i == m.cursor) + "\n")
	}
	if m.info != "" {
		line := m.info
		if m.applying {
			line = m.spin.View() + " " + line
		}
		b.WriteString("\n" + dim.Render(m.wrap(line)))
	}
	if m.errMsg != "" {
		b.WriteString("\n" + m.th.Error.Render(m.wrap("✗ "+m.errMsg)))
	}
	b.WriteString("\n\n" + dim.Render("↑/↓ move • Enter apply • Esc close"))

	return overlay.Frame(m.th, m.width, overlay.DefaultMinWidth, b.String())
}

func (m Model) wrap(s string) string {
	return ui.WrapText(s, overlay.ContentWidth(m.width, overlay.DefaultMinWidth))
}
