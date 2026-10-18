package tools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/modes"
)

func goWorkspace(t *testing.T) *Workspace {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

func TestProjectChecksFixGatedByConfig(t *testing.T) {
	ws := goWorkspace(t)
	tool := newProjectChecksTool(ws, false)

	res, err := tool.Execute(t.Context(), map[string]any{ProjectChecksParamFix: true})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res["error"] == nil {
		t.Fatalf("fix=true with allowFix=false should error, got %#v", res)
	}
}

func TestProjectChecksMissingTools(t *testing.T) {
	ws := goWorkspace(t)
	// Empty PATH makes every command lookup fail, so all checks are reported as
	// missing tools and none execute.
	t.Setenv("PATH", "")
	tool := newProjectChecksTool(ws, false)

	res, err := tool.Execute(t.Context(), map[string]any{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res["stack"] != "go" {
		t.Fatalf("stack = %v, want go", res["stack"])
	}
	missing, ok := res["missing_tools"].([]any)
	if !ok || len(missing) == 0 {
		t.Fatalf("expected missing_tools, got %#v", res["missing_tools"])
	}
	first, _ := missing[0].(map[string]any)
	if first["install_hint"] == "" || first["install_hint"] == nil {
		t.Fatalf("missing tool should carry an install hint, got %#v", first)
	}
	if res["all_ok"] != true {
		t.Fatalf("all_ok should be true when no checks executed, got %v", res["all_ok"])
	}
}

func TestProjectChecksNoStack(t *testing.T) {
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tool := newProjectChecksTool(ws, false)

	res, err := tool.Execute(t.Context(), map[string]any{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res["stack"] != "" {
		t.Fatalf("stack = %v, want empty", res["stack"])
	}
	if res["all_ok"] != true {
		t.Fatalf("all_ok = %v, want true", res["all_ok"])
	}
}

func TestProjectChecksPathsValidated(t *testing.T) {
	ws := goWorkspace(t)
	tool := newProjectChecksTool(ws, false)

	_, err := tool.Execute(t.Context(), map[string]any{
		ProjectChecksParamPaths: []any{"../escape.go"},
	})
	if err == nil {
		t.Fatal("expected error for path outside workspace")
	}
}

// TestProjectChecksPathsRejectFlags guards against flag injection: a path that
// looks like a flag (e.g. "--fix") must be rejected so it can never reach the
// underlying tool and bypass the allowFix gate or the plan/ask read-only rules.
func TestProjectChecksPathsRejectFlags(t *testing.T) {
	ws := goWorkspace(t)
	tool := newProjectChecksTool(ws, false)

	for _, bad := range []string{"--fix", "-l", "--write"} {
		_, err := tool.Execute(t.Context(), map[string]any{
			ProjectChecksParamPaths: []any{bad},
		})
		if err == nil {
			t.Fatalf("path %q should be rejected as a flag", bad)
		}
	}
}

// checkEntry returns the result entry for the named check, or nil.
func checkEntry(t *testing.T, res map[string]any, name string) map[string]any {
	t.Helper()
	list, ok := res["checks"].([]any)
	if !ok {
		t.Fatalf("checks = %#v, want a list", res["checks"])
	}
	for _, item := range list {
		if e, _ := item.(map[string]any); e["name"] == name {
			return e
		}
	}
	return nil
}

// TestProjectChecksNarrowsPathsPerCheck is the regression for mypy being handed
// pyproject.toml (it parses every argument as Python and reported a bogus
// syntax error). A fake mypy on PATH echoes its arguments so the test sees the
// exact argv; ruff is absent, so only mypy runs.
func TestProjectChecksNarrowsPathsPerCheck(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"pyproject.toml": "[project]\nname = \"x\"\nrequires-python = \">=3.11\"\n",
		"app.py":         "x = 1\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "mypy"), []byte("#!/bin/sh\necho \"args: $*\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	tool := newProjectChecksTool(ws, false)

	res, err := tool.Execute(t.Context(), map[string]any{
		ProjectChecksParamPaths: []any{"app.py", "pyproject.toml", "pkg"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	mypy := checkEntry(t, res, "typecheck")
	if mypy == nil {
		t.Fatalf("no typecheck entry in %#v", res["checks"])
	}
	if got := mypy["output"]; got != "args: app.py pkg" {
		t.Fatalf("mypy argv output = %q, want %q", got, "args: app.py pkg")
	}

	res, err = tool.Execute(t.Context(), map[string]any{
		ProjectChecksParamPaths: []any{"pyproject.toml"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	mypy = checkEntry(t, res, "typecheck")
	if mypy == nil || mypy["skipped"] == nil || mypy["ok"] != true {
		t.Fatalf("mypy with only pyproject.toml should be skipped and ok, got %#v", mypy)
	}
	for _, item := range res["missing_tools"].([]any) {
		if m, _ := item.(map[string]any); m["tool"] == "ruff" {
			t.Fatalf("ruff reported missing although no path applied to it: %#v", res["missing_tools"])
		}
	}
	if res["all_ok"] != true {
		t.Fatalf("all_ok = %v, want true when every scoped check was skipped", res["all_ok"])
	}
}

func TestProjectChecksModeGates(t *testing.T) {
	t.Parallel()

	ws := goWorkspace(t)

	// Check-only is allowed in plan and ask.
	for _, mode := range []modes.Mode{modes.ModePlan, modes.ModeAsk} {
		allowed, reason := InteractionModeAllow(mode, ProjectChecksToolName, map[string]any{}, ws)
		if !allowed {
			t.Fatalf("%v should allow check-only run_project_checks: %s", mode, reason)
		}
	}

	// fix=true is denied in plan and ask.
	for _, mode := range []modes.Mode{modes.ModePlan, modes.ModeAsk} {
		allowed, reason := InteractionModeAllow(mode, ProjectChecksToolName, map[string]any{
			ProjectChecksParamFix: true,
		}, ws)
		if allowed || reason == "" {
			t.Fatalf("%v should deny fix=true, got allowed=%v reason=%q", mode, allowed, reason)
		}
	}

	// Agent mode allows both.
	allowed, _ := InteractionModeAllow(modes.ModeAgent, ProjectChecksToolName, map[string]any{
		ProjectChecksParamFix: true,
	}, ws)
	if !allowed {
		t.Fatal("agent mode should allow fix=true")
	}
}

func TestProjectChecksVisibleInPlanAsk(t *testing.T) {
	t.Parallel()

	ws := goWorkspace(t)
	registry := NewBuiltinRegistry(ws)
	assertHasTool(t, registry.ListDeclarationsForMode(modes.ModePlan), ProjectChecksToolName)
	assertHasTool(t, registry.ListDeclarationsForMode(modes.ModeAsk), ProjectChecksToolName)
}
