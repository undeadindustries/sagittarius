package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/slash"
	"github.com/undeadindustries/sagittarius/internal/ui/mcpdialog"
)

const (
	scopeTestProvider = "GX10-01"
	scopeTestModelA   = "gemma-a"
	scopeTestModelB   = "gemma-b"
)

// scopeTestApp builds an App over real settings documents whose global tier
// defines one custom provider with two active models.
func scopeTestApp(t *testing.T) (*App, string) {
	t.Helper()
	t.Setenv("SAGITTARIUS_HOME", t.TempDir())
	projectDir := t.TempDir()
	docs, err := config.LoadDocuments(projectDir)
	if err != nil {
		t.Fatalf("LoadDocuments: %v", err)
	}
	docs.Global.Providers = &config.ProvidersSettings{
		Active: scopeTestProvider,
		Custom: map[string]config.CustomProviderDefinition{
			scopeTestProvider: {DisplayName: "GX10", BaseURL: "http://127.0.0.1:8000/v1"},
		},
		Extra: map[string]json.RawMessage{
			scopeTestProvider: json.RawMessage(`{"model":"` + scopeTestModelA + `","activeModels":["` + scopeTestModelA + `","` + scopeTestModelB + `"]}`),
		},
	}
	if err := docs.Save(config.ScopeGlobal); err != nil {
		t.Fatalf("save global: %v", err)
	}
	return &App{docs: docs, deps: slash.Deps{Settings: docs.Global}}, projectDir
}

func TestPersistScopeProjectWritesOnlyProjectFile(t *testing.T) {
	app, projectDir := scopeTestApp(t)
	if err := app.persistScope(config.ScopeProject, func(s *config.Settings) error {
		return provider.SetProviderModel(s, scopeTestProvider, scopeTestModelB)
	}); err != nil {
		t.Fatalf("persistScope(project): %v", err)
	}

	reloaded, err := config.LoadDocuments(projectDir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := provider.InstanceConfig(reloaded.Global, scopeTestProvider).Model; got != scopeTestModelA {
		t.Errorf("global model = %q, want %q untouched", got, scopeTestModelA)
	}
	if got := provider.InstanceConfig(reloaded.Project, scopeTestProvider); got == nil || got.Model != scopeTestModelB {
		t.Errorf("project instance = %+v, want model %q", got, scopeTestModelB)
	}
	if reloaded.Project.Providers != nil && len(reloaded.Project.Providers.Custom) > 0 {
		t.Errorf("custom definitions leaked into the project file: %+v", reloaded.Project.Providers.Custom)
	}
	ep, err := provider.ResolveEndpointForProvider(reloaded.Merged(), scopeTestProvider)
	if err != nil || ep.Model != scopeTestModelB {
		t.Errorf("merged endpoint = %+v, %v; want model %q", ep, err, scopeTestModelB)
	}
}

func TestPersistScopeProjectWithoutProjectTier(t *testing.T) {
	app := &App{}
	err := app.persistScope(config.ScopeProject, func(*config.Settings) error { return nil })
	if !errors.Is(err, config.ErrNoProjectScope) {
		t.Fatalf("err = %v, want ErrNoProjectScope", err)
	}
}

// TestCoerceLiveModelWritesOwningScope: when the project narrows a provider's
// active set, the stale model is fixed in the project file, never in the
// global default every other project shares.
func TestCoerceLiveModelWritesOwningScope(t *testing.T) {
	app, _ := scopeTestApp(t)
	if err := app.persistScope(config.ScopeProject, func(s *config.Settings) error {
		return provider.SetActiveModels(s, scopeTestProvider, []string{scopeTestModelB})
	}); err != nil {
		t.Fatalf("narrow project active set: %v", err)
	}

	if !app.coerceLiveModel() {
		t.Fatal("coerceLiveModel reported no change for a model outside the curated set")
	}
	if got := provider.InstanceConfig(app.docs.Global, scopeTestProvider).Model; got != scopeTestModelA {
		t.Errorf("global model = %q, want %q untouched", got, scopeTestModelA)
	}
	if got := provider.InstanceConfig(app.docs.Project, scopeTestProvider).Model; got != scopeTestModelB {
		t.Errorf("project model = %q, want %q", got, scopeTestModelB)
	}
	if app.coerceLiveModel() {
		t.Error("second coerceLiveModel changed settings again; want a noop")
	}
}

func TestHomeDirProjectWritesFailCleanlyWithoutDirtyingMemory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SAGITTARIUS_HOME", home)
	// Launching in home means <workDir>/.sagittarius/settings.json == global file,
	// so LoadDocuments disables the project tier (ProjectAvailable() == false).
	docs, err := config.LoadDocuments(home)
	if err != nil {
		t.Fatalf("LoadDocuments(home): %v", err)
	}
	if docs.ProjectAvailable() {
		t.Fatal("ProjectAvailable() = true in home directory, want false")
	}
	docs.Global.Providers = &config.ProvidersSettings{
		Active: scopeTestProvider,
		Custom: map[string]config.CustomProviderDefinition{
			scopeTestProvider: {DisplayName: "GX10", BaseURL: "http://127.0.0.1:8000/v1"},
		},
		Extra: map[string]json.RawMessage{
			scopeTestProvider: json.RawMessage(`{"model":"` + scopeTestModelA + `","activeModels":["` + scopeTestModelA + `","` + scopeTestModelB + `"]}`),
		},
	}
	if err := docs.Save(config.ScopeGlobal); err != nil {
		t.Fatalf("save global: %v", err)
	}

	app := &App{docs: docs, deps: slash.Deps{Settings: docs.Global, Documents: docs}}
	app.deps.Hooks = &appHooks{app: app}
	ctx := context.Background()

	ops := []struct {
		name string
		run  func() error
	}{
		{
			name: "model picker SelectCurrentModel",
			run: func() error {
				return app.ModelPickDialogDeps().SelectCurrentModel(ctx, scopeTestProvider, scopeTestModelB, config.ScopeProject)
			},
		},
		{
			name: "modes dialog SetModeOverride",
			run: func() error {
				return app.ModesDialogDeps().SetModeOverride(ctx, "plan", scopeTestProvider, scopeTestModelB, config.ScopeProject)
			},
		},
		{
			name: "modes dialog ClearModeOverride",
			run: func() error {
				return app.ModesDialogDeps().ClearModeOverride(ctx, "plan", config.ScopeProject)
			},
		},
		{
			name: "subagents dialog SetModeOverride",
			run: func() error {
				return app.SubagentsDialogDeps().SetModeOverride(ctx, "coding", scopeTestProvider, scopeTestModelB, config.ScopeProject)
			},
		},
		{
			name: "subagents dialog ClearModeOverride",
			run: func() error {
				return app.SubagentsDialogDeps().ClearModeOverride(ctx, "coding", config.ScopeProject)
			},
		},
		{
			name: "subagents reset",
			run: func() error {
				_, err := app.deps.Hooks.ResetSubagentOverrides(ctx, config.ScopeProject)
				return err
			},
		},
		{
			name: "settings SetValue",
			run: func() error {
				return app.SettingsDialogDeps().SetValue(ctx, config.ScopeProject, "sagittarius.maxToolRounds", "42")
			},
		},
		{
			name: "settings ClearValue",
			run: func() error {
				return app.SettingsDialogDeps().ClearValue(ctx, config.ScopeProject, "sagittarius.maxToolRounds")
			},
		},
		{
			name: "mcp SaveServer",
			run: func() error {
				return app.MCPDialogDeps().SaveServer(ctx, "", mcpdialog.ServerForm{
					Name:      "demo",
					Transport: mcpdialog.TransportStdio,
					Command:   "echo",
				}, config.ScopeProject)
			},
		},
	}

	for _, op := range ops {
		t.Run(op.name, func(t *testing.T) {
			err := op.run()
			if !errors.Is(err, config.ErrNoProjectScope) {
				t.Fatalf("err = %v, want ErrNoProjectScope", err)
			}
			if docs.Project != nil {
				t.Fatalf("docs.Project allocated after failed %s: %+v", op.name, docs.Project)
			}
		})
	}

	// Verify a subsequent global write does not merge any leaked project state.
	if err := app.persistGlobal(func(s *config.Settings) error {
		return s.SetUITheme("default")
	}); err != nil {
		t.Fatalf("persistGlobal: %v", err)
	}
	var mergedModes *config.SagittariusModes
	if m := docs.Merged(); m != nil && m.Sagittarius != nil {
		mergedModes = m.Sagittarius.Modes
	}
	if got := config.ModeConfig(mergedModes, "plan"); got != nil {
		t.Fatalf("leaked plan mode override in Merged(): %+v", got)
	}
}

func TestReadsAndGlobalRenamesDoNotAllocateProjectDocument(t *testing.T) {
	app, _ := scopeTestApp(t)
	if app.docs.Project != nil {
		t.Fatalf("expected initial docs.Project == nil, got %+v", app.docs.Project)
	}

	// Pure read of project settings in /settings must not allocate docs.Project.
	_ = app.SettingsDialogDeps().ListSettings(config.ScopeProject)
	if app.docs.Project != nil {
		t.Fatalf("ListSettings(ScopeProject) allocated docs.Project: %+v", app.docs.Project)
	}

	// Adding and renaming a global MCP server must not allocate docs.Project.
	ctx := context.Background()
	mcpDeps := app.MCPDialogDeps()
	if err := mcpDeps.SaveServer(ctx, "", mcpdialog.ServerForm{
		Name:      "srv-one",
		Transport: mcpdialog.TransportStdio,
		Command:   "echo",
	}, config.ScopeGlobal); err != nil {
		t.Fatalf("SaveServer(add global): %v", err)
	}
	if err := mcpDeps.SaveServer(ctx, "srv-one", mcpdialog.ServerForm{
		Name:      "srv-two",
		Transport: mcpdialog.TransportStdio,
		Command:   "echo",
	}, config.ScopeGlobal); err != nil {
		t.Fatalf("SaveServer(rename global): %v", err)
	}
	if app.docs.Project != nil {
		t.Fatalf("renaming global MCP server allocated docs.Project: %+v", app.docs.Project)
	}
}
