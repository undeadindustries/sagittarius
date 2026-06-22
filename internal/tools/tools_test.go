package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

func TestReadFileTool(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "hello.txt")
	if err := os.WriteFile(path, []byte("line1\nline2\nline3"), 0o644); err != nil {
		t.Fatal(err)
	}

	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	tool := newReadFileTool(ws)

	tests := []struct {
		name    string
		args    map[string]any
		want    string
		wantErr bool
	}{
		{
			name: "full file",
			args: map[string]any{ParamFilePath: "hello.txt"},
			want: "line1\nline2\nline3",
		},
		{
			name: "line range",
			args: map[string]any{
				ParamFilePath:          "hello.txt",
				ReadFileParamStartLine: 2,
				ReadFileParamEndLine:   2,
			},
			want: "line2",
		},
		{
			name:    "traversal blocked",
			args:    map[string]any{ParamFilePath: "../outside.txt"},
			wantErr: true,
		},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tool.Execute(ctx, tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if got["content"] != tt.want {
				t.Fatalf("content = %q, want %q", got["content"], tt.want)
			}
		})
	}
}

func TestWriteFileConfirmation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewBuiltinRegistry(ws)

	tests := []struct {
		name        string
		mode        ApprovalMode
		wantFile    bool
		wantConfirm bool
	}{
		{name: "default denies headless write", mode: ApprovalDefault, wantFile: false, wantConfirm: true},
		{name: "auto_edit allows headless write", mode: ApprovalAutoEdit, wantFile: true, wantConfirm: false},
		{name: "yolo allows headless write", mode: ApprovalYolo, wantFile: true, wantConfirm: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			policy := Policy{Mode: tt.mode}
			tool, ok := registry.Lookup(WriteFileToolName)
			if !ok {
				t.Fatal("write_file not registered")
			}
			if policy.NeedsConfirmation(tool) != tt.wantConfirm {
				t.Fatalf("NeedsConfirmation = %v, want %v", policy.NeedsConfirmation(tool), tt.wantConfirm)
			}

			scheduler := NewScheduler(registry, policy, false, nil, ws)
			var confirms int
			emit := func(ev ui.StreamEvent) {
				if ev.Type == ui.StreamToolConfirm {
					confirms++
				}
			}
			// Each parallel subtest writes its own file so they cannot collide
			// on a shared target under the shared workspace root.
			fileName := string(tt.mode) + "-out.txt"
			_, err := scheduler.Execute(context.Background(), []provider.ToolCall{{
				Name: WriteFileToolName,
				Args: map[string]any{
					ParamFilePath:         fileName,
					WriteFileParamContent: "data",
				},
			}}, emit)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if confirms != 0 {
				t.Fatalf("headless confirms = %d, want 0", confirms)
			}

			target := filepath.Join(root, fileName)
			if fileExists(target) != tt.wantFile {
				t.Fatalf("file exists = %v, want %v", fileExists(target), tt.wantFile)
			}
		})
	}
}

func TestShellBlockedWhenDenied(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewBuiltinRegistry(ws)
	policy := Policy{Mode: ApprovalDefault}
	scheduler := NewScheduler(registry, policy, false, nil, ws)

	emit := func(ui.StreamEvent) {}
	responses, err := scheduler.Execute(context.Background(), []provider.ToolCall{{
		Name: ShellToolName,
		Args: map[string]any{ShellParamCommand: "echo hello"},
	}}, emit)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(responses) != 1 {
		t.Fatalf("responses = %d, want 1", len(responses))
	}
	if responses[0].Response["error"] != "user denied tool execution" {
		t.Fatalf("response = %#v, want denial", responses[0].Response)
	}

	tool, ok := registry.Lookup(ShellToolName)
	if !ok {
		t.Fatal("shell tool missing")
	}
	_, execErr := tool.Execute(context.Background(), map[string]any{
		ShellParamCommand: "rm -rf /tmp/sagittarius-test",
	})
	if execErr == nil {
		t.Fatal("dangerous rm -rf should be blocked by shell tool")
	}
}

func TestToolSchemaOpenAICompat(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewBuiltinRegistry(ws)
	decls := registry.ListDeclarations()
	if len(decls) < 5 {
		t.Fatalf("declarations = %d, want at least 5", len(decls))
	}

	for _, decl := range decls {
		if decl.Name == "" {
			t.Fatal("empty tool name")
		}
		if typ, _ := decl.Parameters["type"].(string); typ != "object" {
			t.Fatalf("tool %s parameters type = %v, want object", decl.Name, decl.Parameters["type"])
		}
		if _, ok := decl.Parameters["properties"]; !ok {
			t.Fatalf("tool %s missing properties", decl.Name)
		}
	}

	openai := provider.ToolDeclarationsToOpenAI(decls)
	if len(openai) != len(decls) {
		t.Fatalf("openai tools = %d, want %d", len(openai), len(decls))
	}
	for _, tool := range openai {
		if tool.Type != "function" || tool.Function.Name == "" {
			t.Fatalf("invalid openai tool %#v", tool)
		}
	}
}

func TestRipgrepIntegration(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not in PATH")
	}

	root := t.TempDir()
	path := filepath.Join(root, "search.txt")
	if err := os.WriteFile(path, []byte("findme here\nother\nfindme again"), 0o644); err != nil {
		t.Fatal(err)
	}

	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	tool := newGrepTool(ws)
	got, err := tool.Execute(context.Background(), map[string]any{
		ParamPattern: "findme",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	matches, _ := got["matches"].(string)
	if matches == "" || matches == "(no matches)" {
		t.Fatalf("matches = %q, want hits", matches)
	}
}

func TestPathTraversalBlocked(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ws.ResolvePath("../../etc/passwd")
	if err == nil {
		t.Fatal("expected traversal outside workspace to fail")
	}
}

func TestIsDangerousCommand(t *testing.T) {
	t.Parallel()
	if !IsDangerousCommand("rm -rf /tmp") {
		t.Fatal("rm -rf should be dangerous")
	}
	if IsDangerousCommand("echo hello") {
		t.Fatal("echo should not be dangerous")
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
