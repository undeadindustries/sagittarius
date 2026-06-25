package settingsdialog

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

// View renders the settings browser overlay.
func (m Model) View() string {
	dim := m.th.Dim
	var b strings.Builder
	b.WriteString(m.th.Title.Render("Settings") + "\n\n")

	if m.editing {
		b.WriteString(m.viewEdit())
	} else {
		b.WriteString(m.viewList())
	}

	if m.info != "" {
		b.WriteString("\n\n" + dim.Render(m.wrap(m.info)))
	}
	if m.errMsg != "" {
		b.WriteString("\n\n" + m.th.Error.Render(m.wrap("✗ "+m.errMsg)))
	}
	if !m.editing {
		if !m.scopeSel.Disabled {
			b.WriteString("\n\n" + m.scopeSel.View(m.th))
		}
		b.WriteString("\n\n" + dim.Render(m.footerHint()))
	}

	box := m.boxStyle()
	body := b.String()
	if m.width > 0 {
		return box.Width(m.contentWidth()).Render(body)
	}
	return box.Render(body)
}

func (m Model) viewList() string {
	var b strings.Builder
	b.WriteString(m.th.Dim.Render("* = overridden in this scope  •  Ctrl+L clear  •  Enter edit/toggle") + "\n\n")
	for i, e := range m.entries {
		b.WriteString(m.renderEntry(e, i == m.cursor) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) renderEntry(e SettingEntry, selected bool) string {
	dim := m.th.Dim
	if e.IsHeader() {
		return dim.Render("── " + e.Label + " ──")
	}
	star := "  "
	if e.DefinedHere {
		star = "* "
	}
	label := e.Label
	val := e.Value
	if val == "" {
		val = dim.Render("(not set)")
	}
	row := fmt.Sprintf("%s%-30s %s", star, label, val)
	if e.MergedValue != "" && e.MergedValue != e.Value && e.MergedValue != "(not set)" {
		row += dim.Render(fmt.Sprintf("  [effective: %s]", e.MergedValue))
	}
	if e.ReadOnly {
		row = dim.Render(row)
	}
	if selected && !e.IsHeader() && !e.ReadOnly {
		return m.th.Accent.Render("> " + row)
	}
	return "  " + row
}

func (m Model) viewEdit() string {
	e := m.entries[m.cursor]
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Edit: %s\n\n", e.Label))
	b.WriteString(m.input.View())
	b.WriteString("\n\n" + m.th.Dim.Render("Enter save • Esc cancel"))
	return b.String()
}

func (m Model) footerHint() string {
	base := "↑/↓ move • Enter edit/toggle • Ctrl+L clear • Esc close"
	if !m.scopeSel.Disabled {
		base += " • Tab · scope"
	}
	return base
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
