package mcp

import (
	"context"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/undeadindustries/sagittarius/internal/config"
)

func pruneTestManager(t *testing.T, prune bool) *Manager {
	t.Helper()
	session := &mockSession{
		tools: []*sdkmcp.Tool{{
			Name:        "search",
			Description: strings.Repeat("verbose ", 200),
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "the search query"},
				},
			},
		}},
	}
	manager := NewManager(ManagerConfig{Connector: &mockConnector{session: session}})
	if err := manager.Reload(context.Background(), map[string]config.MCPServerConfig{
		"demo": {Command: "mock"},
	}, prune); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	if len(manager.Tools()) != 1 {
		t.Fatalf("Tools() len = %d, want 1", len(manager.Tools()))
	}
	return manager
}

func propertyHasDescription(t *testing.T, decl map[string]any) bool {
	t.Helper()
	props, ok := decl["properties"].(map[string]any)
	if !ok {
		t.Fatalf("declaration has no properties map: %#v", decl)
	}
	query, ok := props["query"].(map[string]any)
	if !ok {
		t.Fatalf("declaration has no query property: %#v", props)
	}
	_, has := query["description"]
	return has
}

// TestSetPruneToolSchemasAppliesWithoutReload is the regression test for the
// /settings toggle being inert: pruning is read at Declaration time from a flag
// the manager shares with every discovered tool, so flipping it changes the
// schemas the model sees on the next request with no reconnect.
func TestSetPruneToolSchemasAppliesWithoutReload(t *testing.T) {
	t.Parallel()

	manager := pruneTestManager(t, false)
	tool := manager.Tools()[0]

	decl := tool.Declaration()
	if len(decl.Description) <= pruneDescriptionLimit {
		t.Fatalf("unpruned description len = %d, want > %d", len(decl.Description), pruneDescriptionLimit)
	}
	if !propertyHasDescription(t, decl.Parameters) {
		t.Fatal("unpruned declaration dropped the property description")
	}

	if changed := manager.SetPruneToolSchemas(true); !changed {
		t.Fatal("SetPruneToolSchemas(true) reported no change")
	}
	if changed := manager.SetPruneToolSchemas(true); changed {
		t.Fatal("SetPruneToolSchemas(true) twice reported a change")
	}

	decl = tool.Declaration()
	if len([]rune(decl.Description)) != pruneDescriptionLimit {
		t.Fatalf("pruned description len = %d, want %d", len([]rune(decl.Description)), pruneDescriptionLimit)
	}
	if propertyHasDescription(t, decl.Parameters) {
		t.Fatal("pruned declaration kept the property description")
	}
}

// TestPruneDoesNotMutateSharedSchema guards the copy in
// withoutPropertyDescriptions: deleting in place would corrupt the tool's own
// schema, so turning pruning back off could never restore the full form.
func TestPruneDoesNotMutateSharedSchema(t *testing.T) {
	t.Parallel()

	manager := pruneTestManager(t, true)
	tool := manager.Tools()[0]

	if propertyHasDescription(t, tool.Declaration().Parameters) {
		t.Fatal("pruned declaration kept the property description")
	}

	manager.SetPruneToolSchemas(false)

	decl := tool.Declaration()
	if !propertyHasDescription(t, decl.Parameters) {
		t.Fatal("property description not restored after pruning was disabled")
	}
	if len(decl.Description) <= pruneDescriptionLimit {
		t.Fatalf("description not restored: len = %d", len(decl.Description))
	}
}
