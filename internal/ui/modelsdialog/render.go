package modelsdialog

import (
	"fmt"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/ui"
	"github.com/undeadindustries/sagittarius/internal/ui/overlay"
)

// View renders the per-model settings editor.
func (m Model) View() string {
	dim := m.th.Dim
	var b strings.Builder
	b.WriteString(m.th.Title.Render("Model Settings") + "\n\n")
	b.WriteString(m.body())

	if m.info != "" {
		b.WriteString("\n\n" + dim.Render(m.wrap(m.info)))
	}
	if m.errMsg != "" {
		b.WriteString("\n\n" + m.th.Error.Render(m.wrap("✗ "+m.errMsg)))
	}
	b.WriteString("\n\n" + dim.Render(m.footerHint()))

	return overlay.Frame(m.th, m.width, overlay.DefaultMinWidth, b.String())
}

func (m Model) footerHint() string {
	switch m.screen {
	case screenSetting:
		return "↑/↓ move • Enter select • r clear • Esc back"
	case screenEditField:
		return "Enter save • Esc cancel"
	default:
		return "↑/↓ move • Enter select • Esc close"
	}
}

func (m Model) wrap(s string) string {
	return ui.WrapText(s, overlay.ContentWidth(m.width, overlay.DefaultMinWidth))
}

func (m Model) body() string {
	switch m.screen {
	case screenList:
		return m.renderList()
	case screenSetting:
		return m.renderSettings()
	case screenEditField:
		return m.editTitle + "\n\n" + m.input.View()
	}
	return ""
}

func (m Model) renderList() string {
	dim := m.th.Dim
	var b strings.Builder
	if len(m.entries) == 0 {
		b.WriteString(dim.Render("No active models. Open /providers and activate some first."))
		return b.String()
	}
	for i, e := range m.entries {
		label := fmt.Sprintf("%s/%s", e.ProviderLabel, e.Model)
		b.WriteString(m.renderRow(label, i == m.cursor) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) renderSettings() string {
	dim := m.th.Dim
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Settings for %s/%s\n\n",
		m.targetProvider, m.targetModel))
	for i, item := range m.settingItems {
		label := item.label
		if item.key != "back" {
			if v := m.settingValues[item.key]; v != "" {
				label += dim.Render("  = " + v)
			}
		}
		b.WriteString(m.renderRow(label, i == m.cursor) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) renderRow(label string, selected bool) string {
	return overlay.Row(m.th, label, selected)
}
