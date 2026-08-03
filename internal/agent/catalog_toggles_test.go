package agent

import (
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/tools"
)

func newToggleCatalog(t *testing.T, cfg CatalogConfig) *Catalog {
	// Tests explicitly set what they care about, but default edit to true
	// because it defaults to true in settings.
	cfg.EditEnabled = true
	t.Helper()
	ws, err := tools.NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	cfg.Workspace = ws
	cat, err := NewCatalog(cfg)
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	t.Cleanup(func() { _ = cat.Close() })
	return cat
}

func settingsWithSymbolsEnabled(enabled bool) *config.Settings {
	return &config.Settings{
		Sagittarius: &config.SagittariusSettings{
			Symbols: &config.SagittariusSymbolsConfig{Enabled: &enabled},
		},
	}
}

// TestRefreshBuiltinTogglesAppliesSymbolsChange is the regression test for the
// /settings toggle that persisted and rebuilt the runner but left find_symbol
// registered until a full restart.
func TestRefreshBuiltinTogglesAppliesSymbolsChange(t *testing.T) {
	cat := newToggleCatalog(t, CatalogConfig{SymbolsEnabled: true, SymbolsPreferGopls: true})

	if _, ok := cat.BuildRegistry().Lookup(tools.FindSymbolToolName); !ok {
		t.Fatalf("%s should be registered at startup", tools.FindSymbolToolName)
	}

	if changed := cat.RefreshBuiltinToggles(settingsWithSymbolsEnabled(false)); !changed {
		t.Fatal("disabling symbols should report a change")
	}
	if _, ok := cat.BuildRegistry().Lookup(tools.FindSymbolToolName); ok {
		t.Errorf("%s should be gone after the toggle is disabled", tools.FindSymbolToolName)
	}

	if changed := cat.RefreshBuiltinToggles(settingsWithSymbolsEnabled(true)); !changed {
		t.Fatal("re-enabling symbols should report a change")
	}
	if _, ok := cat.BuildRegistry().Lookup(tools.FindSymbolToolName); !ok {
		t.Errorf("%s should return after the toggle is re-enabled", tools.FindSymbolToolName)
	}
}

func settingsWithEditEnabled(enabled bool) *config.Settings {
	return &config.Settings{
		Sagittarius: &config.SagittariusSettings{
			Edit: &config.SagittariusEditConfig{Enabled: &enabled},
		},
	}
}

func TestRefreshBuiltinTogglesEditTool(t *testing.T) {
	cat := newToggleCatalog(t, CatalogConfig{EditEnabled: true})

	if _, ok := cat.BuildRegistry().Lookup(tools.EditToolName); !ok {
		t.Fatalf("%s should be registered at startup", tools.EditToolName)
	}

	if changed := cat.RefreshBuiltinToggles(settingsWithEditEnabled(false)); !changed {
		t.Fatal("disabling edit should report a change")
	}
	if _, ok := cat.BuildRegistry().Lookup(tools.EditToolName); ok {
		t.Errorf("%s should be gone after the toggle is disabled", tools.EditToolName)
	}

	if changed := cat.RefreshBuiltinToggles(settingsWithEditEnabled(true)); !changed {
		t.Fatal("re-enabling edit should report a change")
	}
	if _, ok := cat.BuildRegistry().Lookup(tools.EditToolName); !ok {
		t.Errorf("%s should return after the toggle is re-enabled", tools.EditToolName)
	}
}

// TestRefreshBuiltinTogglesNoopWhenUnchanged keeps mode switches and model picks
// free of registry churn: RebuildRunner only reinstalls a registry on a change.
// Every toggle is set explicitly so the result cannot depend on ambient
// credentials or on a default changing underneath the test.
func TestRefreshBuiltinTogglesNoopWhenUnchanged(t *testing.T) {
	off, on := false, true
	cat := newToggleCatalog(t, CatalogConfig{SymbolsEnabled: true, SymbolsPreferGopls: true, EditEnabled: true, SubagentsEnabled: false})

	same := &config.Settings{
		Sagittarius: &config.SagittariusSettings{
			Symbols: &config.SagittariusSymbolsConfig{Enabled: &on, PreferGopls: &on},
			Web:     &config.SagittariusWebConfig{SearchEnabled: &off, FetchEnabled: &off},
		},
	}
	if changed := cat.RefreshBuiltinToggles(same); changed {
		t.Fatal("settings equal to the current state should report no change")
	}
	if changed := cat.RefreshBuiltinToggles(same); changed {
		t.Error("a second identical refresh should still report no change")
	}
}

// TestRefreshBuiltinTogglesSettingsWinOverStartupFlags documents the precedence:
// the CatalogConfig flags seed the initial state, after which the live settings
// document is authoritative. main.go derives those flags from the same resolvers,
// so the two agree at startup in production.
func TestRefreshBuiltinTogglesSettingsWinOverStartupFlags(t *testing.T) {
	enabled := true
	cat := newToggleCatalog(t, CatalogConfig{SymbolsEnabled: false})

	if _, ok := cat.BuildRegistry().Lookup(tools.FindSymbolToolName); ok {
		t.Fatalf("%s should be absent when constructed disabled", tools.FindSymbolToolName)
	}
	if changed := cat.RefreshBuiltinToggles(&config.Settings{
		Sagittarius: &config.SagittariusSettings{
			Symbols: &config.SagittariusSymbolsConfig{Enabled: &enabled},
		},
	}); !changed {
		t.Fatal("settings enabling symbols should override the startup flag")
	}
	if _, ok := cat.BuildRegistry().Lookup(tools.FindSymbolToolName); !ok {
		t.Errorf("%s should be present once settings enable it", tools.FindSymbolToolName)
	}
}

// TestRefreshBuiltinTogglesResolvesFetchBudget asserts sagittarius.web
// .maxFetchBytes reaches the registry rather than defaulting to a zero budget.
func TestRefreshBuiltinTogglesResolvesFetchBudget(t *testing.T) {
	// Web tools stay off so no Gemini utility client (and no credential lookup)
	// is attempted; the budget field is resolved either way.
	cat := newToggleCatalog(t, CatalogConfig{})

	if cat.webMaxFetchBytes != config.DefaultMaxFetchBytes {
		t.Errorf("startup budget = %d; want the default %d", cat.webMaxFetchBytes, config.DefaultMaxFetchBytes)
	}

	budget := 4096
	cat.RefreshBuiltinToggles(&config.Settings{
		Sagittarius: &config.SagittariusSettings{
			Web: &config.SagittariusWebConfig{MaxFetchBytes: &budget},
		},
	})
	if cat.webMaxFetchBytes != budget {
		t.Errorf("configured budget = %d; want %d", cat.webMaxFetchBytes, budget)
	}
}
