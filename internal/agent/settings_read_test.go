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
