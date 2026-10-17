package modelsdialog

import (
	"fmt"

	"github.com/undeadindustries/sagittarius/internal/ui/overlay"
)

// View renders the per-model settings editor. List screens are windowed to the
// terminal height so the title, scope line, and hints stay visible.
func (m Model) View() string {
	if m.screen == screenEditField {
		body := m.head() + "\n" + m.editTitle + "\n\n" + m.input.View() + "\n" +
			overlay.Foot(m.th, m.info, m.errMsg, m.footerHint())
		return overlay.Frame(m.th, m.width, overlay.DefaultMinWidth, body)
	}
	rows, cursor, sub := m.rows()
	head := m.head()
	if sub != "" {
		head += "\n" + sub
	}
	foot := overlay.Foot(m.th, m.info, m.errMsg, m.footerHint())
	return overlay.FitFrame(m.th, overlay.Box{Width: m.width, Height: m.height},
		overlay.List{Head: head, Rows: rows, Foot: foot, Cursor: cursor})
}

// head is the title plus the scope line (when a project file is available).
func (m Model) head() string {
	head := overlay.Title(m.th, "Model Settings") + "\n"
	if !m.scopeSel.Disabled {
		head += m.scopeSel.View(m.th) + "\n"
	}
	return head
}

func (m Model) footerHint() string {
	switch m.screen {
	case screenSetting:
		return "↑/↓ move • Enter select • r clear • Esc back"
	case screenPickValue:
		return "↑/↓ move • Enter select • Esc back"
	case screenEditField:
		return "Enter save • Esc cancel"
	default:
		return "↑/↓ move • Enter select • Esc close"
	}
}

// rows returns the current screen's selectable rows, the cursor, and an
// optional sub-heading rendered above them.
func (m Model) rows() (rows []string, cursor int, sub string) {
	switch m.screen {
	case screenSetting:
		return m.settingRows(), m.cursor, m.settingsHeading()
	case screenPickValue:
		return m.pickerRows(), m.cursor, m.pickTitle + "\n"
	}
	return m.listRows(), m.cursor, ""
}

func (m Model) listRows() []string {
	if len(m.entries) == 0 {
		return []string{m.th.Dim.Render("No active models. Open /providers and activate some first.")}
	}
	rows := make([]string, len(m.entries))
	for i, e := range m.entries {
		rows[i] = overlay.Row(m.th, fmt.Sprintf("%s/%s", e.ProviderLabel, e.Model), i == m.cursor)
	}
	return rows
}

func (m Model) settingsHeading() string {
	heading := fmt.Sprintf("Settings for %s/%s", m.targetProvider, m.targetModel)
	if m.capabilityHint != "" {
		heading += "\n" + m.th.Dim.Render("Reasoning: "+m.capabilityHint)
	}
	return heading + "\n"
}

// settingRows shows a value set in the selected scope as "= v" and, in the
// project view, a global value it inherits as "(global: v)".
func (m Model) settingRows() []string {
	dim := m.th.Dim
	rows := make([]string, len(m.settingItems))
	for i, item := range m.settingItems {
		label := item.label
		if item.key != "back" {
			if v := m.settingValues[item.key]; v != "" {
				label += dim.Render("  = " + v)
			} else if v := m.inheritedValues[item.key]; v != "" {
				label += dim.Render("  (global: " + v + ")")
			}
		}
		rows[i] = overlay.Row(m.th, label, i == m.cursor)
	}
	return rows
}

func (m Model) pickerRows() []string {
	rows := make([]string, len(m.pickOptions))
	for i, opt := range m.pickOptions {
		rows[i] = overlay.Row(m.th, opt.label, i == m.cursor)
	}
	return rows
}
