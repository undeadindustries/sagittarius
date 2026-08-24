package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/modes"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

func TestScriptToolDisabledByDefault(t *testing.T) {
	t.Parallel()
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reg := NewBuiltinRegistry(ws)
	if _, ok := reg.Lookup(ScriptToolName); ok {
		t.Fatal("run_script should be off by default")
	}
	regOn := NewBuiltinRegistry(ws, WithScriptTool(true))
	if _, ok := regOn.Lookup(ScriptToolName); !ok {
		t.Fatal("run_script should register when enabled")
	}
}

func TestScriptToolRunsReadOnlyBatch(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.go"), []byte("package b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	reg := NewBuiltinRegistry(ws, WithScriptTool(true), WithSymbols(false, false))
	tool, ok := reg.Lookup(ScriptToolName)
	if !ok {
		t.Fatal("run_script missing")
	}

	script := `{
	  "operations": [
	    {"tool":"list_directory","args":{"dir_path":"."}},
	    {"tool":"read_file","args":{"file_path":"a.go"}},
	    {"tool":"read_file","args":{"file_path":"b.go"}},
	    {"tool":"grep_search","args":{"pattern":"package","dir_path":"."}},
	    {"tool":"list_directory","args":{"dir_path":"."}}
	  ]
	}`
	res, err := tool.Execute(context.Background(), map[string]any{ScriptParamScript: script})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	raw, ok := res["results"].([]map[string]any)
	if !ok {
		// JSON round-trip type: []any
		items, ok := res["results"].([]any)
		if !ok {
			t.Fatalf("results type %T", res["results"])
		}
		if len(items) != 5 {
			t.Fatalf("got %d results, want 5", len(items))
		}
		for i, item := range items {
			m, ok := item.(map[string]any)
			if !ok {
				t.Fatalf("result %d type %T", i, item)
			}
			if m["error"] != nil {
				t.Fatalf("result %d error: %v", i, m["error"])
			}
			if m["output"] == nil {
				t.Fatalf("result %d missing output", i)
			}
		}
		return
	}
	if len(raw) != 5 {
		t.Fatalf("got %d results, want 5", len(raw))
	}
}

func TestScriptToolRejectsMutatingTool(t *testing.T) {
	t.Parallel()
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reg := NewBuiltinRegistry(ws, WithScriptTool(true), WithSymbols(false, false))
	tool, _ := reg.Lookup(ScriptToolName)
	script := `{"operations":[{"tool":"write_file","args":{"file_path":"x.txt","content":"no"}}]}`
	res, err := tool.Execute(context.Background(), map[string]any{ScriptParamScript: script})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	items, _ := res["results"].([]map[string]any)
	if len(items) != 1 {
		t.Fatalf("results = %#v", res["results"])
	}
	if items[0]["code"] != string(ErrCodeModeRestriction) {
		t.Fatalf("code = %v, want MODE_RESTRICTION", items[0]["code"])
	}
	if _, err := os.Stat(filepath.Join(ws.Root(), "x.txt")); !os.IsNotExist(err) {
		t.Fatal("write_file ran inside run_script")
	}
}

func TestScriptToolVisibleInAskMode(t *testing.T) {
	t.Parallel()
	if !ToolVisibleInMode(modes.ModeAsk, ScriptToolName) {
		t.Fatal("run_script should be visible in ask mode")
	}
}

func TestScriptNestedBeforeToolRewrite(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.go"), []byte("package b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	reg := NewBuiltinRegistry(ws, WithScriptTool(true), WithSymbols(false, false))
	before := func(_ context.Context, name string, _ map[string]any) (map[string]any, bool, string, error) {
		if name == ReadFileToolName {
			return map[string]any{ParamFilePath: "b.go"}, false, "", nil
		}
		return nil, false, "", nil
	}
	sched := NewScheduler(reg, Policy{Mode: ApprovalYolo}, false, nil, ws, WithHooks(before, nil))
	items := executeScript(t, sched, `{"operations":[{"tool":"read_file","args":{"file_path":"a.go"}}]}`)
	if items[0]["error"] != nil {
		t.Fatalf("op error: %v", items[0]["error"])
	}
	out, _ := items[0]["output"].(map[string]any)
	if out["content"] != "package b\n" {
		t.Fatalf("hook rewrite did not reach nested read: %#v", items[0])
	}
}

func TestScriptNestedHookDenyLeavesSiblings(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.go"), []byte("package b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	reg := NewBuiltinRegistry(ws, WithScriptTool(true), WithSymbols(false, false))
	before := func(_ context.Context, name string, args map[string]any) (map[string]any, bool, string, error) {
		if name == ReadFileToolName {
			if path, _ := args[ParamFilePath].(string); path == "a.go" {
				return nil, true, "blocked a.go", nil
			}
		}
		return nil, false, "", nil
	}
	sched := NewScheduler(reg, Policy{Mode: ApprovalYolo}, false, nil, ws, WithHooks(before, nil))
	items := executeScript(t, sched, `{
	  "operations": [
	    {"tool":"read_file","args":{"file_path":"a.go"}},
	    {"tool":"read_file","args":{"file_path":"b.go"}}
	  ]
	}`)
	if items[0]["code"] != string(ErrCodeHookDenied) {
		t.Fatalf("first code = %v, want HOOK_DENIED", items[0]["code"])
	}
	if items[1]["error"] != nil {
		t.Fatalf("sibling should still run: %v", items[1]["error"])
	}
	out, _ := items[1]["output"].(map[string]any)
	if out["content"] != "package b\n" {
		t.Fatalf("sibling output = %#v", items[1])
	}
}

func TestScriptNestedAfterToolFiresPerOp(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.go"), []byte("package b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	reg := NewBuiltinRegistry(ws, WithScriptTool(true), WithSymbols(false, false))
	var seen []string
	after := func(_ context.Context, name string, _ map[string]any, _ map[string]any) {
		seen = append(seen, name)
	}
	sched := NewScheduler(reg, Policy{Mode: ApprovalYolo}, false, nil, ws, WithHooks(nil, after))
	_ = executeScript(t, sched, `{
	  "operations": [
	    {"tool":"read_file","args":{"file_path":"a.go"}},
	    {"tool":"read_file","args":{"file_path":"b.go"}}
	  ]
	}`)
	reads := 0
	for _, name := range seen {
		if name == ReadFileToolName {
			reads++
		}
	}
	if reads != 2 {
		t.Fatalf("AfterTool fired on read_file %d times, want 2 (%v)", reads, seen)
	}
}

func TestScriptNestedRewriteRevalidated(t *testing.T) {
	t.Parallel()
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reg := NewBuiltinRegistry(ws, WithScriptTool(true), WithSymbols(false, false))
	before := func(_ context.Context, name string, _ map[string]any) (map[string]any, bool, string, error) {
		if name == ProjectChecksToolName {
			return map[string]any{ProjectChecksParamFix: true}, false, "", nil
		}
		return nil, false, "", nil
	}
	ask := func() modes.Mode { return modes.ModeAsk }
	sched := NewScheduler(reg, Policy{Mode: ApprovalYolo}, false, ask, ws, WithHooks(before, nil))
	items := executeScript(t, sched, `{"operations":[{"tool":"run_project_checks","args":{}}]}`)
	if items[0]["code"] != string(ErrCodeModeRestriction) {
		t.Fatalf("rewrite escape not caught: %#v", items[0])
	}
}

func executeScript(t *testing.T, sched *Scheduler, script string) []map[string]any {
	t.Helper()
	resps, err := sched.Execute(context.Background(), []provider.ToolCall{{
		Name: ScriptToolName,
		Args: map[string]any{ScriptParamScript: script},
	}}, func(ui.StreamEvent) {})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(resps) != 1 {
		t.Fatalf("got %d responses", len(resps))
	}
	raw, ok := resps[0].Response["results"].([]map[string]any)
	if !ok {
		t.Fatalf("results type %T", resps[0].Response["results"])
	}
	if len(raw) == 0 {
		t.Fatal("empty script results")
	}
	return raw
}

func TestParseScriptBareArray(t *testing.T) {
	t.Parallel()
	ops, err := parseScript(`[{"tool":"read_file","args":{"file_path":"a.go"}}]`)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 || ops[0].Tool != "read_file" {
		t.Fatalf("ops = %+v", ops)
	}
	raw, _ := json.Marshal(ops)
	if len(raw) == 0 {
		t.Fatal("marshal")
	}
}
