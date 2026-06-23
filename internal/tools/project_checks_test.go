package tools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/modes"
	"github.com/undeadindustries/sagittarius/internal/tools/checks"
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

func TestCheckArgvNarrowsFileScoped(t *testing.T) {
	t.Parallel()

	fileScoped := checks.Check{Name: "format", Command: "gofmt", Args: []string{"-l", "."}, FileScoped: true}
	got := checkArgv(fileScoped, []string{"a.go", "b.go"})
	want := []string{"-l", "a.go", "b.go"}
	if len(got) != len(want) {
		t.Fatalf("argv = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("argv = %v, want %v", got, want)
		}
	}

	moduleScoped := checks.Check{Name: "vet", Command: "go", Args: []string{"vet", "./..."}}
	got = checkArgv(moduleScoped, []string{"a.go"})
	if len(got) != 2 || got[1] != "./..." {
		t.Fatalf("module-scoped argv should be unchanged, got %v", got)
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
