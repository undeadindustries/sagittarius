package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/modes"
	"github.com/undeadindustries/sagittarius/internal/provider"
)

// fakeMCPTool stands in for mcp.DiscoveredTool. The gate reaches an MCP tool's
// read-only state through the optional ReadOnlyHinter interface, so a local
// double is enough and internal/tools keeps no dependency on internal/mcp.
type fakeMCPTool struct {
	name     string
	readOnly bool
	// hinter controls whether the tool implements ReadOnlyHinter at all, so the
	// "server ships no annotations and predates the interface" case is covered.
	hinter bool
}

func (f fakeMCPTool) Name() string { return f.name }

func (f fakeMCPTool) Declaration() provider.ToolDeclaration {
	return provider.ToolDeclaration{Name: f.name}
}

func (f fakeMCPTool) RequiresConfirmation() bool { return false }

func (f fakeMCPTool) Execute(context.Context, map[string]any) (map[string]any, error) {
	return map[string]any{"result": "ok"}, nil
}

type readOnlyMCPTool struct{ fakeMCPTool }

func (r readOnlyMCPTool) ReadOnlyHint() bool { return r.readOnly }

func mcpTool(name string, readOnly bool) Tool {
	base := fakeMCPTool{name: name, readOnly: readOnly, hinter: true}
	return readOnlyMCPTool{fakeMCPTool: base}
}

// TestReadOnlyMCPToolAllowedInReadOnlyModes is the point of the feature: an MCP
// tool that modifies nothing (a calculator, a docs search) must be usable in the
// modes whose promise is that nothing changes. It was previously denied by a
// blanket mcp_ prefix check that conflated "we did not verify this" with
// "this is dangerous".
func TestReadOnlyMCPToolAllowedInReadOnlyModes(t *testing.T) {
	t.Parallel()

	tool := mcpTool("mcp_math_evaluate", true)

	for _, mode := range []modes.Mode{modes.ModeAsk, modes.ModePlan} {
		allowed, reason := InteractionModeAllowTool(mode, tool, tool.Name(), nil, nil)
		if !allowed {
			t.Fatalf("%s mode denied a read-only MCP tool: %s", mode, reason)
		}
	}

	if allowed, reason := inspectModeAllow(tool.Name(), nil, tool); !allowed {
		t.Fatalf("inspect posture denied a read-only MCP tool: %s", reason)
	}
	if allowed, reason := grillModeAllow(tool.Name(), nil, tool); !allowed {
		t.Fatalf("grill mode denied a read-only MCP tool: %s", reason)
	}
}

// TestMutatingMCPToolStillDeniedInReadOnlyModes guards the other half. The
// allowlist polarity is deliberate: a server added later must not gain ask-mode
// access just by existing.
func TestMutatingMCPToolStillDeniedInReadOnlyModes(t *testing.T) {
	t.Parallel()

	tool := mcpTool("mcp_gam_sheets_update", false)

	for _, mode := range []modes.Mode{modes.ModeAsk, modes.ModePlan} {
		allowed, reason := InteractionModeAllowTool(mode, tool, tool.Name(), nil, nil)
		if allowed {
			t.Fatalf("%s mode allowed an unvouched MCP tool", mode)
		}
		if !strings.Contains(reason, "readOnlyTools") {
			t.Fatalf("%s deny reason = %q, want it to name readOnlyTools", mode, reason)
		}
	}

	allowed, reason := inspectModeAllow(tool.Name(), nil, tool)
	if allowed {
		t.Fatal("inspect posture allowed an unvouched MCP tool")
	}
	if !strings.Contains(reason, "/readonly off") {
		t.Fatalf("inspect deny reason = %q, want it to name /readonly off", reason)
	}
}

// TestMCPToolWithoutHintDeniedInReadOnlyModes covers a tool that does not
// implement ReadOnlyHinter at all. It must fail closed rather than panicking or
// being admitted by an unchecked type assertion.
func TestMCPToolWithoutHintDeniedInReadOnlyModes(t *testing.T) {
	t.Parallel()

	tool := fakeMCPTool{name: "mcp_legacy_thing"}
	allowed, reason := InteractionModeAllowTool(modes.ModeAsk, tool, tool.Name(), nil, nil)
	if allowed {
		t.Fatalf("a tool with no read-only hint was allowed in ask mode: %s", reason)
	}
}

// TestMCPToolAllowedInAgentModeRegardless confirms the gate did not tighten
// agent or debug mode: read-only marking governs read-only modes only.
func TestMCPToolAllowedInAgentModeRegardless(t *testing.T) {
	t.Parallel()

	tool := mcpTool("mcp_gam_sheets_update", false)
	for _, mode := range []modes.Mode{modes.ModeAgent, modes.ModeDebug} {
		if allowed, reason := InteractionModeAllowTool(mode, tool, tool.Name(), nil, nil); !allowed {
			t.Fatalf("%s mode denied an MCP tool: %s", mode, reason)
		}
	}
}

// TestReadOnlyMCPToolIsDeclaredInReadOnlyModes ensures an admitted tool is also
// advertised to the model. Allowing a call the model was never told about would
// leave the capability unreachable in practice.
func TestReadOnlyMCPToolIsDeclaredInReadOnlyModes(t *testing.T) {
	t.Parallel()

	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reg := NewBuiltinRegistry(ws)
	readable := mcpTool("mcp_math_evaluate", true)
	mutating := mcpTool("mcp_gam_sheets_update", false)
	reg.Register(readable)
	reg.Register(mutating)

	for _, mode := range []modes.Mode{modes.ModeAsk, modes.ModePlan} {
		declared := map[string]bool{}
		for _, d := range reg.ListDeclarationsForMode(mode) {
			declared[d.Name] = true
		}
		if !declared[readable.Name()] {
			t.Fatalf("%s mode hid the read-only MCP tool from the model", mode)
		}
		if declared[mutating.Name()] {
			t.Fatalf("%s mode declared an unvouched MCP tool", mode)
		}
	}
}

// TestNameOnlyGateStillDeniesMCP pins the conservative fallback: callers that
// cannot resolve the tool (hook-rewrite revalidation, declaration checks before
// registration) must not admit an MCP tool by default.
func TestNameOnlyGateStillDeniesMCP(t *testing.T) {
	t.Parallel()

	if allowed, _ := InteractionModeAllow(modes.ModeAsk, "mcp_math_evaluate", nil, nil); allowed {
		t.Fatal("the name-only gate admitted an MCP tool without seeing its hint")
	}
}
