package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/modes"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

// A BeforeTool hook returning only the keys it cares about must not wipe the
// rest of the call. write_file without content would fail validation, so a
// replace-instead-of-merge regression shows up as a lost file.
func TestBeforeToolHookMergesArgs(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewBuiltinRegistry(ws)

	before := func(_ context.Context, _ string, _ map[string]any) (map[string]any, bool, string, error) {
		return map[string]any{ParamFilePath: "redirected.txt"}, false, "", nil
	}
	scheduler := NewScheduler(registry, Policy{Mode: ApprovalYolo}, false, nil, ws,
		WithHooks(before, nil))

	calls := []provider.ToolCall{
		{Name: WriteFileToolName, Args: map[string]any{ParamFilePath: "original.txt", WriteFileParamContent: "kept\n"}},
	}
	if _, err := scheduler.Execute(context.Background(), calls, func(ui.StreamEvent) {}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(root, "redirected.txt"))
	if err != nil {
		t.Fatalf("hook-redirected write missing (content likely dropped by a replace): %v", err)
	}
	if string(got) != "kept\n" {
		t.Fatalf("content = %q, want %q", string(got), "kept\n")
	}
	if fileExists(filepath.Join(root, "original.txt")) {
		t.Fatal("original path should not have been written")
	}
}

// The interaction-mode gate is scheduler-only: no tool repeats it. In plan mode
// write_file is allowed only under docs/plans/, so a hook rewriting the path to a
// source file would escape plan mode entirely unless the gate is re-applied.
func TestBeforeToolHookRewriteIsRevalidated(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, PlansDirRelative), 0o755); err != nil {
		t.Fatal(err)
	}
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewBuiltinRegistry(ws)

	// Absolute and outside docs/plans: a bare relative name would resolve *into*
	// the plans dir and be legitimately allowed.
	escaped := filepath.Join(root, "main.go")
	before := func(_ context.Context, _ string, _ map[string]any) (map[string]any, bool, string, error) {
		return map[string]any{ParamFilePath: escaped}, false, "", nil
	}
	planMode := func() modes.Mode { return modes.ModePlan }
	scheduler := NewScheduler(registry, Policy{Mode: ApprovalYolo}, false, planMode, ws,
		WithHooks(before, nil))

	var errText string
	emit := func(ev ui.StreamEvent) {
		if ev.Type == ui.StreamToolResult && ev.IsError {
			errText = ev.Text
		}
	}
	calls := []provider.ToolCall{
		{Name: WriteFileToolName, Args: map[string]any{
			ParamFilePath:         PlansDirRelative + "/notes.md",
			WriteFileParamContent: "plan notes\n",
		}},
	}
	if _, err := scheduler.Execute(context.Background(), calls, emit); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if fileExists(escaped) {
		t.Fatal("hook rewrote the path out of docs/plans and plan mode did not stop it")
	}
	if errText == "" {
		t.Fatal("expected the rewritten path to be rejected by the plan-mode gate")
	}
}

// A denying hook must short-circuit before the tool runs.
func TestBeforeToolHookDenyBlocksExecution(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewBuiltinRegistry(ws)

	before := func(_ context.Context, _ string, _ map[string]any) (map[string]any, bool, string, error) {
		return nil, true, "blocked by policy", nil
	}
	scheduler := NewScheduler(registry, Policy{Mode: ApprovalYolo}, false, nil, ws,
		WithHooks(before, nil))

	var errText string
	emit := func(ev ui.StreamEvent) {
		if ev.Type == ui.StreamToolResult && ev.IsError {
			errText = ev.Text
		}
	}
	calls := []provider.ToolCall{
		{Name: WriteFileToolName, Args: map[string]any{ParamFilePath: "nope.txt", WriteFileParamContent: "x\n"}},
	}
	if _, err := scheduler.Execute(context.Background(), calls, emit); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if fileExists(filepath.Join(root, "nope.txt")) {
		t.Fatal("denied tool still executed")
	}
	if !strings.Contains(errText, "blocked by policy") {
		t.Fatalf("expected the hook reason in the error, got %q", errText)
	}
}
