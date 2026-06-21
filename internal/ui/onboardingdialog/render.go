package onboardingdialog

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

func (m Model) View() string {
	dim := m.th.Dim
	var b strings.Builder
	b.WriteString(m.th.Title.Render("Welcome to Sagittarius") + "\n\n")
	b.WriteString(m.screenBody())

	if m.info != "" {
		b.WriteString("\n\n" + dim.Render(m.wrap(m.info)))
	}
	if m.errMsg != "" {
		b.WriteString("\n\n" + m.th.Error.Render(m.wrap("✗ "+m.errMsg)))
	}
	b.WriteString("\n\n" + dim.Render(m.footerHint()))

	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	if m.th.Colored {
		box = box.BorderForeground(m.th.FocusBorderColor)
	}
	body := b.String()
	if m.width > 0 {
		return box.Width(m.contentWidth()).Render(body)
	}
	return box.Render(body)
}

func (m Model) wrap(s string) string {
	return ui.WrapText(s, m.contentWidth())
}

func (m Model) screenBody() string {
	switch m.screen {
	case screenChoose:
		return m.renderChoose()
	case screenAPIKey, screenCustomURL, screenCustomKey:
		return m.renderInputScreen()
	case screenModels:
		return m.renderModels()
	default:
		return ""
	}
}

func (m Model) renderChoose() string {
	var b strings.Builder
	b.WriteString(m.th.Primary.Render("Choose how to connect:") + "\n\n")
	for i, label := range choiceLabels {
		prefix := "  "
		line := label
		if i == m.cursor {
			prefix = m.th.Accent.Render("› ")
			line = m.th.Selected.Render(label)
		}
		b.WriteString(prefix + line + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) renderInputScreen() string {
	title := "API key"
	switch m.screen {
	case screenCustomURL:
		title = "Endpoint URL"
	case screenCustomKey:
		title = "API key / bearer token"
	}
	return m.th.Primary.Render(title) + "\n\n" + m.input.View()
}

func (m Model) renderModels() string {
	if m.loading {
		return m.th.Secondary.Render("Discovering models…")
	}
	if m.modelsErr != "" {
		return m.th.Error.Render("Could not list models: "+m.modelsErr) + "\n\n" +
			m.th.Secondary.Render("Check your key and endpoint, then press Esc to go back.")
	}
	if len(m.models) == 0 {
		return m.th.Secondary.Render("No models returned.")
	}

	var b strings.Builder
	b.WriteString(m.th.Primary.Render("Pick a model to start with:") + "\n\n")
	visible := m.visibleListRows()
	end := m.listOffset + visible
	if end > len(m.models) {
		end = len(m.models)
	}
	for i := m.listOffset; i < end; i++ {
		label := m.models[i]
		prefix := "  "
		if i == m.cursor {
			prefix = m.th.Accent.Render("› ")
			label = m.th.Selected.Render(label)
		}
		b.WriteString(prefix + label + "\n")
	}
	if rest := len(m.models) - end; rest > 0 {
		b.WriteString(m.th.Dim.Render(fmt.Sprintf("  … %d more", rest)) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) footerHint() string {
	switch m.screen {
	case screenChoose:
		return "↑/↓ select · Enter continue · Ctrl+C quit"
	case screenAPIKey, screenCustomURL, screenCustomKey:
		return "Enter submit · Esc back · Ctrl+C quit"
	case screenModels:
		if m.loading {
			return "Please wait…"
		}
		return "↑/↓ select · Enter confirm model · Esc back"
	default:
		return ""
	}
}
