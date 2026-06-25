package modesdialog

import (
	"fmt"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/ui"
	"github.com/undeadindustries/sagittarius/internal/ui/overlay"
)

// View renders the mode-override editor overlay.
func (m Model) View() string {
	dim := m.th.Dim
	var b strings.Builder
	b.WriteString(m.th.Title.Render("Mode Overrides") + "\n\n")
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
	b.WriteString("\n\n" + dim.Render(m.footerHint()))

	return overlay.Frame(m.th, m.width, overlay.DefaultMinWidth, b.String())
}

func (m Model) footerHint() string {
	switch m.screen {
	case screenPicker:
		hint := "↑/↓ move • Enter assign (first row = clear) • Esc back"
		if !m.scopeSel.Disabled {
			hint += " • Tab · scope"
		}
		return hint
	default:
		return "↑/↓ move • Enter assign model • r clear override • Esc close"
	}
}

func (m Model) wrap(s string) string {
	return ui.WrapText(s, overlay.ContentWidth(m.width, overlay.DefaultMinWidth))
}

func (m Model) body() string {
	switch m.screen {
	case screenModes:
		return m.renderModes()
	case screenPicker:
		return m.renderPicker()
	}
	return ""
}

func (m Model) renderModes() string {
	dim := m.th.Dim
	var b strings.Builder
	b.WriteString(dim.Render("Enter = assign model override • r = clear to default") + "\n\n")
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
		b.WriteString(overlay.Row(m.th, label, i == m.modeCursor) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) renderPicker() string {
	dim := m.th.Dim
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Assign model for %s mode\n\n", m.targetMode))
	if len(m.models) <= 1 { // only the sentinel, no real models
		b.WriteString(dim.Render("No active models. Open /providers and activate some first."))
		return b.String()
	}
	for i, e := range m.models {
		var label string
		if e.IsClear {
			label = dim.Render("(use default)")
		} else {
			label = fmt.Sprintf("%s/%s", e.DisplayID, e.Model)
		}
		b.WriteString(overlay.Row(m.th, label, i == m.pickCursor) + "\n")
	}
	if !m.scopeSel.Disabled {
		b.WriteString("\n" + m.scopeSel.View(m.th))
	}
	return strings.TrimRight(b.String(), "\n")
}
