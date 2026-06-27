package tools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

// TestSessionGrantSkipsLaterConfirmations verifies that answering a write_file
// confirmation with ConfirmSession suppresses confirmation for subsequent
// write_file calls in the same scheduler.
func TestSessionGrantSkipsLaterConfirmations(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewBuiltinRegistry(ws)
	scheduler := NewScheduler(registry, Policy{Mode: ApprovalDefault}, true, nil, ws)

	var confirms int
	emit := func(ev ui.StreamEvent) {
		if ev.Type == ui.StreamToolConfirm {
			confirms++
			ev.ConfirmReply <- ui.ConfirmSession
		}
	}

	calls := []provider.ToolCall{
		{Name: WriteFileToolName, Args: map[string]any{ParamFilePath: "a.txt", WriteFileParamContent: "one"}},
		{Name: WriteFileToolName, Args: map[string]any{ParamFilePath: "b.txt", WriteFileParamContent: "two"}},
	}
	if _, err := scheduler.Execute(context.Background(), calls, emit); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if confirms != 1 {
		t.Fatalf("confirms = %d, want 1 (session grant should skip the second)", confirms)
	}
	for _, f := range []string{"a.txt", "b.txt"} {
		if !fileExists(filepath.Join(root, f)) {
			t.Fatalf("%s should have been written", f)
		}
	}
}

// TestConfirmOnceDoesNotGrantSession verifies ConfirmOnce approves only the
// single call and the next call still prompts.
func TestConfirmOnceDoesNotGrantSession(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewBuiltinRegistry(ws)
	scheduler := NewScheduler(registry, Policy{Mode: ApprovalDefault}, true, nil, ws)

	var confirms int
	emit := func(ev ui.StreamEvent) {
		if ev.Type == ui.StreamToolConfirm {
			confirms++
			ev.ConfirmReply <- ui.ConfirmOnce
		}
	}

	calls := []provider.ToolCall{
		{Name: WriteFileToolName, Args: map[string]any{ParamFilePath: "a.txt", WriteFileParamContent: "one"}},
		{Name: WriteFileToolName, Args: map[string]any{ParamFilePath: "b.txt", WriteFileParamContent: "two"}},
	}
	if _, err := scheduler.Execute(context.Background(), calls, emit); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if confirms != 2 {
		t.Fatalf("confirms = %d, want 2 (once does not grant a session)", confirms)
	}
}

// TestConfirmDiffPreviewAndResult verifies the confirm event carries a unified
// diff preview and the tool result emits the diff instead of "ok".
func TestConfirmDiffPreviewAndResult(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewBuiltinRegistry(ws)
	scheduler := NewScheduler(registry, Policy{Mode: ApprovalDefault}, true, nil, ws)

	var preview, result string
	emit := func(ev ui.StreamEvent) {
		switch ev.Type {
		case ui.StreamToolConfirm:
			preview = ev.Diff
			ev.ConfirmReply <- ui.ConfirmOnce
		case ui.StreamToolResult:
			result = ev.Text
		}
	}

	calls := []provider.ToolCall{
		{Name: WriteFileToolName, Args: map[string]any{ParamFilePath: "new.txt", WriteFileParamContent: "hello\nworld\n"}},
	}
	if _, err := scheduler.Execute(context.Background(), calls, emit); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !strings.Contains(preview, "+hello") || !strings.Contains(preview, "+world") {
		t.Fatalf("confirm preview missing added lines: %q", preview)
	}
	if !strings.Contains(result, "+hello") {
		t.Fatalf("result should be the diff, got %q", result)
	}
}

func TestFormatToolSummary(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		tool string
		args map[string]any
		want string
	}{
		{"write path", WriteFileToolName, map[string]any{ParamFilePath: "pkg/x.go"}, "pkg/x.go"},
		{"shell command", ShellToolName, map[string]any{ShellParamCommand: "go build ./..."}, "go build ./..."},
		{"shell multiline truncated to first line", ShellToolName, map[string]any{ShellParamCommand: "echo a\necho b"}, "echo a"},
		{"unknown tool", "grep_search", map[string]any{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := formatToolSummary(tc.tool, tc.args); got != tc.want {
				t.Fatalf("formatToolSummary = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFormatToolResult(t *testing.T) {
	t.Parallel()

	t.Run("write_file uses the diff verbatim", func(t *testing.T) {
		t.Parallel()
		text, code, isErr := formatToolResult(WriteFileToolName, map[string]any{"status": "ok"}, "+added")
		if text != "+added" || code != nil || isErr {
			t.Fatalf("got (%q, %v, %v)", text, code, isErr)
		}
	})

	t.Run("shell success carries output and zero exit", func(t *testing.T) {
		t.Parallel()
		text, code, isErr := formatToolResult(ShellToolName, map[string]any{"output": "done", "exit_code": 0}, "")
		if text != "done" || code == nil || *code != 0 || isErr {
			t.Fatalf("got (%q, %v, %v)", text, code, isErr)
		}
	})

	t.Run("shell non-zero exit is an error", func(t *testing.T) {
		t.Parallel()
		text, code, isErr := formatToolResult(ShellToolName, map[string]any{"output": "boom", "exit_code": 2}, "")
		if text != "boom" || code == nil || *code != 2 || !isErr {
			t.Fatalf("got (%q, %v, %v)", text, code, isErr)
		}
	})

	t.Run("error key wins", func(t *testing.T) {
		t.Parallel()
		text, _, isErr := formatToolResult(GrepToolName, map[string]any{"error": "no rg"}, "")
		if text != "no rg" || !isErr {
			t.Fatalf("got (%q, isErr=%v)", text, isErr)
		}
	})

	t.Run("read_file summarizes path and line count", func(t *testing.T) {
		t.Parallel()
		text, _, _ := formatToolResult(ReadFileToolName, map[string]any{"file_path": "a.txt", "content": "x\ny\nz"}, "")
		if text != "Read a.txt (3 lines)" {
			t.Fatalf("got %q", text)
		}
	})

	t.Run("mcp result stringified", func(t *testing.T) {
		t.Parallel()
		text, _, isErr := formatToolResult("mcp_srv_do", map[string]any{"result": "hi there"}, "")
		if text != "hi there" || isErr {
			t.Fatalf("got (%q, isErr=%v)", text, isErr)
		}
	})
}

func TestCapLinesKeepsTail(t *testing.T) {
	t.Parallel()
	in := "1\n2\n3\n4\n5"
	got := capLines(in, 2)
	if !strings.Contains(got, "… 3 more lines") || !strings.HasSuffix(got, "4\n5") {
		t.Fatalf("capLines = %q", got)
	}
}
