package mcpdialog

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/undeadindustries/sagittarius/internal/ui/theme"
)

type screen int

const (
	screenList   screen = iota // server list
	screenForm                 // add/edit field list
	screenField                // single text/secret field editor
	screenDelete               // delete confirmation
)

type fieldID int

const (
	fName fieldID = iota
	fTransport
	fCommand
	fArgs
	fURL
	fEnv
	fHeaders
	fBearer
	fTimeout
	fDescription
	fTrust
	fDisabled
	fSave
)

type fieldKind int

const (
	kindText fieldKind = iota
	kindSecret
	kindTransport
	kindToggle
	kindAction
)

// Model is the /mcp server management overlay.
type Model struct {
	deps Deps
	ctx  context.Context
	th   theme.Theme

	width  int
	height int

	screen     screen
	servers    []ServerEntry
	listCursor int

	// form state
	adding       bool
	originalName string
	form         ServerForm
	fields       []fieldID
	fieldCursor  int

	// field editor state
	input   textinput.Model
	editing fieldID

	deleteName string

	done      bool
	openTools bool
	status    string
	errMsg    string
	info      string

	saving    bool
	reloading bool
	spin      spinner.Model
}

type saveResultMsg struct {
	err    error
	adding bool
	name   string
}

type reloadResultMsg struct {
	err    error
	status string
}

// New constructs the MCP server wizard.
func New(ctx context.Context, deps Deps) Model {
	m := Model{
		deps:   deps,
		ctx:    ctx,
		th:     theme.Default(),
		screen: screenList,
		spin:   newDialogSpinner(theme.Default()),
	}
	m.servers = deps.ListServers()
	return m
}

// Done reports whether the dialog has finished and should be removed.
func (m Model) Done() bool { return m.done }

// OpenTools reports whether the dialog asked to switch to the /tools inventory.
func (m Model) OpenTools() bool { return m.openTools }

// Status returns a one-line message to surface after the dialog closes.
func (m Model) Status() string { return m.status }

// SetSize informs the dialog of the terminal dimensions.
func (m Model) SetSize(w, h int) Model {
	m.width = w
	m.height = h
	return m
}

// SetTheme applies the resolved color theme to the overlay.
func (m Model) SetTheme(th theme.Theme) Model {
	m.th = th
	m.spin = newDialogSpinner(th)
	return m
}

// Update advances the wizard for one message.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if !m.busy() {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case saveResultMsg:
		return m.handleSaveResult(msg)
	case reloadResultMsg:
		return m.handleReloadResult(msg)
	}

	if m.busy() {
		return m, nil
	}

	if m.screen == screenField {
		return m.updateField(msg)
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch m.screen {
	case screenList:
		return m.updateList(key)
	case screenForm:
		return m.updateForm(key)
	case screenDelete:
		return m.updateDelete(key)
	}
	return m, nil
}

func (m Model) updateList(key tea.KeyMsg) (Model, tea.Cmd) {
	switch key.String() {
	case "esc", "q":
		m.done = true
		return m, nil
	case "up", "k":
		m.listCursor = wrapDec(m.listCursor, len(m.servers))
		return m, nil
	case "down", "j":
		m.listCursor = wrapInc(m.listCursor, len(m.servers))
		return m, nil
	case "a":
		m.startAdd()
		return m, nil
	case "enter":
		return m.startEdit()
	case "x":
		return m.startDelete()
	case "d":
		return m.toggleDisabled()
	case "r":
		return m.reload()
	case "t":
		m.openTools = true
		m.done = true
		return m, nil
	}
	return m, nil
}

func (m *Model) startAdd() {
	m.adding = true
	m.originalName = ""
	m.form = ServerForm{Transport: TransportStdio}
	m.fieldCursor = 0
	m.errMsg = ""
	m.info = ""
	m.rebuildFields()
	m.screen = screenForm
}

func (m Model) startEdit() (Model, tea.Cmd) {
	srv, ok := m.currentServer()
	if !ok {
		return m, nil
	}
	if !srv.Editable {
		m.info = fmt.Sprintf("%s is provided by %s and cannot be edited here.", srv.Name, srv.Source)
		return m, nil
	}
	form, ok := m.deps.GetServer(srv.Name)
	if !ok {
		m.errMsg = fmt.Sprintf("server %q not found", srv.Name)
		return m, nil
	}
	if form.Transport == "" {
		form.Transport = TransportStdio
	}
	m.adding = false
	m.originalName = srv.Name
	m.form = form
	m.fieldCursor = 0
	m.errMsg = ""
	m.info = ""
	m.rebuildFields()
	m.screen = screenForm
	return m, nil
}

func (m Model) startDelete() (Model, tea.Cmd) {
	srv, ok := m.currentServer()
	if !ok {
		return m, nil
	}
	if !srv.Editable {
		m.info = fmt.Sprintf("%s is provided by %s and cannot be removed here.", srv.Name, srv.Source)
		return m, nil
	}
	m.deleteName = srv.Name
	m.screen = screenDelete
	return m, nil
}

func (m Model) toggleDisabled() (Model, tea.Cmd) {
	srv, ok := m.currentServer()
	if !ok {
		return m, nil
	}
	if !srv.Editable {
		m.info = fmt.Sprintf("%s is provided by %s and cannot be changed here.", srv.Name, srv.Source)
		return m, nil
	}
	if err := m.deps.SetDisabled(m.ctx, srv.Name, !srv.Disabled); err != nil {
		m.errMsg = err.Error()
		return m, nil
	}
	state := "enabled"
	if !srv.Disabled {
		state = "disabled"
	}
	m.info = fmt.Sprintf("%s %s.", srv.Name, state)
	m.errMsg = ""
	m.servers = m.deps.ListServers()
	return m, nil
}

func (m Model) reload() (Model, tea.Cmd) {
	if m.busy() {
		return m, nil
	}
	m.reloading = true
	m.errMsg = ""
	return m, tea.Batch(
		m.spin.Tick,
		reloadServerCmd(m.ctx, m.deps),
	)
}

func reloadServerCmd(ctx context.Context, deps Deps) tea.Cmd {
	return func() tea.Msg {
		status, err := deps.Reload(ctx)
		return reloadResultMsg{err: err, status: status}
	}
}

func (m Model) handleReloadResult(msg reloadResultMsg) (Model, tea.Cmd) {
	m.reloading = false
	if msg.err != nil {
		m.errMsg = msg.err.Error()
		return m, nil
	}
	m.info = msg.status
	m.errMsg = ""
	m.servers = m.deps.ListServers()
	return m, nil
}

func (m Model) busy() bool { return m.saving || m.reloading }

func (m Model) updateForm(key tea.KeyMsg) (Model, tea.Cmd) {
	switch key.String() {
	case "esc":
		m.screen = screenList
		m.servers = m.deps.ListServers()
		return m, nil
	case "up", "k":
		m.fieldCursor = wrapDec(m.fieldCursor, len(m.fields))
		return m, nil
	case "down", "j":
		m.fieldCursor = wrapInc(m.fieldCursor, len(m.fields))
		return m, nil
	case "enter", " ", "space", "left", "right":
		return m.activateField(key.String())
	}
	return m, nil
}

func (m Model) activateField(keyStr string) (Model, tea.Cmd) {
	if m.fieldCursor < 0 || m.fieldCursor >= len(m.fields) {
		return m, nil
	}
	id := m.fields[m.fieldCursor]
	switch fieldKindOf(id) {
	case kindText, kindSecret:
		if keyStr != "enter" {
			return m, nil
		}
		m.editing = id
		m.input = m.freshFieldInput(id)
		m.screen = screenField
		return m, nil
	case kindTransport:
		m.form.Transport = nextTransport(m.form.Transport)
		m.rebuildFields()
		return m, nil
	case kindToggle:
		switch id {
		case fTrust:
			m.form.Trust = !m.form.Trust
		case fDisabled:
			m.form.Disabled = !m.form.Disabled
		}
		return m, nil
	case kindAction:
		if keyStr != "enter" {
			return m, nil
		}
		return m.save()
	}
	return m, nil
}

func (m Model) save() (Model, tea.Cmd) {
	if m.saving {
		return m, nil
	}
	m.saving = true
	m.errMsg = ""
	form := m.form
	return m, tea.Batch(
		m.spin.Tick,
		saveServerCmd(m.ctx, m.deps, m.originalName, form, m.adding),
	)
}

func saveServerCmd(ctx context.Context, deps Deps, originalName string, form ServerForm, adding bool) tea.Cmd {
	return func() tea.Msg {
		err := deps.SaveServer(ctx, originalName, form)
		return saveResultMsg{err: err, adding: adding, name: form.Name}
	}
}

func (m Model) handleSaveResult(msg saveResultMsg) (Model, tea.Cmd) {
	m.saving = false
	if msg.err != nil {
		m.errMsg = msg.err.Error()
		return m, nil
	}
	verb := "added"
	if !msg.adding {
		verb = "updated"
	}
	m.info = fmt.Sprintf("Server %s %s.", msg.name, verb)
	m.errMsg = ""
	m.servers = m.deps.ListServers()
	m.screen = screenList
	return m, nil
}

func newDialogSpinner(th theme.Theme) spinner.Model {
	s := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	if th.Colored {
		s.Style = s.Style.Foreground(th.Accent.GetForeground())
	}
	return s
}

func (m Model) updateField(msg tea.Msg) (Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if ok {
		switch key.String() {
		case "esc":
			m.screen = screenForm
			return m, nil
		case "enter":
			m.applyFieldValue(m.editing, m.input.Value())
			m.screen = screenForm
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *Model) applyFieldValue(id fieldID, value string) {
	switch id {
	case fName:
		m.form.Name = value
	case fCommand:
		m.form.Command = value
	case fArgs:
		m.form.Args = value
	case fURL:
		m.form.URL = value
	case fEnv:
		m.form.Env = value
	case fHeaders:
		m.form.Headers = value
	case fBearer:
		m.form.Bearer = value
	case fTimeout:
		m.form.Timeout = value
	case fDescription:
		m.form.Description = value
	}
}

func (m Model) updateDelete(key tea.KeyMsg) (Model, tea.Cmd) {
	switch key.String() {
	case "y", "enter":
		if err := m.deps.RemoveServer(m.ctx, m.deleteName); err != nil {
			m.errMsg = err.Error()
			m.screen = screenList
			return m, nil
		}
		m.info = fmt.Sprintf("Removed %s.", m.deleteName)
		m.errMsg = ""
		m.servers = m.deps.ListServers()
		if m.listCursor >= len(m.servers) {
			m.listCursor = 0
		}
		m.screen = screenList
		return m, nil
	case "esc", "n":
		m.screen = screenList
		return m, nil
	}
	return m, nil
}

func (m *Model) rebuildFields() {
	fields := []fieldID{fName, fTransport}
	switch m.form.Transport {
	case TransportHTTP, TransportSSE:
		fields = append(fields, fURL, fHeaders, fBearer)
	default:
		fields = append(fields, fCommand, fArgs)
	}
	fields = append(fields, fEnv, fTimeout, fDescription, fTrust, fDisabled, fSave)
	m.fields = fields
	if m.fieldCursor >= len(m.fields) {
		m.fieldCursor = 0
	}
}

func (m Model) currentServer() (ServerEntry, bool) {
	if m.listCursor < 0 || m.listCursor >= len(m.servers) {
		return ServerEntry{}, false
	}
	return m.servers[m.listCursor], true
}

func (m Model) freshFieldInput(id fieldID) textinput.Model {
	ti := textinput.New()
	ti.CharLimit = 8192
	ti.Prompt = "> "
	ti.Focus()
	if fieldKindOf(id) == kindSecret {
		ti.EchoMode = textinput.EchoPassword
		return ti
	}
	ti.SetValue(m.fieldValue(id))
	ti.CursorEnd()
	return ti
}

func (m Model) fieldValue(id fieldID) string {
	switch id {
	case fName:
		return m.form.Name
	case fCommand:
		return m.form.Command
	case fArgs:
		return m.form.Args
	case fURL:
		return m.form.URL
	case fEnv:
		return m.form.Env
	case fHeaders:
		return m.form.Headers
	case fTimeout:
		return m.form.Timeout
	case fDescription:
		return m.form.Description
	}
	return ""
}

func fieldKindOf(id fieldID) fieldKind {
	switch id {
	case fTransport:
		return kindTransport
	case fTrust, fDisabled:
		return kindToggle
	case fBearer:
		return kindSecret
	case fSave:
		return kindAction
	default:
		return kindText
	}
}

func nextTransport(t string) string {
	switch t {
	case TransportStdio:
		return TransportHTTP
	case TransportHTTP:
		return TransportSSE
	default:
		return TransportStdio
	}
}

func wrapInc(i, n int) int {
	if n <= 0 {
		return 0
	}
	return (i + 1) % n
}

func wrapDec(i, n int) int {
	if n <= 0 {
		return 0
	}
	return (i - 1 + n) % n
}
