package systempromptdialog

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/customprompt"
	"github.com/undeadindustries/sagittarius/internal/ui/overlay"
	"github.com/undeadindustries/sagittarius/internal/ui/theme"
)

type screen int

const (
	// screenPick lists built-in presets, saved custom prompts, and the
	// Custom prompts row.
	screenPick screen = iota
	// screenManage lists saved custom prompts plus New prompt.
	screenManage
	// screenName asks for a name, for a new prompt or a rename.
	screenName
	// screenContent takes the prompt text by paste or by file import.
	screenContent
	// screenConfirmDelete asks y/N before deleting a prompt.
	screenConfirmDelete
)

type source int

const (
	sourcePaste source = iota
	sourceImport
)

const (
	customLabelSuffix = " (custom)"
	manageRowLabel    = "Custom prompts…"
	newPromptLabel    = "New prompt…"

	// Rows the content screen spends outside the text area: frame border,
	// title, hint, blank lines, byte count, error, and key hints.
	contentChromeRows = 12
	minEditorRows     = 3
	defaultEditorCols = 60
	defaultEditorRows = 8
)

type optionKind int

const (
	optionPreset optionKind = iota
	optionCustom
	optionManage
)

type pickOption struct {
	id    string
	label string
	kind  optionKind
}

type presetOption struct {
	id    string
	label string
}

// Model is the system-prompt picker overlay and its custom prompt manager.
type Model struct {
	deps Deps
	ctx  context.Context
	th   theme.Theme

	width  int
	height int

	screen    screen
	presets   []presetOption
	customs   []string
	currentID string
	// listed is set once the first custom prompt list arrives, so the pick
	// cursor moves to a selected custom prompt only on open.
	listed       bool
	pickCursor   int
	manageCursor int

	done   bool
	status string
	errMsg string
	info   string

	spin spinner.Model
	busy bool

	nameInput  textinput.Model
	renameFrom string
	editName   string
	creating   bool
	src        source
	body       textarea.Model
	pathInput  textinput.Model
	deleteName string
}

// applyResultMsg carries the outcome of an off-Update ApplyPreset call (which
// reloads the system instruction and rebuilds the runner) so it never blocks the
// UI loop.
type applyResultMsg struct {
	info  string
	label string
	err   error
}

// customListMsg carries the saved custom prompt names read off Update.
type customListMsg struct {
	names []string
	err   error
}

// bodyLoadedMsg carries a prompt's text loaded for editing.
type bodyLoadedMsg struct {
	name string
	body string
	err  error
}

// opDoneMsg carries the result of a save, import, rename, or delete.
type opDoneMsg struct {
	info string
	err  error
}

// New constructs the picker. The returned command lists saved custom prompts;
// the caller must run it.
func New(ctx context.Context, deps Deps) (Model, tea.Cmd) {
	presets := config.SortedSystemPromptPresets()
	opts := make([]presetOption, len(presets))
	for i, p := range presets {
		opts[i] = presetOption{id: p.ID, label: p.Label}
	}
	m := Model{
		deps:      deps,
		ctx:       ctx,
		th:        theme.Default(),
		presets:   opts,
		currentID: deps.CurrentPresetID(),
		spin:      spinner.New(spinner.WithSpinner(spinner.MiniDot)),
	}
	for i, o := range opts {
		if o.id == m.currentID {
			m.pickCursor = i
			break
		}
	}
	return m, m.listCmd()
}

// Done reports whether the dialog has finished.
func (m Model) Done() bool { return m.done }

// Status returns a one-line message to surface after close.
func (m Model) Status() string { return m.status }

// SetSize informs the dialog of the terminal dimensions.
func (m Model) SetSize(w, h int) Model {
	m.width = w
	m.height = h
	// The textarea exists only on the content screen; a zero textarea.Model
	// has a nil viewport and must not be sized.
	if m.screen == screenContent {
		m.body = m.sizeEditor(m.body)
	}
	if m.width > 0 {
		m.pathInput.Width = m.textCols() - len(m.pathInput.Prompt) - 1
		m.nameInput.Width = m.textCols() - len(m.nameInput.Prompt) - 1
	}
	return m
}

// SetTheme applies the resolved color theme.
func (m Model) SetTheme(th theme.Theme) Model {
	m.th = th
	return m
}

func (m Model) pickOptions() []pickOption {
	out := make([]pickOption, 0, len(m.presets)+len(m.customs)+1)
	for _, p := range m.presets {
		out = append(out, pickOption{id: p.id, label: p.label, kind: optionPreset})
	}
	for _, name := range m.customs {
		out = append(out, pickOption{id: name, label: name + customLabelSuffix, kind: optionCustom})
	}
	return append(out, pickOption{label: manageRowLabel, kind: optionManage})
}

// manageRows is the custom prompt names followed by the New prompt row.
func (m Model) manageRows() int { return len(m.customs) + 1 }

func (m Model) selectedCustom() (string, bool) {
	if m.manageCursor >= 0 && m.manageCursor < len(m.customs) {
		return m.customs[m.manageCursor], true
	}
	return "", false
}

// Update advances the dialog for one message.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if !m.busy {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case applyResultMsg:
		return m.handleApplyResult(msg)
	case customListMsg:
		return m.handleList(msg), nil
	case bodyLoadedMsg:
		return m.handleBodyLoaded(msg)
	case opDoneMsg:
		return m.handleOpDone(msg)
	}
	if m.busy {
		return m, nil
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m.forwardToInput(msg)
	}
	switch m.screen {
	case screenManage:
		return m.updateManage(key)
	case screenName:
		return m.updateName(key)
	case screenContent:
		return m.updateContent(key)
	case screenConfirmDelete:
		return m.updateConfirmDelete(key)
	default:
		return m.updatePick(key)
	}
}

// forwardToInput passes non-key messages (cursor blink) to the focused input.
func (m Model) forwardToInput(msg tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.screen {
	case screenName:
		m.nameInput, cmd = m.nameInput.Update(msg)
	case screenContent:
		if m.src == sourceImport {
			m.pathInput, cmd = m.pathInput.Update(msg)
		} else {
			m.body, cmd = m.body.Update(msg)
		}
	}
	return m, cmd
}

func (m Model) updatePick(key tea.KeyMsg) (Model, tea.Cmd) {
	opts := m.pickOptions()
	switch key.String() {
	case "esc", "q":
		m.done = true
	case "up", "k":
		m.pickCursor = (m.pickCursor - 1 + len(opts)) % len(opts)
	case "down", "j":
		m.pickCursor = (m.pickCursor + 1) % len(opts)
	case "enter":
		if m.pickCursor < 0 || m.pickCursor >= len(opts) {
			return m, nil
		}
		o := opts[m.pickCursor]
		if o.kind == optionManage {
			m.screen = screenManage
			m.manageCursor = 0
			m.errMsg, m.info = "", ""
			return m, nil
		}
		return m.apply(o)
	}
	return m, nil
}

func (m Model) updateManage(key tea.KeyMsg) (Model, tea.Cmd) {
	rows := m.manageRows()
	switch key.String() {
	case "esc", "q":
		m.screen = screenPick
		m.errMsg = ""
	case "up", "k":
		m.manageCursor = (m.manageCursor - 1 + rows) % rows
	case "down", "j":
		m.manageCursor = (m.manageCursor + 1) % rows
	case "n":
		return m.startName("")
	case "enter":
		if name, ok := m.selectedCustom(); ok {
			return m.startEdit(name)
		}
		return m.startName("")
	case "r":
		if name, ok := m.selectedCustom(); ok {
			return m.startName(name)
		}
	case "d":
		if name, ok := m.selectedCustom(); ok {
			m.deleteName = name
			m.screen = screenConfirmDelete
			m.errMsg, m.info = "", ""
		}
	}
	return m, nil
}

func (m Model) updateName(key tea.KeyMsg) (Model, tea.Cmd) {
	switch key.String() {
	case "esc":
		m.screen = screenManage
		m.errMsg = ""
		return m, nil
	case "enter":
		return m.submitName()
	}
	var cmd tea.Cmd
	m.nameInput, cmd = m.nameInput.Update(key)
	return m, cmd
}

func (m Model) submitName() (Model, tea.Cmd) {
	name, err := customprompt.NormalizeName(m.nameInput.Value())
	if err != nil {
		m.errMsg = err.Error()
		return m, nil
	}
	if m.renameFrom != "" {
		if name == m.renameFrom {
			m.screen = screenManage
			m.errMsg = ""
			return m, nil
		}
		from := m.renameFrom
		return m.runOp(fmt.Sprintf("Renaming %s…", from), func(ctx context.Context, d Deps) (string, error) {
			return d.RenameCustomPrompt(ctx, from, name)
		})
	}
	for _, c := range m.customs {
		if c == name {
			m.errMsg = fmt.Sprintf("%s already exists. Choose another name, or edit %s from the list.", name, name)
			return m, nil
		}
	}
	m.editName = name
	m.creating = true
	return m.openContent("")
}

func (m Model) updateContent(key tea.KeyMsg) (Model, tea.Cmd) {
	switch key.String() {
	case "esc":
		m.screen = screenManage
		m.errMsg = ""
		return m, nil
	case "tab":
		return m.toggleSource()
	case "ctrl+s":
		return m.saveContent()
	case "enter":
		if m.src == sourceImport {
			return m.saveContent()
		}
	}
	var cmd tea.Cmd
	if m.src == sourceImport {
		m.pathInput, cmd = m.pathInput.Update(key)
	} else {
		m.body, cmd = m.body.Update(key)
	}
	return m, cmd
}

func (m Model) toggleSource() (Model, tea.Cmd) {
	m.errMsg = ""
	if m.src == sourcePaste {
		m.src = sourceImport
		m.body.Blur()
		return m, m.pathInput.Focus()
	}
	m.src = sourcePaste
	m.pathInput.Blur()
	return m, m.body.Focus()
}

func (m Model) saveContent() (Model, tea.Cmd) {
	name, create := m.editName, m.creating
	if m.src == sourceImport {
		path := strings.TrimSpace(m.pathInput.Value())
		if path == "" {
			m.errMsg = "Enter the path of a text or Markdown file to import."
			return m, nil
		}
		return m.runOp(fmt.Sprintf("Importing %s…", path), func(ctx context.Context, d Deps) (string, error) {
			return d.ImportCustomPrompt(ctx, name, path, create)
		})
	}
	text := m.body.Value()
	if _, err := customprompt.ValidateBody(text); err != nil {
		m.errMsg = err.Error()
		return m, nil
	}
	return m.runOp(fmt.Sprintf("Saving %s…", name), func(ctx context.Context, d Deps) (string, error) {
		return d.SaveCustomPrompt(ctx, name, text, create)
	})
}

func (m Model) updateConfirmDelete(key tea.KeyMsg) (Model, tea.Cmd) {
	if key.String() != "y" && key.String() != "Y" {
		m.screen = screenManage
		return m, nil
	}
	name := m.deleteName
	return m.runOp(fmt.Sprintf("Deleting %s…", name), func(ctx context.Context, d Deps) (string, error) {
		return d.DeleteCustomPrompt(ctx, name)
	})
}

// startName opens the name screen. An empty from starts a new prompt;
// otherwise it renames from.
func (m Model) startName(from string) (Model, tea.Cmd) {
	m.renameFrom = from
	m.nameInput = m.newTextInput("lowercase letters, digits, and hyphens")
	m.nameInput.CharLimit = customprompt.MaxNameLen
	m.nameInput.SetValue(from)
	m.nameInput.CursorEnd()
	m.screen = screenName
	m.errMsg, m.info = "", ""
	return m, m.nameInput.Focus()
}

// startEdit loads a prompt's text off Update, then opens the content screen.
func (m Model) startEdit(name string) (Model, tea.Cmd) {
	m.editName = name
	m.creating = false
	m.errMsg = ""
	m.busy = true
	m.info = fmt.Sprintf("Loading %s…", name)
	deps := m.deps
	return m, tea.Batch(m.spin.Tick, func() tea.Msg {
		body, err := deps.LoadCustomPrompt(name)
		return bodyLoadedMsg{name: name, body: body, err: err}
	})
}

func (m Model) openContent(text string) (Model, tea.Cmd) {
	m.body = m.newEditor()
	m.body.SetValue(text)
	m.pathInput = m.newTextInput("~/prompts/reviewer.md")
	m.pathInput.CharLimit = 4096
	m.src = sourcePaste
	m.screen = screenContent
	m.errMsg, m.info = "", ""
	return m, m.body.Focus()
}

func (m Model) newTextInput(placeholder string) textinput.Model {
	ti := textinput.New()
	ti.Prompt = "> "
	ti.Placeholder = placeholder
	overlay.StyleTextInput(&ti, m.th)
	if m.width > 0 {
		ti.Width = m.textCols() - len(ti.Prompt) - 1
	}
	return ti
}

func (m Model) newEditor() textarea.Model {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.Placeholder = "Paste or type the system prompt."
	ta.CharLimit = customprompt.MaxPromptBytes
	// The default 99-line cap would silently drop the tail of a long paste.
	ta.MaxHeight = 0
	ta.MaxWidth = 0
	plain := lipgloss.NewStyle()
	ta.FocusedStyle.CursorLine = plain
	ta.FocusedStyle.Placeholder = m.th.Dim
	ta.BlurredStyle.Placeholder = m.th.Dim
	return m.sizeEditor(ta)
}

func (m Model) sizeEditor(ta textarea.Model) textarea.Model {
	cols, rows := defaultEditorCols, defaultEditorRows
	if m.width > 0 {
		cols = m.textCols()
	}
	if m.height > 0 {
		rows = max(m.height-contentChromeRows, minEditorRows)
	}
	ta.SetWidth(cols)
	ta.SetHeight(rows)
	return ta
}

// textCols is the usable text width inside the overlay frame.
func (m Model) textCols() int {
	return max(overlay.ContentWidth(m.width, overlay.DefaultMinWidth)-2, 1)
}

func (m Model) listCmd() tea.Cmd {
	deps := m.deps
	return func() tea.Msg {
		names, err := deps.ListCustomPrompts()
		return customListMsg{names: names, err: err}
	}
}

// runOp starts a store operation off Update and shows a spinner until it ends.
func (m Model) runOp(info string, op func(context.Context, Deps) (string, error)) (Model, tea.Cmd) {
	m.busy = true
	m.errMsg = ""
	m.info = info
	ctx, deps := m.ctx, m.deps
	return m, tea.Batch(m.spin.Tick, func() tea.Msg {
		msg, err := op(ctx, deps)
		return opDoneMsg{info: msg, err: err}
	})
}

func (m Model) apply(o pickOption) (Model, tea.Cmd) {
	ctx, deps := m.ctx, m.deps
	m.busy = true
	m.errMsg = ""
	m.info = fmt.Sprintf("Applying %s…", o.label)
	return m, tea.Batch(
		m.spin.Tick,
		func() tea.Msg {
			info, err := deps.ApplyPreset(ctx, o.id)
			return applyResultMsg{info: info, label: o.label, err: err}
		},
	)
}

func (m Model) handleList(msg customListMsg) Model {
	if msg.err != nil {
		m.errMsg = "Could not list custom prompts: " + msg.err.Error()
		return m
	}
	m.customs = msg.names
	if !m.listed {
		m.listed = true
		for i, name := range m.customs {
			if name == m.currentID {
				m.pickCursor = len(m.presets) + i
				break
			}
		}
	}
	m.pickCursor = min(m.pickCursor, len(m.pickOptions())-1)
	m.manageCursor = min(m.manageCursor, m.manageRows()-1)
	return m
}

func (m Model) handleBodyLoaded(msg bodyLoadedMsg) (Model, tea.Cmd) {
	m.busy = false
	m.info = ""
	if msg.err != nil {
		m.errMsg = msg.err.Error()
		return m, nil
	}
	return m.openContent(msg.body)
}

func (m Model) handleOpDone(msg opDoneMsg) (Model, tea.Cmd) {
	m.busy = false
	if msg.err != nil {
		m.info = ""
		m.errMsg = msg.err.Error()
		if m.screen == screenConfirmDelete {
			m.screen = screenManage
		}
		return m, nil
	}
	m.info = msg.info
	m.errMsg = ""
	m.screen = screenManage
	return m, m.listCmd()
}

func (m Model) handleApplyResult(msg applyResultMsg) (Model, tea.Cmd) {
	m.busy = false
	if msg.err != nil {
		m.errMsg = msg.err.Error()
		m.info = ""
		return m, nil
	}
	m.info = msg.info
	m.status = fmt.Sprintf("System prompt → %s", msg.label)
	m.done = true
	return m, nil
}
