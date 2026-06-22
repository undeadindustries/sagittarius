package toolsdialog

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/undeadindustries/sagittarius/internal/ui/theme"
)

// rowKind classifies a rendered row for navigation and key handling.
type rowKind int

const (
	rowSectionHeader rowKind = iota // not selectable
	rowServerHeader                 // not selectable
	rowBuiltin                      // selectable, read-only
	rowMCPTool                      // selectable, space toggles
	rowAction                       // selectable, enter triggers
	rowNote                         // not selectable
)

type row struct {
	kind    rowKind
	text    string
	server  string
	tool    string
	enabled bool
}

// Model is the /tools inventory overlay driven by the parent Bubble Tea model.
type Model struct {
	deps Deps
	ctx  context.Context
	th   theme.Theme

	width  int
	height int

	rows   []row
	cursor int

	done        bool
	openServers bool
	status      string
	errMsg      string
	info        string
}

// New constructs the tool inventory overlay.
func New(ctx context.Context, deps Deps) Model {
	m := Model{deps: deps, ctx: ctx, th: theme.Default()}
	m.rebuild()
	m.cursor = m.firstSelectable()
	return m
}

// Done reports whether the dialog has finished and should be removed.
func (m Model) Done() bool { return m.done }

// OpenServers reports whether the dialog asked to switch to the /mcp wizard.
func (m Model) OpenServers() bool { return m.openServers }

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
	return m
}

func (m *Model) rebuild() {
	rows := []row{{kind: rowSectionHeader, text: "Built-in tools  ·  not editable"}}
	builtins := m.deps.BuiltinTools()
	if len(builtins) == 0 {
		rows = append(rows, row{kind: rowNote, text: "No built-in tools registered."})
	}
	for _, b := range builtins {
		rows = append(rows, row{kind: rowBuiltin, text: b.Name, tool: b.Description})
	}

	rows = append(rows, row{kind: rowSectionHeader, text: "MCP tools"})
	groups := m.deps.ServerTools(m.ctx)
	if len(groups) == 0 {
		rows = append(rows, row{kind: rowNote, text: "No MCP servers configured. Use /mcp to add one."})
	}
	for _, g := range groups {
		header := g.Server
		if g.Status != "" {
			header += "  (" + g.Status + ")"
		}
		rows = append(rows, row{kind: rowServerHeader, text: header})
		if g.Err != "" {
			rows = append(rows, row{kind: rowNote, text: "  error: " + g.Err})
			continue
		}
		if len(g.Tools) == 0 {
			rows = append(rows, row{kind: rowNote, text: "  (no tools)"})
			continue
		}
		for _, t := range g.Tools {
			rows = append(rows, row{
				kind:    rowMCPTool,
				text:    t.Name,
				server:  g.Server,
				tool:    t.Name,
				enabled: t.Enabled,
			})
		}
	}

	rows = append(rows, row{kind: rowAction, text: "Manage MCP servers…"})
	m.rows = rows
	if m.cursor >= len(m.rows) {
		m.cursor = m.firstSelectable()
	}
}

func selectable(k rowKind) bool {
	return k == rowBuiltin || k == rowMCPTool || k == rowAction
}

func (m Model) firstSelectable() int {
	for i, r := range m.rows {
		if selectable(r.kind) {
			return i
		}
	}
	return 0
}

func (m *Model) moveCursor(dir int) {
	n := len(m.rows)
	if n == 0 {
		return
	}
	i := m.cursor
	for step := 0; step < n; step++ {
		i = (i + dir + n) % n
		if selectable(m.rows[i].kind) {
			m.cursor = i
			return
		}
	}
}

// Update advances the inventory for one message.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "esc", "q":
		m.done = true
		return m, nil
	case "up", "k":
		m.moveCursor(-1)
		return m, nil
	case "down", "j":
		m.moveCursor(1)
		return m, nil
	case "r":
		return m.reload()
	case " ", "space":
		return m.toggle()
	case "enter":
		return m.activate()
	}
	return m, nil
}

func (m Model) current() (row, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return row{}, false
	}
	return m.rows[m.cursor], true
}

func (m Model) toggle() (Model, tea.Cmd) {
	r, ok := m.current()
	if !ok {
		return m, nil
	}
	switch r.kind {
	case rowBuiltin:
		m.info = "Built-in tools are not editable."
		m.errMsg = ""
		return m, nil
	case rowMCPTool:
		want := !r.enabled
		if err := m.deps.SetToolEnabled(m.ctx, r.server, r.tool, want); err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		state := "enabled"
		if !want {
			state = "disabled"
		}
		m.info = fmt.Sprintf("%s %s on %s.", r.tool, state, r.server)
		m.errMsg = ""
		m.rebuild()
		return m, nil
	}
	return m, nil
}

func (m Model) activate() (Model, tea.Cmd) {
	r, ok := m.current()
	if !ok {
		return m, nil
	}
	if r.kind == rowAction {
		m.openServers = true
		m.done = true
		return m, nil
	}
	if r.kind == rowMCPTool {
		return m.toggle()
	}
	return m, nil
}

func (m Model) reload() (Model, tea.Cmd) {
	if err := m.deps.ReloadTools(m.ctx); err != nil {
		m.errMsg = err.Error()
		return m, nil
	}
	m.info = "Tools reloaded."
	m.errMsg = ""
	m.rebuild()
	return m, nil
}
