package providersdialog

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true)
	selectedStyle = lipgloss.NewStyle().Bold(true)
	dimStyle      = lipgloss.NewStyle().Faint(true)
	errStyle      = lipgloss.NewStyle().Bold(true)
	boxStyle      = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			Padding(0, 1)
)

// View renders the active dialog screen.
func (m Model) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Providers") + "\n\n")
	b.WriteString(m.screenBody())

	if m.info != "" {
		b.WriteString("\n\n" + dimStyle.Render(m.wrap(m.info)))
	}
	if m.errMsg != "" {
		b.WriteString("\n\n" + errStyle.Render(m.wrap("✗ "+m.errMsg)))
	}
	b.WriteString("\n\n" + dimStyle.Render(m.footerHint()))

	body := b.String()
	if m.width > 0 {
		// Width is inner content only; border + padding add 4 cols (see contentWidth).
		return boxStyle.Width(m.contentWidth()).Render(body)
	}
	return boxStyle.Render(body)
}

func (m Model) wrap(s string) string {
	return ui.WrapText(s, m.contentWidth())
}

func (m Model) footerHint() string {
	switch m.screen {
	case screenEditField, screenSetKey, screenModelsAdd:
		return "Enter to save • Esc to cancel"
	case screenAdd:
		if m.add.fieldIdx == addFieldWire {
			return "←/→ toggle • Enter next • Esc cancel"
		}
		return "Enter next field • Esc cancel"
	case screenAddModels:
		return "↑/↓ select • Enter choose • Esc back"
	case screenModels:
		return "↑/↓ move • Space toggle • A all/none • a add • Enter save • Esc back"
	default:
		return "↑/↓ move • Enter select • Esc back"
	}
}

func (m Model) screenBody() string {
	switch m.screen {
	case screenMenu:
		return m.renderMenu()
	case screenSwitch:
		return m.renderProviderList("Switch active provider", m.providers)
	case screenEditPick:
		return m.renderProviderList("Edit which provider?", m.providers)
	case screenEdit:
		return m.renderEdit()
	case screenEditField:
		return m.renderTextEntry(fmt.Sprintf("Set %s for %s", m.editingKey, config.ProviderDisplayID(m.targetID)))
	case screenSetKey:
		return m.renderTextEntry(fmt.Sprintf("Set API key for %s\n(Paste your key, then Enter — field starts blank)", config.ProviderDisplayID(m.targetID)))
	case screenModelsAdd:
		return m.renderTextEntry("Add model name")
	case screenAdd:
		return m.renderAdd()
	case screenAddModels:
		return m.renderModels("Select a default model for "+config.ProviderDisplayID(m.targetID), true)
	case screenRemove:
		return m.renderRemove()
	case screenModels:
		return m.renderActivation("Activate models on " + config.ProviderDisplayID(m.targetID))
	}
	return ""
}

func (m Model) renderMenu() string {
	var b strings.Builder
	active := m.deps.ActiveProviderID()
	if active != "" {
		b.WriteString(dimStyle.Render("Active: "+config.ProviderDisplayID(active)) + "\n\n")
	}
	for i, item := range m.menuItems() {
		b.WriteString(renderRow(item.label, i == m.cursor) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) renderProviderList(title string, entries []ProviderEntry) string {
	var b strings.Builder
	b.WriteString(title + "\n\n")
	if len(entries) == 0 {
		b.WriteString(dimStyle.Render("(none)"))
		return b.String()
	}
	for i, p := range entries {
		label := fmt.Sprintf("%s (%s)", p.DisplayID, p.DisplayName)
		marker := ""
		if p.IsActive {
			marker = " — active"
		}
		if p.IsCustom {
			marker += " [custom]"
		}
		b.WriteString(renderRow(label+dimStyle.Render(marker), i == m.cursor) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) renderEdit() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Editing %s (%s)\n\n", config.ProviderDisplayID(m.targetID), m.targetWire))
	values := m.deps.ProviderSettings(m.targetID)
	for i, item := range m.editItems {
		label := item.label
		if (item.kind == editOverride || item.kind == editDefn) && values[item.key] != "" {
			label += dimStyle.Render("  = " + values[item.key])
		}
		if item.kind == editWireDefn {
			label += dimStyle.Render("  = " + string(m.targetWire))
		}
		b.WriteString(renderRow(label, i == m.cursor) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) renderTextEntry(title string) string {
	return title + "\n\n" + m.input.View()
}

func (m Model) renderAdd() string {
	var b strings.Builder
	b.WriteString("Add custom provider\n\n")
	b.WriteString(dimStyle.Render(m.addSummary()) + "\n\n")
	if m.add.fieldIdx == addFieldWire {
		b.WriteString("wireFormat: " + renderWireToggle(m.add.wire))
		return b.String()
	}
	b.WriteString(addFieldLabel(m.add.fieldIdx) + "\n" + m.input.View())
	return b.String()
}

func (m Model) addSummary() string {
	parts := []string{}
	if m.add.id != "" {
		parts = append(parts, "id="+m.add.id)
	}
	if m.add.displayName != "" {
		parts = append(parts, "name="+m.add.displayName)
	}
	if m.add.baseURL != "" {
		parts = append(parts, "url="+m.add.baseURL)
	}
	if m.add.fieldIdx >= addFieldEnvVar {
		parts = append(parts, "wire="+string(m.add.wire))
	}
	if len(parts) == 0 {
		return "(new provider)"
	}
	return strings.Join(parts, "  ")
}

func (m Model) renderModels(title string, pickable bool) string {
	var b strings.Builder
	b.WriteString(title + "\n\n")
	if m.loading {
		b.WriteString(dimStyle.Render("Connecting and listing models…"))
		return b.String()
	}
	if m.modelsErr != "" {
		b.WriteString(errStyle.Render("✗ "+m.modelsErr) + "\n\n")
		b.WriteString(dimStyle.Render("Esc to go back."))
		return b.String()
	}
	if len(m.models) == 0 {
		b.WriteString(dimStyle.Render("No models returned by the endpoint."))
		if pickable {
			b.WriteString("\n" + dimStyle.Render("Provider was added; set a model later from the /providers edit sheet."))
		}
		return b.String()
	}
	b.WriteString(m.renderScrollableRows(m.models, nil))
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) renderActivation(title string) string {
	var b strings.Builder
	b.WriteString(title + "\n\n")
	if m.loading {
		b.WriteString(dimStyle.Render("Connecting and listing models…"))
		return b.String()
	}
	if m.modelsErr != "" {
		b.WriteString(errStyle.Render(m.wrap("✗ "+m.modelsErr)) + "\n\n")
		if len(m.models) > 0 {
			b.WriteString(dimStyle.Render("Showing saved models — edit below or press a to add more.") + "\n\n")
		} else {
			b.WriteString(dimStyle.Render("Press a to add a model name manually, or Esc to go back.") + "\n")
			return b.String()
		}
	}
	if len(m.models) == 0 {
		b.WriteString(dimStyle.Render("No models yet. Press a to add a model name.") + "\n")
		return b.String()
	}
	b.WriteString(dimStyle.Render("Checked models are active. Space toggles, A all/none, a adds, Enter saves.") + "\n\n")
	b.WriteString(m.renderScrollableRows(m.models, m.checked))
	return strings.TrimRight(b.String(), "\n")
}

// renderScrollableRows renders a window of list rows that fits the terminal height.
func (m Model) renderScrollableRows(labels []string, checked []bool) string {
	total := len(labels)
	if total == 0 {
		return ""
	}
	start, end := m.listWindow(total)
	var b strings.Builder
	if start > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  … %d more above", start)) + "\n")
	}
	for i := start; i < end; i++ {
		label := labels[i]
		if checked != nil {
			box := "[ ]"
			if i < len(checked) && checked[i] {
				box = "[x]"
			}
			label = box + " " + label
		}
		b.WriteString(renderRow(label, i == m.cursor) + "\n")
	}
	if end < total {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  … %d more below", total-end)) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) renderRemove() string {
	customs := m.customProviders()
	var b strings.Builder
	b.WriteString("Remove a custom provider\n\n")
	if len(customs) == 0 {
		b.WriteString(dimStyle.Render("No custom providers to remove."))
		return b.String()
	}
	for i, p := range customs {
		b.WriteString(renderRow(fmt.Sprintf("%s (%s)", p.DisplayID, p.DisplayName), i == m.cursor) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderRow(label string, selected bool) string {
	if selected {
		return selectedStyle.Render("> " + label)
	}
	return "  " + label
}

func renderWireToggle(wire config.WireFormat) string {
	chat := "openai-chat"
	responses := "openai-responses"
	if wire == config.WireFormatOpenAIChat {
		chat = selectedStyle.Render("[openai-chat]")
		responses = dimStyle.Render(" openai-responses ")
	} else {
		chat = dimStyle.Render(" openai-chat ")
		responses = selectedStyle.Render("[openai-responses]")
	}
	return chat + "  " + responses
}

func addFieldLabel(idx int) string {
	switch idx {
	case addFieldID:
		return "Provider id"
	case addFieldName:
		return "Display name"
	case addFieldBaseURL:
		return "Base URL"
	case addFieldEnvVar:
		return "API key env var (optional)"
	case addFieldAPIKey:
		return "API key (optional)"
	}
	return ""
}
