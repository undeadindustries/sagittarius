package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func newSymbolWorkspace(t *testing.T) *Workspace {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"greet.go": "package app\n\n" +
			"func Greet() string { return \"hi\" }\n\n" +
			"func run() { Greet() }\n",
		"util.py":  "def helper():\n    return 1\n\ndef caller():\n    helper()\n",
		"data.txt": "Greet is just a word in prose here.\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

func requireRipgrep(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("ripgrep (rg) not installed; skipping directory-scan test")
	}
}

func TestFindSymbolFileOutline(t *testing.T) {
	ws := newSymbolWorkspace(t)
	tool := newFindSymbolTool(ws, false)

	res, err := tool.Execute(context.Background(), map[string]any{
		ParamDirPath: "greet.go",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	matches, _ := res["matches"].(string)
	if !strings.Contains(matches, "Greet") {
		t.Errorf("outline missing Greet definition: %q", matches)
	}
	// Outline mode returns definitions only.
	if refs, _ := res["references"].(int); refs != 0 {
		t.Errorf("outline should have 0 references, got %d", refs)
	}
	if defs, _ := res["definitions"].(int); defs < 2 {
		t.Errorf("expected at least 2 definitions (Greet, run), got %d", defs)
	}
}

func TestFindSymbolDirectoryScanWithFilter(t *testing.T) {
	requireRipgrep(t)
	ws := newSymbolWorkspace(t)
	tool := newFindSymbolTool(ws, false)

	res, err := tool.Execute(context.Background(), map[string]any{
		FindSymbolParamSymbol: "greet",
		FindSymbolParamKind:   findSymbolKindAll,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	matches, _ := res["matches"].(string)
	if !strings.Contains(matches, "greet.go") {
		t.Errorf("expected greet.go in matches, got %q", matches)
	}
	// Case-insensitive substring should match both def and reference of Greet.
	if defs, _ := res["definitions"].(int); defs < 1 {
		t.Errorf("expected the Greet definition, got %d defs", defs)
	}
	if refs, _ := res["references"].(int); refs < 1 {
		t.Errorf("expected the Greet() call reference, got %d refs", refs)
	}
	// The prose file must not contribute symbols.
	if strings.Contains(matches, "data.txt") {
		t.Errorf("prose file should not yield symbols: %q", matches)
	}
}

func TestFindSymbolKindFilter(t *testing.T) {
	requireRipgrep(t)
	ws := newSymbolWorkspace(t)
	tool := newFindSymbolTool(ws, false)

	res, err := tool.Execute(context.Background(), map[string]any{
		FindSymbolParamSymbol: "helper",
		FindSymbolParamKind:   findSymbolKindReference,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if defs, _ := res["definitions"].(int); defs != 0 {
		t.Errorf("reference-only query returned %d definitions", defs)
	}
	if refs, _ := res["references"].(int); refs < 1 {
		t.Errorf("expected the helper() call reference, got %d", refs)
	}
}

func TestFindSymbolMaxResultsTruncation(t *testing.T) {
	requireRipgrep(t)
	ws := newSymbolWorkspace(t)
	tool := newFindSymbolTool(ws, false)

	res, err := tool.Execute(context.Background(), map[string]any{
		FindSymbolParamKind:       findSymbolKindAll,
		FindSymbolParamMaxResults: 1,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if count, _ := res["count"].(int); count != 1 {
		t.Errorf("expected count capped at 1, got %d", count)
	}
	if truncated, _ := res["truncated"].(bool); !truncated {
		t.Errorf("expected truncated=true when results exceed max_results")
	}
}

func TestFindSymbolInvalidKind(t *testing.T) {
	ws := newSymbolWorkspace(t)
	tool := newFindSymbolTool(ws, false)
	if _, err := tool.Execute(context.Background(), map[string]any{
		FindSymbolParamSymbol: "x",
		FindSymbolParamKind:   "bogus",
	}); err == nil {
		t.Fatal("expected error for invalid kind")
	}
}

func TestFindSymbolRegistrationToggle(t *testing.T) {
	ws := newSymbolWorkspace(t)

	on := NewBuiltinRegistry(ws)
	if _, ok := on.Lookup(FindSymbolToolName); !ok {
		t.Error("find_symbol should be registered by default")
	}

	off := NewBuiltinRegistry(ws, WithSymbols(false, true))
	if _, ok := off.Lookup(FindSymbolToolName); ok {
		t.Error("find_symbol should be absent when WithSymbols(false, …)")
	}
}

func TestFindSymbolGoplsHintInDescription(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/x\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	withHint := newFindSymbolTool(ws, true).Declaration().Description
	if !strings.Contains(withHint, "gopls") {
		t.Errorf("expected gopls note on a Go module with preferGopls, got %q", withHint)
	}
	noHint := newFindSymbolTool(ws, false).Declaration().Description
	if strings.Contains(noHint, "gopls") {
		t.Errorf("gopls note should be absent when preferGopls is false")
	}
}
