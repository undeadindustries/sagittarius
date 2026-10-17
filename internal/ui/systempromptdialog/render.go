package systempromptdialog

import (
	"github.com/undeadindustries/sagittarius/internal/ui/overlay"
)

// View renders the project system-prompt picker, windowed to the terminal
// height so the title and hints stay visible.
func (m Model) View() string {
	head := overlay.Title(m.th, "System Prompt") + "\n\n" +
		overlay.Hints(m.th, "Project-wide personality for this workspace (.sagittarius/settings.json)") + "\n"
	rows := make([]string, len(m.options))
	for i, p := range m.options {
		rows[i] = overlay.Row(m.th, p.label, i == m.cursor)
	}
	info := m.info
	if info != "" && m.applying {
		info = m.spin.View() + " " + info
	}
	foot := overlay.Foot(m.th, info, m.errMsg, "↑/↓ move • Enter apply • Esc close")
	return overlay.FitFrame(m.th, overlay.Box{Width: m.width, Height: m.height},
		overlay.List{Head: head, Rows: rows, Foot: foot, Cursor: m.cursor})
}
