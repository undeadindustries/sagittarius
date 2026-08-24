package agent

import (
	"encoding/json"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/slash"
)

func hasModel(pairs []provider.ProviderModelPair, model string) bool {
	for _, p := range pairs {
		if p.Model == model {
			return true
		}
	}
	return false
}

// TestAllActiveModelsUsesMergedView asserts that a project-scoped activeModels
// list is visible through appHooks.AllActiveModels(). Before plan 05's Problem B
// fix the hook read deps.Settings (= Global) directly, so project-scoped picks
// were invisible in the /model picker and autocomplete.
func TestAllActiveModelsUsesMergedView(t *testing.T) {
	t.Parallel()

	global := &config.Settings{
		Providers: &config.ProvidersSettings{
			GeminiAPIKey: &config.ProviderInstanceConfig{},
		},
		Raw: map[string]json.RawMessage{},
	}
	const projectModel = "gemini-project-only-model"
	project := &config.Settings{
		Providers: &config.ProvidersSettings{
			GeminiAPIKey: &config.ProviderInstanceConfig{
				ActiveModels: []string{projectModel},
			},
		},
		Raw: map[string]json.RawMessage{},
	}
	docs := &config.Documents{Global: global, Project: project}
	docs.ReloadMerged()

	// Guard: the Global-only view must NOT contain the project model, otherwise
	// the test could false-pass via a default-model fallback.
	if hasModel(provider.AllActiveModels(global), projectModel) {
		t.Fatal("precondition: global view unexpectedly contains the project model")
	}

	app := &App{docs: docs, deps: slash.Deps{Settings: global}}
	h := &appHooks{app: app}

	if !hasModel(h.AllActiveModels(), projectModel) {
		t.Fatal("AllActiveModels did not surface the project-scoped activeModels entry")
	}
}

func TestModelsDialogListUsesMergedView(t *testing.T) {
	t.Parallel()

	global := &config.Settings{
		Providers: &config.ProvidersSettings{
			GeminiAPIKey: &config.ProviderInstanceConfig{},
		},
		Raw: map[string]json.RawMessage{},
	}
	const projectModel = "gemini-project-only-model"
	project := &config.Settings{
		Providers: &config.ProvidersSettings{
			GeminiAPIKey: &config.ProviderInstanceConfig{
				ActiveModels: []string{projectModel},
			},
		},
		Raw: map[string]json.RawMessage{},
	}
	docs := &config.Documents{Global: global, Project: project}
	docs.ReloadMerged()

	app := &App{docs: docs, deps: slash.Deps{Settings: global}}
	d := &modelsDialogDeps{app: app}
	found := false
	for _, e := range d.ListAllActiveModels() {
		if e.Model == projectModel {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("ListAllActiveModels did not surface the project-scoped activeModels entry")
	}
}

func TestSetActiveModelsRefreshesMerged(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SAGITTARIUS_HOME", home)

	docs, err := config.LoadDocuments(t.TempDir())
	if err != nil {
		t.Fatalf("LoadDocuments: %v", err)
	}
	if docs.Global.Providers == nil {
		docs.Global.Providers = &config.ProvidersSettings{}
	}
	if docs.Global.Providers.GeminiAPIKey == nil {
		docs.Global.Providers.GeminiAPIKey = &config.ProviderInstanceConfig{}
	}
	docs.ReloadMerged()

	const model = "gemini-activation-only"
	if hasModel(provider.AllActiveModels(docs.Merged()), model) {
		t.Fatal("precondition: merged view already contains the test model")
	}

	app := &App{docs: docs, deps: slash.Deps{Settings: docs.Global}}
	d := &providerDialogDeps{app: app}
	if err := d.SetActiveModels(t.Context(), "gemini-apikey", []string{model}); err != nil {
		t.Fatalf("SetActiveModels: %v", err)
	}
	if !hasModel(provider.AllActiveModels(app.effectiveSettings()), model) {
		t.Fatal("effectiveSettings/Merged did not pick up the newly activated model")
	}
}
