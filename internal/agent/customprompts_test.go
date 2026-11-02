package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/customprompt"
	"github.com/undeadindustries/sagittarius/internal/slash"
)

func customPromptTestApp(t *testing.T, withProject bool) *App {
	t.Helper()
	t.Setenv("SAGITTARIUS_HOME", t.TempDir())
	workDir := t.TempDir()
	docs, err := config.LoadDocuments(workDir)
	if err != nil {
		t.Fatalf("LoadDocuments: %v", err)
	}
	docs.Global.Sagittarius = &config.SagittariusSettings{
		SystemPrompt: &config.SagittariusSystemPromptConfig{Custom: "atlas"},
	}
	if err := docs.Save(config.ScopeGlobal); err != nil {
		t.Fatalf("save global: %v", err)
	}
	if withProject {
		p := docs.TargetSettings(config.ScopeProject)
		p.Sagittarius = &config.SagittariusSettings{
			SystemPrompt: &config.SagittariusSystemPromptConfig{Custom: "atlas"},
		}
		if err := docs.Save(config.ScopeProject); err != nil {
			t.Fatalf("save project: %v", err)
		}
	}
	return &App{docs: docs, deps: slash.Deps{Settings: docs.Global}}
}

func TestRetargetCustomPromptSelection(t *testing.T) {
	app := customPromptTestApp(t, true)

	changed, err := app.retargetCustomPromptSelection("atlas", "orion")
	if err != nil || !changed {
		t.Fatalf("retarget = %v, %v; want changed", changed, err)
	}
	for _, s := range []*config.Settings{app.docs.Global, app.docs.Project} {
		if got := s.Sagittarius.SystemPrompt.Custom; got != "orion" {
			t.Fatalf("custom = %q, want orion", got)
		}
	}

	reloaded, err := config.LoadDocuments(app.docs.WorkDir())
	if err != nil {
		t.Fatal(err)
	}
	if got := config.ProjectSystemPromptPresetID(reloaded.Merged()); got != "orion" {
		t.Fatalf("persisted selection = %q, want orion", got)
	}

	changed, err = app.retargetCustomPromptSelection("orion", "")
	if err != nil || !changed {
		t.Fatalf("clear = %v, %v", changed, err)
	}
	if app.docs.Global.Sagittarius.SystemPrompt != nil || app.docs.Project.Sagittarius.SystemPrompt != nil {
		t.Fatal("clearing should drop the empty systemPrompt object")
	}

	changed, err = app.retargetCustomPromptSelection("nobody", "x")
	if err != nil || changed {
		t.Fatalf("unrelated retarget = %v, %v; want unchanged", changed, err)
	}
}

func TestRetargetDoesNotCreateProjectFile(t *testing.T) {
	app := customPromptTestApp(t, false)
	if _, err := app.retargetCustomPromptSelection("atlas", "orion"); err != nil {
		t.Fatal(err)
	}
	if app.docs.Project != nil {
		t.Fatal("retarget must not create a project settings document")
	}
}

func TestDeleteAndRenameUnselectedPrompt(t *testing.T) {
	app := customPromptTestApp(t, false)
	ctx := context.Background()

	if _, err := app.SaveCustomPrompt(ctx, "Spare Prompt", "Spare text.", true); err != nil {
		t.Fatalf("SaveCustomPrompt: %v", err)
	}
	if _, err := app.SaveCustomPrompt(ctx, "spare-prompt", "again", true); !errors.Is(err, customprompt.ErrExists) {
		t.Fatalf("create over existing err = %v, want ErrExists", err)
	}
	if _, err := app.RenameCustomPrompt(ctx, "spare-prompt", "backup"); err != nil {
		t.Fatalf("RenameCustomPrompt: %v", err)
	}
	names, err := app.ListCustomPrompts()
	if err != nil || len(names) != 1 || names[0] != "backup" {
		t.Fatalf("ListCustomPrompts = %v, %v", names, err)
	}
	if _, err := app.DeleteCustomPrompt(ctx, "backup"); err != nil {
		t.Fatalf("DeleteCustomPrompt: %v", err)
	}
	if got := app.docs.Global.Sagittarius.SystemPrompt.Custom; got != "atlas" {
		t.Fatalf("unrelated selection changed to %q", got)
	}
}

func TestApplyRejectsUnknownCustomPrompt(t *testing.T) {
	app := customPromptTestApp(t, false)
	r, err := NewRunner(RunnerConfig{Generator: &fakeGenerator{}, Model: "test-model", WorkDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	app.runner = r
	h := &appHooks{app: app}

	for _, id := range []string{"no-such-prompt", "Bad/Name"} {
		if _, err := h.ApplyProjectSystemPromptPreset(context.Background(), id); err == nil {
			t.Fatalf("apply %q: expected error", id)
		}
	}
	if got := app.docs.Global.Sagittarius.SystemPrompt.Custom; got != "atlas" {
		t.Fatalf("a rejected apply must not write settings; custom = %q", got)
	}
}
