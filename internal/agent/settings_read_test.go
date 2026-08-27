package agent

import (
	"context"
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

type mockRunnerRebuildHooks struct {
	slash.Hooks
	rebuildCalled bool
}

func (m *mockRunnerRebuildHooks) RebuildRunner(ctx context.Context) (string, string, error) {
	m.rebuildCalled = true
	return "custom-label", "custom-model", nil
}

func TestSelectCurrentModelProjectScopeGlobalCustomProvider(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SAGITTARIUS_HOME", home)
	projectDir := t.TempDir()

	docs, err := config.LoadDocuments(projectDir)
	if err != nil {
		t.Fatalf("LoadDocuments: %v", err)
	}

	// Global settings define custom provider GX10-01 with active models.
	const customProvider = "GX10-01"
	const customModel = "gemma-4-31b-it-low"
	docs.Global.Providers = &config.ProvidersSettings{
		Active: "gemini-apikey",
		Custom: map[string]config.CustomProviderDefinition{
			customProvider: {
				DisplayName: "GX10 Local Node",
				BaseURL:     "http://127.0.0.1:8000/v1/chat/completions",
			},
		},
		Extra: map[string]json.RawMessage{
			customProvider: json.RawMessage(`{"activeModels":["gemma-4-31b-it-low","gemma-4-31b-it-high"]}`),
		},
	}
	// Project settings only have a system prompt (no custom provider definitions).
	project := docs.TargetSettings(config.ScopeProject)
	project.Sagittarius = &config.SagittariusSettings{
		SystemPrompt: &config.SagittariusSystemPromptConfig{
			Personality: "programmer",
		},
	}
	docs.ReloadMerged()

	mockHooks := &mockRunnerRebuildHooks{}
	app := &App{
		docs: docs,
		deps: slash.Deps{
			Settings: docs.Global,
			Hooks:    mockHooks,
		},
	}

	d := app.ModelPickDialogDeps()
	// Selecting model with ScopeProject should succeed even though GX10-01 definition is global-only.
	if err := d.SelectCurrentModel(t.Context(), customProvider, customModel, config.ScopeProject); err != nil {
		t.Fatalf("SelectCurrentModel with ScopeProject failed: %v", err)
	}

	if !mockHooks.rebuildCalled {
		t.Error("expected RebuildRunner to be called")
	}

	// Verify project settings file on disk has only active provider & model override, no Custom definition copy.
	reloadedDocs, err := config.LoadDocuments(projectDir)
	if err != nil {
		t.Fatalf("reloaded LoadDocuments: %v", err)
	}
	if reloadedDocs.Project.ActiveProvider() != customProvider {
		t.Errorf("project active provider = %q, want %q", reloadedDocs.Project.ActiveProvider(), customProvider)
	}
	if reloadedDocs.Project.Providers != nil && len(reloadedDocs.Project.Providers.Custom) > 0 {
		t.Errorf("project providers.custom should remain empty, got %+v", reloadedDocs.Project.Providers.Custom)
	}

	// Merged settings must reflect active provider = GX10-01 and model = gemma-4-31b-it-low
	merged := reloadedDocs.Merged()
	if merged.ActiveProvider() != customProvider {
		t.Errorf("merged active provider = %q, want %q", merged.ActiveProvider(), customProvider)
	}

	// Validate uncurated model fails
	if err := d.SelectCurrentModel(t.Context(), customProvider, "non-existent-model", config.ScopeProject); err == nil {
		t.Error("expected error for model not in active set, got nil")
	}

	// Validate unknown provider fails
	if err := d.SelectCurrentModel(t.Context(), "completely-unknown", "model-x", config.ScopeProject); err == nil {
		t.Error("expected error for unknown provider, got nil")
	}
}
