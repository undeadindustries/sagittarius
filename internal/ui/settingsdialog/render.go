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
	if m.editing {
		body := overlay.Title(m.th, "Settings") + "\n\n" + m.viewEdit() + m.statusLines()
		return overlay.Frame(m.th, m.width, overlay.DefaultMinWidth, body)
	}
	var head strings.Builder
	head.WriteString(overlay.Title(m.th, "Settings") + "\n")
	if !m.scopeSel.Disabled {
		head.WriteString(m.scopeSel.View(m.th) + "\n")
	}
	head.WriteString("\n" + overlay.Hints(m.th, "* = overridden in this scope  •  Ctrl+L clear  •  Enter edit/toggle") + "\n")

	rows := make([]string, len(m.entries))
	for i, e := range m.entries {
		rows[i] = m.renderEntry(e, i == m.cursor)
	}
	foot := "\n" + overlay.Hints(m.th, m.footerHint())
	if s := m.statusLines(); s != "" {
		foot = strings.TrimPrefix(s, "\n") + "\n" + foot
	}
	return overlay.FitFrame(m.th, overlay.Box{Width: m.width, Height: m.height},
		overlay.List{Head: head.String(), Rows: rows, Foot: foot, Cursor: m.cursor})
}

// statusLines renders the info and error messages, each preceded by a blank
// line, or "" when there are none.
func (m Model) statusLines() string {
	var b strings.Builder
	if m.info != "" {
		b.WriteString("\n\n" + m.th.Dim.Render(m.wrap(m.info)))
	}
	if m.errMsg != "" {
		b.WriteString("\n\n" + m.th.Error.Render(m.wrap("✗ "+m.errMsg)))
	}
	return b.String()
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
	// A secret has no scope value to compare against; its whole state is the
	// status text settingDisplay already returned.
	if e.Kind != KindSecret && e.MergedValue != "" && e.MergedValue != e.Value {
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
	hint := "Enter save • Esc cancel"
	if e.Kind == KindSecret {
		hint = "Enter save to secure storage • Esc cancel • input hidden"
	}
	b.WriteString("\n\n" + overlay.Hints(m.th, hint))
	return b.String()
}

func (m Model) footerHint() string {
	return "↑/↓ move • Enter edit/toggle • Ctrl+L clear • Esc close"
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
	if e.Kind == KindSecret {
		if e.StatusText == "" {
			return statusChecking, ""
		}
		return e.StatusText, ""
	}
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
