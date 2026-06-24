package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFileRejectsEjectionMarker(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	tool := newWriteFileTool(ws)
	tag := "<file_written path=\"js/apps/snake.js\" lines=166 tokens=1296 cached=true>"
	_, err = tool.Execute(context.Background(), map[string]any{
		ParamFilePath:         "snake.js",
		WriteFileParamContent: tag,
	})
	if err == nil {
		t.Fatal("expected error for ejection marker content")
	}
}

func TestWriteFileRejectsDiffContent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	tool := newWriteFileTool(ws)
	diffBody := "-  goBack() {\n-    this.render();\n+      this.os.openApp(file.name);\n   }"
	_, err = tool.Execute(context.Background(), map[string]any{
		ParamFilePath:         "apps/fileExplorer.js",
		WriteFileParamContent: diffBody,
	})
	if err == nil {
		t.Fatal("expected error for diff-like content")
	}
	if !strings.Contains(err.Error(), "unified diff") {
		t.Fatalf("error = %v, want unified diff mention", err)
	}
	target := filepath.Join(root, "apps", "fileExplorer.js")
	if _, statErr := os.Stat(target); statErr == nil {
		t.Fatal("file should not have been written")
	}
}

func TestWriteFileRejectsPlaceholderContent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	tool := newWriteFileTool(ws)
	_, err = tool.Execute(context.Background(), map[string]any{
		ParamFilePath:         "x.js",
		WriteFileParamContent: "const a = 1;\n// ... existing code ...\nconst b = 2;",
	})
	if err == nil {
		t.Fatal("expected error for placeholder content")
	}
}

func TestWriteFileAcceptsCompleteFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	tool := newWriteFileTool(ws)
	body := "class FileExplorer {}\nwindow.FileExplorer = FileExplorer;\n"
	_, err = tool.Execute(context.Background(), map[string]any{
		ParamFilePath:         "fileExplorer.js",
		WriteFileParamContent: body,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "fileExplorer.js"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Fatalf("file content = %q, want %q", string(got), body)
	}
}
