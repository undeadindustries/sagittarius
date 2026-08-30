package settingsdialog

import (
	"fmt"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/ui"
	"github.com/undeadindustries/sagittarius/internal/ui/overlay"
)

// View renders the settings browser overlay.
func (m Model) View() string {
	dim := m.th.Dim
	var b strings.Builder
	b.WriteString(overlay.Title(m.th, "Settings") + "\n\n")

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
		b.WriteString("\n\n" + overlay.Hints(m.th, m.footerHint()))
	}

	return overlay.Frame(m.th, m.width, overlay.DefaultMinWidth, b.String())
}

func (m Model) viewList() string {
	var b strings.Builder
	b.WriteString(overlay.Hints(m.th, "* = overridden in this scope  •  Ctrl+L clear  •  Enter edit/toggle") + "\n\n")
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
	val, suffix := settingDisplay(e, m.scopeSel.Scope)
	if suffix != "" {
		val += dim.Render(suffix)
	}
	row := fmt.Sprintf("%s%-30s %s", star, label, val)
	if e.MergedValue != "" && e.MergedValue != e.Value {
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
	fmt.Fprintf(&b, "Edit: %s\n\n", e.Label)
	b.WriteString(m.input.View())
	b.WriteString("\n\n" + overlay.Hints(m.th, "Enter save • Esc cancel"))
	return b.String()
}

func (m Model) footerHint() string {
	base := "↑/↓ move • Enter edit/toggle • Ctrl+L clear • Esc close"
	if !m.scopeSel.Disabled {
		base += " • Tab · scope"
	}
	return base
}

func (m Model) wrap(s string) string {
	return ui.WrapText(s, overlay.ContentWidth(m.width, overlay.DefaultMinWidth))
}

const (
	suffixFromGlobal  = " (from global)"
	suffixFromProject = " (from project)"
	suffixDefault     = " (default)"
)

func settingDisplay(e SettingEntry, scope config.SettingScope) (value, suffix string) {
	if e.DefinedHere {
		return e.Value, ""
	}
	if e.Inherited {
		if scope == config.ScopeProject {
			return e.MergedValue, suffixFromGlobal
		}
		return e.MergedValue, suffixFromProject
	}
	if e.DefaultValue != "" {
		return e.DefaultValue, suffixDefault
	}
	return "", suffixDefault
}
