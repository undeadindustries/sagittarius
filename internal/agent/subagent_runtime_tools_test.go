package agent

import (
	"context"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/modes"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/tools"
)

// fakeReadOnlyMCPTool is a read-only MCP double for the child-registry tests;
// it implements tools.ReadOnlyHinter so the ask-mode gate admits it.
type fakeReadOnlyMCPTool struct{ name string }

func (f fakeReadOnlyMCPTool) Name() string { return f.name }
func (f fakeReadOnlyMCPTool) Declaration() provider.ToolDeclaration {
	return provider.ToolDeclaration{Name: f.name}
}
func (f fakeReadOnlyMCPTool) RequiresConfirmation() bool { return false }
func (f fakeReadOnlyMCPTool) ReadOnlyHint() bool         { return true }
func (f fakeReadOnlyMCPTool) Execute(context.Context, map[string]any) (map[string]any, error) {
	return map[string]any{"result": "ok"}, nil
}

// TestRuntimeToolsForChildNilRuntime: a parent without a runtime (tests,
// headless without MCP) gives the child no extra tools, and must not panic.
func TestRuntimeToolsForChildNilRuntime(t *testing.T) {
	t.Parallel()
	h := newSubagentHarness(t, openAISettingsWithModelPins(nil))
	if got := h.parent.runtimeToolsForChild(); got != nil {
		t.Errorf("runtimeToolsForChild with no runtime = %v, want nil", got)
	}
}

// TestRuntimeToolsForChildIncludesSkillAndMCP: with a real catalog, the child
// inherits activate_skill and whatever MCP tools the manager has discovered.
func TestRuntimeToolsForChildIncludesSkillAndMCP(t *testing.T) {
	t.Parallel()

	ws, err := tools.NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cat, err := NewCatalog(CatalogConfig{Workspace: ws})
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	t.Cleanup(func() { _ = cat.Close() })

	h := newSubagentHarness(t, openAISettingsWithModelPins(nil))
	h.parent.runtime = &Runtime{Catalog: cat}

	got := h.parent.runtimeToolsForChild()
	var names []string
	for _, tool := range got {
		names = append(names, tool.Name())
	}
	if !containsString(names, "activate_skill") {
		t.Errorf("child tools %v missing activate_skill", names)
	}
}

// TestChildExtraToolsRegisteredAndDeclared: ExtraTools land in the child's
// registry and, for a read-only MCP tool in ask mode, in the declared tools
// the model actually sees.
func TestChildExtraToolsRegisteredAndDeclared(t *testing.T) {
	t.Parallel()

	mcp := fakeReadOnlyMCPTool{name: "mcp_palace_search"}
	child, err := NewRunner(RunnerConfig{
		Generator:   &fakeGenerator{batches: [][]provider.StreamResponse{{{TextDelta: "ok", Done: true}}}},
		Model:       "child-model",
		WorkDir:     t.TempDir(),
		Interactive: false,
		Settings:    openAISettingsWithModelPins(nil),
		InitialMode: modes.ModeAsk,
		ExtraTools:  []tools.Tool{mcp},
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { _ = child.Close() })

	if _, ok := child.Registry().Lookup("mcp_palace_search"); !ok {
		t.Fatal("extra MCP tool not in child registry")
	}
	// In ask mode the read-only MCP tool must be declared to the model.
	decls := child.Registry().ListDeclarationsForMode(modes.ModeAsk)
	var declared []string
	for _, d := range decls {
		declared = append(declared, d.Name)
	}
	if !containsString(declared, "mcp_palace_search") {
		t.Errorf("read-only MCP tool not declared in ask mode: %v", declared)
	}
}

// TestMCPToolAdapterForwardsReadOnlyHint is the latent AD-133 guard: the
// adapter must satisfy tools.ReadOnlyHinter or the mode and lease gates deny
// every MCP tool regardless of the server's annotation.
func TestMCPToolAdapterForwardsReadOnlyHint(t *testing.T) {
	t.Parallel()
	var _ tools.ReadOnlyHinter = (*mcpToolAdapter)(nil)
}
