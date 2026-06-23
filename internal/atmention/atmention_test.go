package atmention

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/tools"
)

func TestScanMentions(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"none", "just some text", nil},
		{"simple", "explain @internal/agent/app.go", []string{"internal/agent/app.go"}},
		{"at start", "@main.go please", []string{"main.go"}},
		{"multiple", "diff @a.go and @b.go", []string{"a.go", "b.go"}},
		{"email not a mention", "mail rob@example.com now", nil},
		{"escaped at ignored", `literal \@notapath here`, nil},
		{"trailing dot dropped", "see @a.go.", []string{"a.go"}},
		{"interior dots kept", "@a.b.c", []string{"a.b.c"}},
		{"quoted with spaces", `open @"my file.go" done`, []string{"my file.go"}},
		{"escaped space in path", `open @my\ file.go`, []string{"my file.go"}},
		{"delimiter stops", "(@a.go)", []string{"a.go"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scanMentions(tt.input)
			if !equalStrings(got, tt.want) {
				t.Fatalf("scanMentions(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestExpandNoMentions(t *testing.T) {
	ws := newWorkspace(t, nil)
	parts, err := Expand(ws, "hello world")
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if len(parts) != 1 || parts[0].Text != "hello world" {
		t.Fatalf("parts = %+v, want single unchanged text part", parts)
	}
}

func TestExpandInjectsFile(t *testing.T) {
	ws := newWorkspace(t, map[string]string{
		"docs/readme.md": "hello from file",
	})
	parts, err := Expand(ws, "summarize @docs/readme.md")
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("got %d parts, want 2", len(parts))
	}
	if parts[0].Text != "summarize @docs/readme.md" {
		t.Fatalf("first part = %q, want original query", parts[0].Text)
	}
	body := parts[1].Text
	if !strings.Contains(body, referenceHeader) || !strings.Contains(body, referenceFooter) {
		t.Fatalf("body missing delimiters: %q", body)
	}
	if !strings.Contains(body, "hello from file") {
		t.Fatalf("body missing file content: %q", body)
	}
	if !strings.Contains(body, "@docs/readme.md") {
		t.Fatalf("body missing file label: %q", body)
	}
}

func TestExpandMissingFileErrors(t *testing.T) {
	ws := newWorkspace(t, nil)
	if _, err := Expand(ws, "see @nope.txt"); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestExpandDirectoryErrors(t *testing.T) {
	ws := newWorkspace(t, map[string]string{"sub/keep.txt": "x"})
	if _, err := Expand(ws, "look @sub"); err == nil {
		t.Fatal("expected error for directory reference")
	}
}

func TestExpandBinaryErrors(t *testing.T) {
	ws := newWorkspace(t, nil)
	bin := filepath.Join(ws.Root(), "data.bin")
	if err := os.WriteFile(bin, []byte{0x00, 0x01, 0x02}, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := Expand(ws, "read @data.bin"); err == nil {
		t.Fatal("expected error for binary file")
	}
}

func TestExpandDeduplicates(t *testing.T) {
	ws := newWorkspace(t, map[string]string{"a.txt": "AAA"})
	parts, err := Expand(ws, "@a.txt @a.txt")
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	body := parts[1].Text
	if strings.Count(body, "File: @a.txt") != 1 {
		t.Fatalf("expected single file block, got: %q", body)
	}
}

func TestComplete(t *testing.T) {
	ws := newWorkspace(t, map[string]string{
		"internal/agent/app.go": "x",
		"internal/agent/run.go": "y",
		"main.go":               "z",
	})
	idx := NewIndex(ws)

	input := "explain @internal/agent/a"
	comp := idx.Complete(input, len(input))
	if len(comp.Items) == 0 {
		t.Fatal("expected suggestions for @internal/agent/a")
	}
	if comp.ReplaceFrom != strings.Index(input, "@")+1 {
		t.Fatalf("ReplaceFrom = %d, want %d", comp.ReplaceFrom, strings.Index(input, "@")+1)
	}
	found := false
	for _, it := range comp.Items {
		if it.Insert == "internal/agent/app.go" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected app.go in suggestions, got %+v", comp.Items)
	}
}

func TestCompleteNoActiveToken(t *testing.T) {
	ws := newWorkspace(t, map[string]string{"a.go": "x"})
	idx := NewIndex(ws)
	input := "no mention here"
	if comp := idx.Complete(input, len(input)); len(comp.Items) != 0 {
		t.Fatalf("expected no items, got %+v", comp.Items)
	}
}

func TestNewIndexNilWorkspace(t *testing.T) {
	if NewIndex(nil) != nil {
		t.Fatal("NewIndex(nil) should be nil")
	}
	var idx *Index
	if comp := idx.Complete("@x", 2); len(comp.Items) != 0 {
		t.Fatalf("nil index should return no items, got %+v", comp.Items)
	}
}

func newWorkspace(t *testing.T, files map[string]string) *tools.Workspace {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		abs := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	ws, err := tools.NewWorkspace(root)
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	return ws
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
