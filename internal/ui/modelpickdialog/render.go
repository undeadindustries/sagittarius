package modelpickdialog

import (
	"fmt"

	"github.com/undeadindustries/sagittarius/internal/ui/overlay"
)

// View renders the global model picker overlay.
func (m Model) View() string {
	head := overlay.Title(m.th, "Select Model") + "\n"
	if !m.scopeSel.Disabled {
		head += m.scopeSel.View(m.th) + "\n"
	}
	info := m.info
	if info != "" && m.applying {
		info = m.spin.View() + " " + info
	}
	foot := overlay.Foot(m.th, info, m.errMsg, "↑/↓ move • Enter select • Esc close")
	return overlay.FitFrame(m.th, overlay.Box{Width: m.width, Height: m.height},
		overlay.List{Head: head, Rows: m.rows(), Foot: foot, Cursor: m.cursor})
}

func (m Model) rows() []string {
	dim := m.th.Dim
	if len(m.entries) == 0 {
		return []string{dim.Render("No active models. Open /providers and activate some first.")}
	}
	rows := make([]string, len(m.entries))
	for i, e := range m.entries {
		label := fmt.Sprintf("%s/%s", e.DisplayID, e.Model)
		if e.ProviderID == m.curProvider && e.Model == m.curModel {
			label += dim.Render("  — current")
		}
		rows[i] = overlay.Row(m.th, label, i == m.cursor)
	}
	return rows
}
