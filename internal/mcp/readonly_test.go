package mcp

import (
	"context"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/undeadindustries/sagittarius/internal/config"
)

func boolPtr(b bool) *bool { return &b }

// TestToolIsReadOnly pins the two admission routes and, more importantly, what
// they refuse. The MCP spec states annotations are hints that "are not
// guaranteed to provide a faithful description of tool behavior" and that
// clients "should never make tool use decisions based on ToolAnnotations
// received from untrusted servers" — so an untrusted server asserting its own
// harmlessness must not be believed.
func TestToolIsReadOnly(t *testing.T) {
	t.Parallel()

	readOnlyTool := &sdkmcp.Tool{
		Name:        "evaluate",
		Annotations: &sdkmcp.ToolAnnotations{ReadOnlyHint: true},
	}
	mutatingTool := &sdkmcp.Tool{
		Name:        "update",
		Annotations: &sdkmcp.ToolAnnotations{ReadOnlyHint: false},
	}
	unannotatedTool := &sdkmcp.Tool{Name: "mystery"}

	cases := []struct {
		name string
		tool *sdkmcp.Tool
		cfg  ServerConfig
		want bool
	}{
		{
			name: "trusted server declaring readOnlyHint is believed",
			tool: readOnlyTool,
			cfg:  ServerConfig{Trust: true},
			want: true,
		},
		{
			name: "untrusted server declaring readOnlyHint is not believed",
			tool: readOnlyTool,
			cfg:  ServerConfig{Trust: false},
			want: false,
		},
		{
			name: "explicit allowlist wins without trust",
			tool: unannotatedTool,
			cfg:  ServerConfig{ReadOnlyTools: []string{"mystery"}},
			want: true,
		},
		{
			name: "explicit allowlist overrides a mutating annotation",
			tool: mutatingTool,
			cfg:  ServerConfig{ReadOnlyTools: []string{"update"}},
			want: true,
		},
		{
			name: "allowlist entry for a different tool does not leak",
			tool: unannotatedTool,
			cfg:  ServerConfig{ReadOnlyTools: []string{"something_else"}},
			want: false,
		},
		{
			name: "no annotations and no allowlist fails closed",
			tool: unannotatedTool,
			cfg:  ServerConfig{Trust: true},
			want: false,
		},
		{
			name: "trusted server declaring not-read-only is denied",
			tool: mutatingTool,
			cfg:  ServerConfig{Trust: true},
			want: false,
		},
		{
			name: "nil tool fails closed",
			tool: nil,
			cfg:  ServerConfig{Trust: true, ReadOnlyTools: []string{"anything"}},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := toolIsReadOnly(tc.tool, tc.cfg); got != tc.want {
				t.Fatalf("toolIsReadOnly() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestDiscoveredToolReadOnlyHint confirms the resolution reaches the registered
// tool, which is what the interaction-mode gate actually probes.
func TestDiscoveredToolReadOnlyHint(t *testing.T) {
	t.Parallel()

	session := &mockSession{
		tools: []*sdkmcp.Tool{
			{Name: "evaluate", Annotations: &sdkmcp.ToolAnnotations{ReadOnlyHint: true}},
			{Name: "update"},
			{Name: "vouched"},
		},
	}
	manager := NewManager(ManagerConfig{Connector: &mockConnector{session: session}})
	if err := manager.Reload(context.Background(), map[string]config.MCPServerConfig{
		"demo": {
			Command:       "mock",
			Trust:         boolPtr(true),
			ReadOnlyTools: []string{"vouched"},
		},
	}, false); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}

	got := map[string]bool{}
	for _, dt := range manager.Tools() {
		got[dt.toolName] = dt.ReadOnlyHint()
	}
	if !got["evaluate"] {
		t.Error("annotated read-only tool should report ReadOnlyHint")
	}
	if got["update"] {
		t.Error("unannotated tool should not report ReadOnlyHint")
	}
	if !got["vouched"] {
		t.Error("allowlisted tool should report ReadOnlyHint")
	}
}

// TestToolInventoryReportsReadOnlyProvenance covers the /tools display: the UI
// must distinguish a user allowlist entry (editable there) from the server's own
// declaration (not editable, since un-listing could not revoke it).
func TestToolInventoryReportsReadOnlyProvenance(t *testing.T) {
	t.Parallel()

	session := &mockSession{
		tools: []*sdkmcp.Tool{
			{Name: "evaluate", Annotations: &sdkmcp.ToolAnnotations{ReadOnlyHint: true}},
			{Name: "vouched"},
			{Name: "update"},
		},
	}
	manager := NewManager(ManagerConfig{Connector: &mockConnector{session: session}})
	if err := manager.Reload(context.Background(), map[string]config.MCPServerConfig{
		"demo": {
			Command:       "mock",
			Trust:         boolPtr(true),
			ReadOnlyTools: []string{"vouched"},
		},
	}, false); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}

	inv := manager.ToolInventory(context.Background())
	if len(inv) != 1 {
		t.Fatalf("ToolInventory() len = %d, want 1", len(inv))
	}
	byName := map[string]ToolInfo{}
	for _, tl := range inv[0].Tools {
		byName[tl.Name] = tl
	}

	if !byName["evaluate"].ReadOnly || !byName["evaluate"].ReadOnlyFromHint {
		t.Errorf("evaluate = %+v, want read-only from the server's own hint", byName["evaluate"])
	}
	if !byName["vouched"].ReadOnly || byName["vouched"].ReadOnlyFromHint {
		t.Errorf("vouched = %+v, want read-only from the user allowlist", byName["vouched"])
	}
	if byName["update"].ReadOnly {
		t.Errorf("update = %+v, want not read-only", byName["update"])
	}
}
