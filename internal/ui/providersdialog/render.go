package providersdialog

import (
	"fmt"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/ui"
	"github.com/undeadindustries/sagittarius/internal/ui/overlay"
)

// View renders the active dialog screen.
func (m Model) View() string {
	dim := m.th.Dim
	var b strings.Builder
	b.WriteString(m.th.Title.Render("Providers") + "\n\n")
	b.WriteString(m.screenBody())

	if m.info != "" {
		b.WriteString("\n\n" + dim.Render(m.wrap(m.info)))
	}
	if m.errMsg != "" {
		b.WriteString("\n\n" + m.th.Error.Render(m.wrap("✗ "+m.errMsg)))
	}
	b.WriteString("\n\n" + dim.Render(m.footerHint()))

	// Width is inner content only; border + padding add 4 cols (see overlay.ContentWidth).
	return overlay.Frame(m.th, m.width, overlay.DefaultMinWidth, b.String())
}

func (m Model) wrap(s string) string {
	return ui.WrapText(s, overlay.ContentWidth(m.width, overlay.DefaultMinWidth))
}

func (m Model) footerHint() string {
	switch m.screen {
	case screenEditField, screenSetKey, screenModelsAdd:
		return "Enter to save • Esc to cancel"
	case screenEditPick:
		return "↑/↓ move • Enter edit • a add • x remove custom • Esc close"
	case screenEdit:
		return "↑/↓ move • Enter select • r reset field • Esc back"
	case screenAdd:
		if m.add.fieldIdx == addFieldWire {
			return "←/→ toggle • Enter next • Esc cancel"
		}
		if m.add.fieldIdx == addFieldIdOverride {
			return "Edit id or Enter to accept • Esc cancel"
		}
		return "Enter next field • Esc cancel"
	case screenRemove:
		return "y / Enter confirm • Esc cancel"
	case screenAddModels:
		return "↑/↓ select • Enter choose • Esc back"
	case screenModels:
		return "↑/↓ move • Space toggle • A all/none • a add • Enter save • Esc back"
	default:
		return "↑/↓ move • Enter select • Esc back\nProvider keys and definitions are always saved globally"
	}
}

func (m Model) screenBody() string {
	switch m.screen {
	case screenEditPick:
		return m.renderProviderList(m.providers)
	case screenEdit:
		return m.renderEdit()
	case screenEditPicker:
		return m.renderPicker()
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
		return m.renderActivation(m.activationHeader())
	}
	return ""
}

// activationHeader builds a descriptive title for the model activation screen
// that shows the provider's display name, wire type, and an OpenRouter note.
func (m Model) activationHeader() string {
	displayID := config.ProviderDisplayID(m.targetID)
	wire := ""
	switch m.targetWire {
	case config.WireFormatGemini:
		wire = " (Gemini native API)"
	case config.WireFormatOpenAIChat:
		wire = " (OpenAI-compatible)"
	case config.WireFormatOpenAIResponses:
		wire = " (OpenAI Responses API)"
	}
	header := "Activate models on " + displayID + wire
	if m.targetID == "openrouter" {
		header += "\nNote: google/* routes here are OpenRouter endpoints, not native Gemini."
	}
	return header
}

func (m Model) renderProviderList(entries []ProviderEntry) string {
	dim := m.th.Dim
	if len(entries) == 0 {
		return dim.Render("No providers configured. Press a to add one.")
	}
	labels := make([]string, len(entries))
	for i, p := range entries {
		label := fmt.Sprintf("%s (%s)", p.DisplayID, p.DisplayName)
		if p.IsCustom {
			label += dim.Render(" [custom]")
		}
		labels[i] = label
	}
	return m.renderWindowedRows(labels)
}

func (m Model) renderEdit() string {
	dim := m.th.Dim
	header := fmt.Sprintf("Editing %s (%s)\n\n", config.ProviderDisplayID(m.targetID), m.targetWire)
	values := m.deps.ProviderSettings(m.targetID)
	eff := m.deps.EffectiveProviderSettings(m.targetID)
	labels := make([]string, len(m.editItems))
	for i, item := range m.editItems {
		label := item.label
		switch item.kind {
		case editPreset:
			label += dim.Render("  = " + m.presetLabel())
		case editWireDefn:
			label += dim.Render("  = " + string(m.targetWire))
		case editDefn:
			if values[item.key] != "" {
				label += dim.Render("  = " + values[item.key])
			}
		case editOverride, editEnum, editToggleBool:
			if v := values[item.key]; v != "" {
				label += dim.Render("  = " + v)
			} else if e := eff[item.key]; e != "" {
				label += dim.Render("  (default: " + e + ")")
			}
		}
		labels[i] = label
	}
	return header + m.renderWindowedRows(labels)
}

// presetLabel returns the display label for the provider's current system prompt
// preset, or "custom" when its personality/variant matches no preset.
func (m Model) presetLabel() string {
	id := m.deps.SystemPromptPresetID(m.targetID)
	if p, ok := config.LookupPreset(id); ok {
		return p.Label
	}
	return "custom"
}

func (m Model) renderPicker() string {
	dim := m.th.Dim
	var b strings.Builder
	b.WriteString(m.pickerTitle + "\n\n")
	if len(m.pickerOptions) == 0 {
		b.WriteString(dim.Render("(no options)"))
		return b.String()
	}
	labels := make([]string, len(m.pickerOptions))
	for i, opt := range m.pickerOptions {
		labels[i] = opt.label
	}
	b.WriteString(m.renderWindowedRows(labels))
	return b.String()
}

func (m Model) renderTextEntry(title string) string {
	return title + "\n\n" + m.input.View()
}

func (m Model) renderAdd() string {
	var b strings.Builder
	b.WriteString("Add custom provider\n\n")
	b.WriteString(m.th.Dim.Render(m.addSummary()) + "\n\n")
	if m.add.fieldIdx == addFieldWire {
		b.WriteString("Wire format: " + m.renderWireToggle(m.add.wire))
		return b.String()
	}
	b.WriteString(addFieldLabel(m.add.fieldIdx) + "\n" + m.input.View())
	return b.String()
}

func (m Model) addSummary() string {
	parts := []string{}
	if m.add.displayName != "" {
		parts = append(parts, "name="+m.add.displayName)
	}
	if m.add.hostOrURL != "" {
		url := m.add.hostOrURL
		if m.add.port != "" {
			url += ":" + m.add.port
		}
		parts = append(parts, "url="+url)
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
	dim := m.th.Dim
	var b strings.Builder
	b.WriteString(title + "\n\n")
	if m.loading {
		b.WriteString(dim.Render("Connecting and listing models…"))
		return b.String()
	}
	if m.modelsErr != "" {
		b.WriteString(m.th.Error.Render("✗ "+m.modelsErr) + "\n\n")
		b.WriteString(dim.Render("Esc to go back."))
		return b.String()
	}
	if len(m.models) == 0 {
		b.WriteString(dim.Render("No models returned by the endpoint."))
		if pickable {
			b.WriteString("\n" + dim.Render("Provider was added; set a model later from the /providers edit sheet."))
		}
		return b.String()
	}
	b.WriteString(m.renderScrollableRows(m.models, nil))
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) renderActivation(title string) string {
	dim := m.th.Dim
	var b strings.Builder
	b.WriteString(title + "\n\n")
	if m.loading {
		b.WriteString(dim.Render("Connecting and listing models…"))
		return b.String()
	}
	if m.modelsErr != "" {
		b.WriteString(m.th.Error.Render(m.wrap("✗ "+m.modelsErr)) + "\n\n")
		if len(m.models) > 0 {
			b.WriteString(dim.Render("Showing saved models — edit below or press a to add more.") + "\n\n")
		} else {
			b.WriteString(dim.Render("Press a to add a model name manually, or Esc to go back.") + "\n")
			return b.String()
		}
	}
	if len(m.models) == 0 {
		b.WriteString(dim.Render("No models yet. Press a to add a model name.") + "\n")
		return b.String()
	}
	b.WriteString(dim.Render("Checked models are active. Space toggles, A all/none, a adds, Enter saves.") + "\n\n")
	b.WriteString(m.renderScrollableRows(m.models, m.checked))
	return strings.TrimRight(b.String(), "\n")
}

// renderWindowedRows renders only the [start,end) slice of pre-built row labels
// that fits the terminal height, with "… N more above/below" indicators.
// Selection highlight uses the absolute index so it tracks m.cursor. Used by the
// row-list screens (provider list, edit sheet, enum picker) so a long list never
// overflows the overlay and pushes the top border off-screen.
func (m Model) renderWindowedRows(labels []string) string {
	total := len(labels)
	if total == 0 {
		return ""
	}
	start, end := m.listWindow(total)
	dim := m.th.Dim
	var b strings.Builder
	if start > 0 {
		b.WriteString(dim.Render(fmt.Sprintf("  … %d more above", start)) + "\n")
	}
	for i := start; i < end; i++ {
		b.WriteString(m.renderRow(labels[i], i == m.cursor) + "\n")
	}
	if end < total {
		b.WriteString(dim.Render(fmt.Sprintf("  … %d more below", total-end)) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// renderScrollableRows renders a window of list rows that fits the terminal height.
func (m Model) renderScrollableRows(labels []string, checked []bool) string {
	total := len(labels)
	if total == 0 {
		return ""
	}
	start, end := m.listWindow(total)
	dim := m.th.Dim
	var b strings.Builder
	if start > 0 {
		b.WriteString(dim.Render(fmt.Sprintf("  … %d more above", start)) + "\n")
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
		b.WriteString(m.renderRow(label, i == m.cursor) + "\n")
	}
	if end < total {
		b.WriteString(dim.Render(fmt.Sprintf("  … %d more below", total-end)) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) renderRemove() string {
	name := m.removeTarget
	if p, ok := m.findProvider(m.removeTarget); ok {
		name = p.DisplayName
		if name == "" {
			name = p.ID
		}
	}
	var b strings.Builder
	b.WriteString("Remove provider\n\n")
	b.WriteString(fmt.Sprintf("Provider: %s\n\n", name))
	b.WriteString(m.th.Dim.Render("This removes the provider definition, its instance settings, and its stored API key."))
	return b.String()
}

func (m Model) renderRow(label string, selected bool) string {
	return overlay.Row(m.th, label, selected)
}

func (m Model) renderWireToggle(wire config.WireFormat) string {
	sel := m.th.Accent
	dim := m.th.Dim
	chat := "openai-chat"
	responses := "openai-responses"
	if wire == config.WireFormatOpenAIChat {
		chat = sel.Render("[openai-chat]")
		responses = dim.Render(" openai-responses ")
	} else {
		chat = dim.Render(" openai-chat ")
		responses = sel.Render("[openai-responses]")
	}
	return chat + "  " + responses
}

func addFieldLabel(idx int) string {
	switch idx {
	case addFieldName:
		return "Provider name"
	case addFieldHostOrURL:
		return "URL or host  (e.g. http://127.0.0.1:8000  or  127.0.0.1)"
	case addFieldPort:
		return "Port  (default: 8000)"
	case addFieldEnvVar:
		return "API key env var  (optional)"
	case addFieldAPIKey:
		return "API key  (optional)"
	case addFieldIdOverride:
		return "Provider id  (auto-generated — edit to override)"
	}
	return ""
}
