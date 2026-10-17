package modesdialog

import (
	"fmt"

	"github.com/undeadindustries/sagittarius/internal/ui/overlay"
)

// View renders the mode-override editor overlay.
func (m Model) View() string {
	head := overlay.Title(m.th, m.title) + "\n"
	if !m.scopeSel.Disabled {
		head += m.scopeSel.View(m.th) + "\n"
	}
	head += "\n" + m.screenHead()
	info := m.info
	if info != "" && m.applying {
		info = m.spin.View() + " " + info
	}
	rows, cursor := m.rows()
	foot := overlay.Foot(m.th, info, m.errMsg, m.footerHint())
	return overlay.FitFrame(m.th, overlay.Box{Width: m.width, Height: m.height},
		overlay.List{Head: head, Rows: rows, Foot: foot, Cursor: cursor})
}

func (m Model) footerHint() string {
	switch m.screen {
	case screenPicker:
		return "↑/↓ move • Enter assign (first row = clear) • Esc back"
	default:
		hint := "↑/↓ move • Enter assign model • r clear override • Esc close"
		if _, ok := m.deps.(Resetter); ok {
			hint += " • R reset all"
		}
		return hint
	}
}

// screenHead is the fixed text above the current screen's rows.
func (m Model) screenHead() string {
	if m.screen == screenPicker {
		return fmt.Sprintf("Assign model for %s %s\n", m.targetMode, m.itemNoun)
	}
	head := ""
	if h, ok := m.deps.(HeaderProvider); ok {
		if header := h.Header(); header != "" {
			head = overlay.Hints(m.th, header) + "\n\n"
		}
	}
	return head + overlay.Hints(m.th, "Enter = assign model override • r = clear to default") + "\n"
}

// rows returns the current screen's rows and the cursor within them.
func (m Model) rows() ([]string, int) {
	if m.screen == screenPicker {
		return m.pickerRows(), m.pickCursor
	}
	return m.modeRows(), m.modeCursor
}

func (m Model) modeRows() []string {
	dim := m.th.Dim
	rows := make([]string, len(m.modes))
	for i, me := range m.modes {
		label := me.Mode
		if me.Model != "" {
			override := me.Model
			if me.Provider != "" {
				override = me.Provider + "/" + me.Model
			}
			label += dim.Render("  → " + override)
		} else {
			label += dim.Render("  (default)")
		}
		rows[i] = overlay.Row(m.th, label, i == m.modeCursor)
	}
	return rows
}

func (m Model) pickerRows() []string {
	dim := m.th.Dim
	if len(m.models) <= 1 { // only the sentinel, no real models
		return []string{dim.Render("No active models. Open /providers and activate some first.")}
	}
	rows := make([]string, len(m.models))
	for i, e := range m.models {
		label := fmt.Sprintf("%s/%s", e.DisplayID, e.Model)
		if e.IsClear {
			label = dim.Render("(use default)")
		}
		rows[i] = overlay.Row(m.th, label, i == m.pickCursor)
	}
	return rows
}
