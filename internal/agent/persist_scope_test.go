package agent

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/slash"
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
		t.Error("second coerceLiveModel changed settings again; want a no-op")
	}
}
