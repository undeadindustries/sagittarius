package tools

import (
	"context"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/provider"
)

func TestRegistryListEntries(t *testing.T) {
	t.Parallel()

	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatalf("NewWorkspace() error = %v", err)
	}
	reg := NewBuiltinRegistry(ws)

	entries := reg.ListEntries()
	if len(entries) == 0 {
		t.Fatal("expected built-in entries")
	}
	byName := make(map[string]ToolEntry, len(entries))
	for _, e := range entries {
		byName[e.Name] = e
	}

	read, ok := byName[ReadFileToolName]
	if !ok {
		t.Fatalf("%s missing from entries", ReadFileToolName)
	}
	if read.Source != SourceBuiltin || !read.ReadOnly {
		t.Fatalf("read_file = %+v, want builtin/read-only", read)
	}
	if read.Description == "" {
		t.Fatal("read_file should carry a description")
	}
}

func TestRegistryListEntriesClassifiesSources(t *testing.T) {
	t.Parallel()

	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatalf("NewWorkspace() error = %v", err)
	}
	reg := NewBuiltinRegistry(ws)
	reg.Register(stubTool{name: "mcp_demo_echo", desc: "remote echo"})

	entries := reg.ListEntries()
	var mcp *ToolEntry
	for i := range entries {
		if entries[i].Name == "mcp_demo_echo" {
			mcp = &entries[i]
			break
		}
	}
	if mcp == nil {
		t.Fatal("mcp tool not listed")
	}
	if mcp.Source != SourceMCP || mcp.ReadOnly {
		t.Fatalf("mcp entry = %+v, want mcp/editable", *mcp)
	}
	if mcp.Description != "remote echo" {
		t.Fatalf("mcp description = %q, want %q", mcp.Description, "remote echo")
	}
}

type stubTool struct {
	name string
	desc string
}

func (s stubTool) Name() string               { return s.name }
func (s stubTool) RequiresConfirmation() bool { return false }
func (s stubTool) Declaration() provider.ToolDeclaration {
	return provider.ToolDeclaration{Name: s.name, Description: s.desc}
}
func (s stubTool) Execute(context.Context, map[string]any) (map[string]any, error) {
	return nil, nil
}
