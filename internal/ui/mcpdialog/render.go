package mcpdialog

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

// View renders the MCP server wizard overlay.
func (m Model) View() string {
	if m.saving {
		return m.wrapBox(m.viewSaving())
	}
	if m.reloading {
		return m.wrapBox(m.viewReloading())
	}

	var b strings.Builder
	switch m.screen {
	case screenForm:
		b.WriteString(m.viewForm())
	case screenField:
		b.WriteString(m.viewField())
	case screenDelete:
		b.WriteString(m.viewDelete())
	default:
		b.WriteString(m.viewList())
	}

	if m.info != "" {
		b.WriteString("\n\n" + m.th.Dim.Render(m.wrap(m.info)))
	}
	if m.errMsg != "" {
		b.WriteString("\n\n" + m.th.Error.Render(m.wrap("✗ "+m.errMsg)))
	}

	body := b.String()
	return m.wrapBox(body)
}

func (m Model) wrapBox(body string) string {
	box := m.boxStyle()
	if m.width > 0 {
		return box.Width(m.contentWidth()).Render(body)
	}
	return box.Render(body)
}

func (m Model) viewSaving() string {
	var b strings.Builder
	title := "Add MCP Server"
	if !m.adding {
		title = "Edit MCP Server: " + m.originalName
	}
	b.WriteString(m.th.Title.Render(title) + "\n\n")
	b.WriteString(m.spin.View() + " " + m.th.Dim.Render("Saving and reconnecting MCP servers…"))
	return b.String()
}

func (m Model) viewReloading() string {
	var b strings.Builder
	b.WriteString(m.th.Title.Render("MCP Servers") + "\n\n")
	b.WriteString(m.spin.View() + " " + m.th.Dim.Render("Reconnecting MCP servers…"))
	return b.String()
}

func (m Model) viewList() string {
	dim := m.th.Dim
	var b strings.Builder
	b.WriteString(m.th.Title.Render("MCP Servers") + "\n\n")
	if len(m.servers) == 0 {
		b.WriteString(dim.Render("No MCP servers configured.") + "\n")
	}
	for i, srv := range m.servers {
		label := srv.Name
		meta := []string{}
		if srv.Transport != "" {
			meta = append(meta, srv.Transport)
		}
		if srv.Disabled {
			meta = append(meta, "disabled")
		} else if srv.Status != "" {
			meta = append(meta, srv.Status)
		}
		if srv.ToolCount > 0 {
			meta = append(meta, fmt.Sprintf("%d tools", srv.ToolCount))
		}
		if !srv.Editable {
			meta = append(meta, srv.Source)
		}
		if len(meta) > 0 {
			label += dim.Render("  — " + strings.Join(meta, ", "))
		}
		b.WriteString(m.renderRow(label, i == m.listCursor) + "\n")
	}
	b.WriteString("\n" + dim.Render("↑/↓ move • Enter edit • a add • x remove • d disable • r reload • t tools • Esc close"))
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) viewForm() string {
	dim := m.th.Dim
	var b strings.Builder
	title := "Add MCP Server"
	if !m.adding {
		title = "Edit MCP Server: " + m.originalName
	}
	b.WriteString(m.th.Title.Render(title) + "\n\n")

	for i, id := range m.fields {
		b.WriteString(m.renderRow(m.fieldLabel(id), i == m.fieldCursor) + "\n")
	}
	if !m.scopeSel.Disabled {
		b.WriteString("\n" + m.scopeSel.View(m.th))
	}
	footerHint := "↑/↓ move • Enter edit/save • Space toggle • Esc back"
	if !m.scopeSel.Disabled {
		footerHint += " • Tab · scope"
	}
	b.WriteString("\n" + dim.Render(footerHint))
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) fieldLabel(id fieldID) string {
	switch id {
	case fName:
		return "Name: " + m.valueOrPlaceholder(m.form.Name)
	case fTransport:
		return "Transport: " + m.form.Transport
	case fCommand:
		return "Command: " + m.valueOrPlaceholder(m.form.Command)
	case fArgs:
		return "Args: " + m.valueOrPlaceholder(m.form.Args)
	case fURL:
		return "URL: " + m.valueOrPlaceholder(m.form.URL)
	case fEnv:
		return "Env (K=V,K=V): " + m.valueOrPlaceholder(m.form.Env)
	case fHeaders:
		return "Headers (K=V,K=V): " + m.valueOrPlaceholder(m.form.Headers)
	case fBearer:
		return "Bearer token: " + m.secretPlaceholder(m.form.Bearer)
	case fTimeout:
		return "Timeout ms: " + m.valueOrPlaceholder(m.form.Timeout)
	case fDescription:
		return "Description: " + m.valueOrPlaceholder(m.form.Description)
	case fTrust:
		return "Trust (skip confirmations): " + boolLabel(m.form.Trust)
	case fDisabled:
		return "Disabled: " + boolLabel(m.form.Disabled)
	case fSave:
		return "Save"
	}
	return ""
}

func (m Model) valueOrPlaceholder(v string) string {
	if strings.TrimSpace(v) == "" {
		return m.th.Dim.Render("(empty)")
	}
	return v
}

func (m Model) secretPlaceholder(v string) string {
	if strings.TrimSpace(v) == "" {
		return m.th.Dim.Render("(none — stored in keychain)")
	}
	return m.th.Dim.Render("(set — write-only)")
}

func boolLabel(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func (m Model) viewField() string {
	var b strings.Builder
	b.WriteString(m.th.Title.Render("Edit field") + "\n\n")
	b.WriteString(m.fieldEditLabel() + "\n\n")
	b.WriteString(m.input.View())
	b.WriteString("\n\n" + m.th.Dim.Render("Enter save • Esc cancel"))
	return b.String()
}

func (m Model) fieldEditLabel() string {
	switch m.editing {
	case fArgs:
		return "Space-separated arguments:"
	case fEnv, fHeaders:
		return "Comma-separated K=V pairs:"
	case fBearer:
		return "Bearer token (stored in keychain, not settings.json):"
	default:
		return "New value:"
	}
}

func (m Model) viewDelete() string {
	var b strings.Builder
	b.WriteString(m.th.Title.Render("Remove MCP Server") + "\n\n")
	b.WriteString(m.wrap(fmt.Sprintf("Remove %q? This deletes its settings entry and stored bearer token.", m.deleteName)))
	b.WriteString("\n\n" + m.th.Dim.Render("y/Enter confirm • n/Esc cancel"))
	return b.String()
}

func (m Model) renderRow(label string, selected bool) string {
	if selected {
		return m.th.Accent.Render("> " + label)
	}
	return "  " + label
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
	if w < 24 {
		return 24
	}
	return w
}
