package systempromptdialog

import (
	"fmt"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/customprompt"
	"github.com/undeadindustries/sagittarius/internal/ui/overlay"
)

// View renders the active screen, fitted to the terminal so the title and
// key hints stay visible.
func (m Model) View() string {
	switch m.screen {
	case screenManage:
		return m.viewManage()
	case screenName:
		return m.viewName()
	case screenContent:
		return m.viewContent()
	case screenConfirmDelete:
		return m.viewConfirmDelete()
	default:
		return m.viewPick()
	}
}

func (m Model) box() overlay.Box {
	return overlay.Box{Width: m.width, Height: m.height}
}

// infoLine prefixes the spinner while an operation runs.
func (m Model) infoLine() string {
	if m.info != "" && m.busy {
		return m.spin.View() + " " + m.info
	}
	return m.info
}

func (m Model) viewPick() string {
	var scopeLine string
	hints := "↑/↓ move • Enter apply • r clear • Esc close"
	if !m.scopeSel.Disabled {
		scopeLine = m.scopeSel.View(m.th)
		hints = "↑/↓ move • Enter apply • Tab scope • r clear • Esc close"
	} else {
		scopeLine = overlay.Hints(m.th, "Saved to ~/.sagittarius/settings.json (home directory)")
	}
	head := overlay.Title(m.th, "System Prompt") + "\n" + scopeLine + "\n"
	opts := m.pickOptions()
	rows := make([]string, len(opts))
	for i, o := range opts {
		rows[i] = overlay.Row(m.th, o.label, i == m.pickCursor)
	}
	foot := overlay.Foot(m.th, m.infoLine(), m.errMsg, hints)
	return overlay.FitFrame(m.th, m.box(),
		overlay.List{Head: head, Rows: rows, Foot: foot, Cursor: m.pickCursor})
}

func (m Model) viewManage() string {
	head := overlay.Title(m.th, "Custom Prompts") + "\n\n" +
		overlay.Hints(m.th, "Stored in ~/.sagittarius/prompts/ and shared by every workspace") + "\n"
	rows := make([]string, 0, m.manageRows())
	for i, name := range m.customs {
		rows = append(rows, overlay.Row(m.th, name, i == m.manageCursor))
	}
	rows = append(rows, overlay.Row(m.th, newPromptLabel, m.manageCursor == len(m.customs)))
	hints := "↑/↓ move • Enter edit • n new • r rename • d delete • Esc back"
	if len(m.customs) == 0 {
		hints = "Enter or n new prompt • Esc back"
	}
	foot := overlay.Foot(m.th, m.infoLine(), m.errMsg, hints)
	return overlay.FitFrame(m.th, m.box(),
		overlay.List{Head: head, Rows: rows, Foot: foot, Cursor: m.manageCursor})
}

func (m Model) viewName() string {
	title, hint := "New Custom Prompt", "Name the prompt. Spaces become hyphens."
	if m.renameFrom != "" {
		title, hint = "Rename "+m.renameFrom, "Settings that select this prompt are updated."
	}
	body := overlay.Title(m.th, title) + "\n\n" +
		overlay.Hints(m.th, hint) + "\n\n" +
		m.nameInput.View() + "\n" +
		overlay.Foot(m.th, m.infoLine(), m.errMsg, "Enter continue • Esc cancel")
	return m.frame(body)
}

func (m Model) viewContent() string {
	verb := "Edit"
	if m.creating {
		verb = "New"
	}
	title := fmt.Sprintf("%s Custom Prompt: %s", verb, m.editName)
	var hint, input, hints, count string
	if m.src == sourceImport {
		hint = "Import from a file (.md, .txt, or any UTF-8 text). The text is copied in."
		input = m.pathInput.View()
		hints = "Enter or Ctrl+S import • Tab paste instead • Esc cancel"
	} else {
		hint = "Paste or type the prompt. Sagittarius adds its tool and safety rules after it."
		input = m.body.View()
		hints = "Ctrl+S save • Tab import a file instead • Esc cancel"
		count = fmt.Sprintf("%d / %d bytes", len(m.body.Value()), customprompt.MaxPromptBytes)
	}
	info := m.infoLine()
	if info == "" {
		info = count
	}
	body := overlay.Title(m.th, title) + "\n\n" +
		overlay.Hints(m.th, hint) + "\n\n" +
		input + "\n" +
		overlay.Foot(m.th, info, m.errMsg, hints)
	return m.frame(body)
}

func (m Model) viewConfirmDelete() string {
	msg := fmt.Sprintf("Delete %s? If it is selected, Sagittarius uses the built-in default instead.", m.deleteName)
	body := overlay.Title(m.th, "Delete Custom Prompt") + "\n\n" + msg + "\n" +
		overlay.Foot(m.th, m.infoLine(), m.errMsg, "y delete • any other key cancel")
	return m.frame(body)
}

// frame draws a non-list screen and clips it to the terminal height.
func (m Model) frame(body string) string {
	view := overlay.Frame(m.th, m.width, overlay.DefaultMinWidth, strings.TrimRight(body, "\n"))
	return overlay.FitHeight(m.th, view, m.height)
}
