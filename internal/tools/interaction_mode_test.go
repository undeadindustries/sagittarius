package tools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/modes"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

func TestResolvePlanPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	plansDir := filepath.Join(root, PlansDirRelative)
	if err := os.MkdirAll(plansDir, 0o755); err != nil {
		t.Fatal(err)
	}
	planFile := filepath.Join(plansDir, "phase-01.md")
	if err := os.WriteFile(planFile, []byte("plan"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{name: "relative to plans dir", path: "phase-01.md"},
		{name: "relative to project root", path: PlansDirRelative + "/phase-01.md"},
		{name: "absolute under plans", path: planFile},
		{name: "outside plans absolute", path: filepath.Join(root, "README.md"), wantErr: true},
		{name: "traversal escape", path: "../outside.md", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := ResolvePlanPath(tt.path, root, plansDir)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolvePlanPath: %v", err)
			}
		})
	}
}

func TestInteractionModeAllowAskBlocksWrites(t *testing.T) {
	t.Parallel()

	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	allowed, reason := InteractionModeAllow(modes.ModeAsk, WriteFileToolName, map[string]any{
		ParamFilePath:         "notes.md",
		WriteFileParamContent: "hi",
	}, ws)
	if allowed || reason == "" {
		t.Fatalf("ask mode should block write_file, got allowed=%v reason=%q", allowed, reason)
	}

	allowed, _ = InteractionModeAllow(modes.ModeAsk, ReadFileToolName, map[string]any{
		ParamFilePath: "notes.md",
	}, ws)
	if !allowed {
		t.Fatal("ask mode should allow read_file")
	}

	allowed, _ = InteractionModeAllow(modes.ModeAsk, ShellToolName, map[string]any{
		ShellParamCommand: "git status",
	}, ws)
	if allowed {
		t.Fatal("ask mode should block shell")
	}
}

func TestInteractionModeAllowPlanWritesPlansOnly(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	plansDir := filepath.Join(root, PlansDirRelative)
	if err := os.MkdirAll(plansDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}

	allowed, _ := InteractionModeAllow(modes.ModePlan, WriteFileToolName, map[string]any{
		ParamFilePath:         PlansDirRelative + "/new-plan.md",
		WriteFileParamContent: "plan body",
	}, ws)
	if !allowed {
		t.Fatal("plan mode should allow write under docs/plans")
	}

	allowed, reason := InteractionModeAllow(modes.ModePlan, WriteFileToolName, map[string]any{
		ParamFilePath:         filepath.Join(root, "src", "main.go"),
		WriteFileParamContent: "code",
	}, ws)
	if allowed || reason == "" {
		t.Fatalf("plan mode should block source writes, got allowed=%v reason=%q", allowed, reason)
	}
}

func TestListDeclarationsForMode(t *testing.T) {
	t.Parallel()

	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	registry := NewBuiltinRegistry(ws)

	agentDecls := registry.ListDeclarationsForMode(modes.ModeAgent)
	askDecls := registry.ListDeclarationsForMode(modes.ModeAsk)
	planDecls := registry.ListDeclarationsForMode(modes.ModePlan)

	if len(askDecls) >= len(agentDecls) {
		t.Fatalf("ask declarations = %d, want fewer than agent %d", len(askDecls), len(agentDecls))
	}
	if len(planDecls) >= len(agentDecls) {
		t.Fatalf("plan declarations = %d, want fewer than agent %d", len(planDecls), len(agentDecls))
	}

	assertHasTool(t, askDecls, ReadFileToolName)
	assertHasTool(t, askDecls, GrepToolName)
	assertMissingTool(t, askDecls, WriteFileToolName)
	assertMissingTool(t, askDecls, ShellToolName)

	assertHasTool(t, planDecls, WriteFileToolName)
	assertMissingTool(t, planDecls, ShellToolName)
}

func assertHasTool(t *testing.T, decls []provider.ToolDeclaration, name string) {
	t.Helper()
	for _, d := range decls {
		if d.Name == name {
			return
		}
	}
	t.Fatalf("expected tool %q in declarations", name)
}

func assertMissingTool(t *testing.T, decls []provider.ToolDeclaration, name string) {
	t.Helper()
	for _, d := range decls {
		if d.Name == name {
			t.Fatalf("tool %q should not be in declarations", name)
		}
	}
}

func TestSchedulerAskModeBlocksWrite(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewBuiltinRegistry(ws)
	mode := modes.ModeAsk
	scheduler := NewScheduler(registry, Policy{Mode: ApprovalAutoEdit}, false, func() modes.Mode {
		return mode
	}, ws)

	responses, err := scheduler.Execute(t.Context(), []provider.ToolCall{{
		Name: WriteFileToolName,
		Args: map[string]any{
			ParamFilePath:         "out.txt",
			WriteFileParamContent: "data",
		},
	}}, func(ui.StreamEvent) {})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(responses) != 1 {
		t.Fatalf("responses = %d, want 1", len(responses))
	}
	if responses[0].Response["error"] == nil {
		t.Fatalf("expected error response, got %#v", responses[0].Response)
	}
	if fileExists(filepath.Join(root, "out.txt")) {
		t.Fatal("file should not have been written")
	}
}
