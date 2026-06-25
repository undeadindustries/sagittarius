package modelpickdialog

import (
	"fmt"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/ui"
	"github.com/undeadindustries/sagittarius/internal/ui/overlay"
)

// View renders the global model picker overlay.
func (m Model) View() string {
	dim := m.th.Dim
	var b strings.Builder
	b.WriteString(m.th.Title.Render("Select Model") + "\n\n")
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
	if !m.scopeSel.Disabled {
		b.WriteString("\n\n" + m.scopeSel.View(m.th))
	}
	footerHint := "↑/↓ move • Enter select • Esc close"
	if !m.scopeSel.Disabled {
		footerHint += " • Tab · scope"
	}
	b.WriteString("\n\n" + dim.Render(footerHint))

	return overlay.Frame(m.th, m.width, overlay.DefaultMinWidth, b.String())
}

func (m Model) wrap(s string) string {
	return ui.WrapText(s, overlay.ContentWidth(m.width, overlay.DefaultMinWidth))
}

func (m Model) body() string {
	dim := m.th.Dim
	var b strings.Builder
	if len(m.entries) == 0 {
		b.WriteString(dim.Render("No active models. Open /providers and activate some first."))
		return b.String()
	}
	for i, e := range m.entries {
		label := fmt.Sprintf("%s/%s", e.DisplayID, e.Model)
		if e.ProviderID == m.curProvider && e.Model == m.curModel {
			label += dim.Render("  — current")
		}
		b.WriteString(overlay.Row(m.th, label, i == m.cursor) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
