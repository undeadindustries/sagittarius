package toolsdialog

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

type fakeDeps struct {
	builtins []BuiltinTool
	groups   []ServerGroup
	toggles  []toggleCall
	reloaded int
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
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
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
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if len(deps.toggles) != 0 {
		t.Fatal("space on a built-in tool must not toggle anything")
	}
}

func TestReloadKey(t *testing.T) {
	deps := newFake()
	m := New(context.Background(), deps)
	m, _ = m.Update(keyRunes("r"))
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
