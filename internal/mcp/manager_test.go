package mcp

import (
	"context"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/undeadindustries/sagittarius/internal/config"
)

type mockSession struct {
	tools []*sdkmcp.Tool
	calls int
}

func (m *mockSession) ListTools(context.Context, *sdkmcp.ListToolsParams) (*sdkmcp.ListToolsResult, error) {
	return &sdkmcp.ListToolsResult{Tools: m.tools}, nil
}

func (m *mockSession) CallTool(_ context.Context, params *sdkmcp.CallToolParams) (*sdkmcp.CallToolResult, error) {
	m.calls++
	return &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "ok-" + params.Name}},
	}, nil
}

func (m *mockSession) Close() error { return nil }

type mockConnector struct {
	session *mockSession
}

func (m *mockConnector) Connect(context.Context, ServerConfig) (Session, error) {
	return m.session, nil
}

func TestMCPListToolsMock(t *testing.T) {
	t.Parallel()

	session := &mockSession{
		tools: []*sdkmcp.Tool{
			{Name: "echo", Description: "echo tool", InputSchema: map[string]any{"type": "object"}},
		},
	}
	manager := NewManager(ManagerConfig{Connector: &mockConnector{session: session}})

	err := manager.Reload(context.Background(), map[string]config.MCPServerConfig{
		"demo": {Command: "mock"},
	})
	if err != nil {
		t.Fatalf("Reload() error = %v", err)
	}

	tools := manager.Tools()
	if len(tools) != 1 {
		t.Fatalf("Tools() len = %d, want 1", len(tools))
	}
	if got := tools[0].Name(); got != "mcp_demo_echo" {
		t.Fatalf("tool name = %q, want mcp_demo_echo", got)
	}

	states := manager.States()
	if len(states) != 1 || states[0].ToolCount != 1 {
		t.Fatalf("States() = %+v, want one connected server with 1 tool", states)
	}

	result, err := tools[0].Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result["result"] != "ok-echo" {
		t.Fatalf("Execute() result = %v, want ok-echo", result["result"])
	}
}

func TestToolInventoryEnabledFlags(t *testing.T) {
	t.Parallel()

	session := &mockSession{
		tools: []*sdkmcp.Tool{
			{Name: "echo", Description: "echo tool"},
			{Name: "danger", Description: "dangerous tool"},
		},
	}
	manager := NewManager(ManagerConfig{Connector: &mockConnector{session: session}})

	err := manager.Reload(context.Background(), map[string]config.MCPServerConfig{
		"demo": {Command: "mock", ExcludeTools: []string{"danger"}},
	})
	if err != nil {
		t.Fatalf("Reload() error = %v", err)
	}

	// Filtered agent path hides the excluded tool.
	if got := len(manager.Tools()); got != 1 {
		t.Fatalf("Tools() len = %d, want 1 (danger excluded)", got)
	}

	inv := manager.ToolInventory(context.Background())
	if len(inv) != 1 {
		t.Fatalf("ToolInventory() len = %d, want 1", len(inv))
	}
	if inv[0].Status != ServerConnected {
		t.Fatalf("status = %q, want connected", inv[0].Status)
	}
	if len(inv[0].Tools) != 2 {
		t.Fatalf("inventory tools = %d, want 2 (unfiltered)", len(inv[0].Tools))
	}
	got := map[string]bool{}
	for _, tl := range inv[0].Tools {
		got[tl.Name] = tl.Enabled
		if tl.Name == "echo" && tl.WireName != "mcp_demo_echo" {
			t.Fatalf("wire name = %q, want mcp_demo_echo", tl.WireName)
		}
	}
	if !got["echo"] {
		t.Fatal("echo should be enabled")
	}
	if got["danger"] {
		t.Fatal("danger should be disabled by excludeTools")
	}
}

func TestToolInventoryIncludeAllowlist(t *testing.T) {
	t.Parallel()

	session := &mockSession{
		tools: []*sdkmcp.Tool{
			{Name: "echo"},
			{Name: "other"},
		},
	}
	manager := NewManager(ManagerConfig{Connector: &mockConnector{session: session}})
	if err := manager.Reload(context.Background(), map[string]config.MCPServerConfig{
		"demo": {Command: "mock", IncludeTools: []string{"echo"}},
	}); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}

	inv := manager.ToolInventory(context.Background())
	got := map[string]bool{}
	for _, tl := range inv[0].Tools {
		got[tl.Name] = tl.Enabled
	}
	if !got["echo"] {
		t.Fatal("echo should be enabled by includeTools allowlist")
	}
	if got["other"] {
		t.Fatal("other should be disabled when not in includeTools allowlist")
	}
}

func TestParseToolName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		in         string
		wantServer string
		wantTool   string
		wantOK     bool
	}{
		{name: "valid", in: "mcp_demo_echo", wantServer: "demo", wantTool: "echo", wantOK: true},
		{name: "builtin", in: "read_file", wantOK: false},
		{name: "invalid", in: "mcp_only", wantOK: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, tool, ok := ParseToolName(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if server != tc.wantServer || tool != tc.wantTool {
				t.Fatalf("ParseToolName(%q) = (%q,%q), want (%q,%q)", tc.in, server, tool, tc.wantServer, tc.wantTool)
			}
		})
	}
}
