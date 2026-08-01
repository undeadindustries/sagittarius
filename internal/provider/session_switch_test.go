package provider

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
)

// TestSaveActiveProviderSwitchesAndPersists verifies SaveActiveProvider both
// updates providers.active in memory and persists it to disk. The former
// session-only reasoning override assertion moved to
// internal/agent.TestRunnerReasoningOverrideSelfInvalidates: the override is
// now Runner-owned and self-invalidates by (provider, model) comparison
// rather than needing an explicit clear on every provider-switch call site
// (see AD-077).
func TestSaveActiveProviderSwitchesAndPersists(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	loader, err := config.NewLoader(config.WithSettingsPath(path))
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}
	settings, err := loader.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if err := SaveActiveProvider(loader, settings, string(config.BuiltInOpenAI)); err != nil {
		t.Fatalf("SaveActiveProvider: %v", err)
	}
	if got := settings.ActiveProvider(); got != string(config.BuiltInOpenAI) {
		t.Fatalf("ActiveProvider() = %q, want %q", got, config.BuiltInOpenAI)
	}

	reloaded, err := loader.Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := reloaded.ActiveProvider(); got != string(config.BuiltInOpenAI) {
		t.Fatalf("persisted ActiveProvider() = %q, want %q", got, config.BuiltInOpenAI)
	}
}
