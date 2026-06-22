package modesdialog

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

// View renders the mode-override editor overlay.
func (m Model) View() string {
	dim := m.th.Dim
	var b strings.Builder
	b.WriteString(m.th.Title.Render("Mode Overrides") + "\n\n")
	b.WriteString(m.body())

	if m.info != "" {
		b.WriteString("\n\n" + dim.Render(m.wrap(m.info)))
	}
	if m.errMsg != "" {
		b.WriteString("\n\n" + m.th.Error.Render(m.wrap("✗ "+m.errMsg)))
	}
	b.WriteString("\n\n" + dim.Render(m.footerHint()))

	box := m.boxStyle()
	body := b.String()
	if m.width > 0 {
		return box.Width(m.contentWidth()).Render(body)
	}
	return box.Render(body)
}

func (m Model) footerHint() string {
	switch m.screen {
	case screenPicker:
		return "↑/↓ move • Enter assign (first row = clear) • Esc back"
	default:
		return "↑/↓ move • Enter assign model • r clear override • Esc close"
	}
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
		b.WriteString(m.renderRow(label, i == m.modeCursor) + "\n")
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
		b.WriteString(m.renderRow(label, i == m.pickCursor) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) renderRow(label string, selected bool) string {
	if selected {
		return m.th.Accent.Render("> " + label)
	}
	return "  " + label
}
