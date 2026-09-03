package toolsdialog

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

type fakeDeps struct {
	builtins     []BuiltinTool
	groups       []ServerGroup
	toggles      []toggleCall
	readOnlySets []toggleCall
	reloaded     int
	readOnlyErr  error
}

type toggleCall struct {
	server  string
	tool    string
	enabled bool
}

func (f *fakeDeps) BuiltinTools() []BuiltinTool { return f.builtins }

func (f *fakeDeps) ServerTools(context.Context) []ServerGroup { return f.groups }

func (f *fakeDeps) SetToolEnabled(_ context.Context, server, tool string, enabled bool) error {
	f.toggles = append(f.toggles, toggleCall{server, tool, enabled})
	// Reflect the new state so a rebuild shows the toggle.
	for gi := range f.groups {
		if f.groups[gi].Server != server {
			continue
		}
		for ti := range f.groups[gi].Tools {
			if f.groups[gi].Tools[ti].Name == tool {
				f.groups[gi].Tools[ti].Enabled = enabled
			}
		}
	}
	return nil
}

func (f *fakeDeps) SetToolReadOnly(_ context.Context, server, tool string, readOnly bool) error {
	if f.readOnlyErr != nil {
		return f.readOnlyErr
	}
	f.readOnlySets = append(f.readOnlySets, toggleCall{server, tool, readOnly})
	for gi := range f.groups {
		if f.groups[gi].Server != server {
			continue
		}
		for ti := range f.groups[gi].Tools {
			if f.groups[gi].Tools[ti].Name == tool {
				f.groups[gi].Tools[ti].ReadOnly = readOnly
			}
		}
	}
	return nil
}

func (f *fakeDeps) ReloadTools(context.Context) error {
	f.reloaded++
	return nil
}

func keyRunes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func newFake() *fakeDeps {
	return &fakeDeps{
		builtins: []BuiltinTool{
			{Name: "read_file", Description: "read", Source: "builtin"},
			{Name: "activate_skill", Description: "skill", Source: "skill"},
		},
		groups: []ServerGroup{
			{Server: "demo", Status: "connected", Tools: []ServerTool{
				{Name: "echo", WireName: "mcp_demo_echo", Enabled: true},
				{Name: "danger", WireName: "mcp_demo_danger", Enabled: false},
			}},
		},
	}
}

// findRow returns the index of the first MCP tool row for tool, so a test can
// place the cursor without depending on the built-in section's length.
func findRow(t *testing.T, m Model, tool string) int {
	t.Helper()
	for i, r := range m.rows {
		if r.kind == rowMCPTool && r.tool == tool {
			return i
		}
	}
	t.Fatalf("no MCP tool row for %q", tool)
	return -1
}

// TestReadOnlyToggleVouchesForTool covers the "a" key: marking an MCP tool
// read-only is how a calculator or docs-search server becomes usable in ask
// mode without opening every tool on that server.
func TestReadOnlyToggleVouchesForTool(t *testing.T) {
	f := newFake()
	m := New(context.Background(), f)
	m.cursor = findRow(t, m, "echo")

	m, _ = m.Update(keyRunes("a"))
	if len(f.readOnlySets) != 1 {
		t.Fatalf("SetToolReadOnly calls = %d, want 1", len(f.readOnlySets))
	}
	got := f.readOnlySets[0]
	if got.server != "demo" || got.tool != "echo" || !got.enabled {
		t.Fatalf("SetToolReadOnly(%+v), want demo/echo/true", got)
	}
	if !strings.Contains(m.info, "ask") {
		t.Fatalf("info = %q, want it to name ask mode", m.info)
	}

	// Toggling again revokes it, so the allowlist is not a one-way door.
	m.cursor = findRow(t, m, "echo")
	m, _ = m.Update(keyRunes("a"))
	if len(f.readOnlySets) != 2 || f.readOnlySets[1].enabled {
		t.Fatalf("second toggle = %+v, want read-only revoked", f.readOnlySets)
	}
}

// TestReadOnlyToggleLeavesDeclaredToolsAlone guards against a lying UI: a tool
// the server itself declares read-only cannot be revoked from our settings, so
// the toggle must explain that instead of pretending to act.
func TestReadOnlyToggleLeavesDeclaredToolsAlone(t *testing.T) {
	f := newFake()
	f.groups[0].Tools[0].ReadOnly = true
	f.groups[0].Tools[0].ReadOnlyFromHint = true
	m := New(context.Background(), f)
	m.cursor = findRow(t, m, "echo")

	m, _ = m.Update(keyRunes("a"))
	if len(f.readOnlySets) != 0 {
		t.Fatalf("SetToolReadOnly called %d times for a server-declared tool, want 0", len(f.readOnlySets))
	}
	if !strings.Contains(m.info, "readOnlyHint") {
		t.Fatalf("info = %q, want it to explain the server declared it", m.info)
	}
}

// TestReadOnlyToggleOnBuiltinIsRejected keeps the key from silently doing
// nothing on a row it cannot affect.
func TestReadOnlyToggleOnBuiltinIsRejected(t *testing.T) {
	f := newFake()
	m := New(context.Background(), f)
	m.cursor = m.firstSelectable() // first row is a built-in

	m, _ = m.Update(keyRunes("a"))
	if len(f.readOnlySets) != 0 {
		t.Fatalf("SetToolReadOnly called for a built-in row, want 0 calls")
	}
	if !strings.Contains(m.info, "Only MCP tools") {
		t.Fatalf("info = %q, want it to say only MCP tools have the setting", m.info)
	}
}

// TestReadOnlyToggleSurfacesError ensures a failed persist is reported rather
// than leaving the row looking changed.
func TestReadOnlyToggleSurfacesError(t *testing.T) {
	f := newFake()
	f.readOnlyErr = fmt.Errorf("server %q is not settings-managed", "demo")
	m := New(context.Background(), f)
	m.cursor = findRow(t, m, "echo")

	m, _ = m.Update(keyRunes("a"))
	if !strings.Contains(m.errMsg, "not settings-managed") {
		t.Fatalf("errMsg = %q, want the persist failure surfaced", m.errMsg)
	}
}

// TestReadOnlyBadgeNamesProvenance covers the row label, which is the only place
// a user can see why a tool is allowed.
func TestReadOnlyBadgeNamesProvenance(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		row  row
		want string
	}{
		{name: "declared", row: row{readOnly: true, readOnlyFromHint: true}, want: "  read-only (declared)"},
		{name: "allowlisted", row: row{readOnly: true}, want: "  read-only"},
		{name: "neither", row: row{}, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := readOnlyBadge(tc.row); got != tc.want {
				t.Fatalf("readOnlyBadge() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEscCloses(t *testing.T) {
	m := New(context.Background(), newFake())
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !m.Done() {
		t.Fatal("esc should close the inventory")
	}
}

func TestCursorStartsOnSelectableRow(t *testing.T) {
	m := New(context.Background(), newFake())
	r, ok := m.current()
	if !ok {
		t.Fatal("expected a selectable row under the cursor")
	}
	if r.kind == rowSectionHeader || r.kind == rowServerHeader || r.kind == rowNote {
		t.Fatalf("cursor landed on a non-selectable row kind %v", r.kind)
	}
}

func TestSpaceTogglesMCPTool(t *testing.T) {
	deps := newFake()
	m := New(context.Background(), deps)
	// Move down to the first MCP tool (echo, currently enabled).
	for {
		r, _ := m.current()
		if r.kind == rowMCPTool {
			break
		}
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if len(deps.toggles) != 1 {
		t.Fatalf("expected one toggle, got %d", len(deps.toggles))
	}
	got := deps.toggles[0]
	if got.tool != "echo" || got.enabled {
		t.Fatalf("toggle = %+v, want echo disabled", got)
	}
}

func TestSpaceOnBuiltinIsNoop(t *testing.T) {
	deps := newFake()
	m := New(context.Background(), deps)
	// New() lands on the first built-in row.
	r, _ := m.current()
	if r.kind != rowBuiltin {
		t.Fatalf("expected first selectable row to be builtin, got %v", r.kind)
	}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if len(deps.toggles) != 0 {
		t.Fatal("space on a built-in tool must not toggle anything")
	}
}

func TestReloadKey(t *testing.T) {
	deps := newFake()
	m := New(context.Background(), deps)
	_, _ = m.Update(keyRunes("r"))
	if deps.reloaded != 1 {
		t.Fatalf("reload count = %d, want 1", deps.reloaded)
	}
}

func TestManageServersAction(t *testing.T) {
	m := New(context.Background(), newFake())
	// Navigate to the action row (last selectable).
	for i := 0; i < len(m.rows); i++ {
		r, _ := m.current()
		if r.kind == rowAction {
			break
		}
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	r, _ := m.current()
	if r.kind != rowAction {
		t.Fatal("could not reach the manage-servers action row")
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.Done() || !m.OpenServers() {
		t.Fatal("activating the action row should close and request the /mcp wizard")
	}
}

func TestLongInventoryScrolls(t *testing.T) {
	tools := make([]ServerTool, 40)
	for i := range tools {
		tools[i] = ServerTool{Name: fmt.Sprintf("tool_%02d", i), Enabled: true}
	}
	deps := &fakeDeps{
		builtins: []BuiltinTool{{Name: "read_file", Description: "read"}},
		groups:   []ServerGroup{{Server: "demo", Status: "connected", Tools: tools}},
	}
	m := New(context.Background(), deps)
	m = m.SetSize(80, 12)

	view := m.View()
	if !strings.Contains(view, "more below") {
		t.Fatalf("expected below scroll indicator in short terminal:\n%s", view)
	}

	for i := 0; i < 30; i++ {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	view = m.View()
	if !strings.Contains(view, "more above") {
		t.Fatalf("expected above scroll indicator after scrolling down:\n%s", view)
	}
	if strings.Contains(view, "tool_00") {
		t.Fatal("first tool should not render when scrolled down")
	}
}
