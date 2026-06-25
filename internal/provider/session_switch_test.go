package provider

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
)

// TestSaveActiveProviderClearsSessionState verifies that switching the active
// provider invalidates the session reasoning override scoped to the previous
// backend. The Responses API chaining id is now per-generator (invalidated by
// building a fresh generator on switch), so it is no longer a global concern
// here. Not parallel: it mutates the process-wide session singleton.
func TestSaveActiveProviderClearsSessionState(t *testing.T) {
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

	SetSessionReasoningOverride("high")
	t.Cleanup(ClearSessionReasoningOverride)

	if err := SaveActiveProvider(loader, settings, string(config.BuiltInOpenAI)); err != nil {
		t.Fatalf("SaveActiveProvider: %v", err)
	}

	if got := SessionReasoningOverride(); got != "" {
		t.Errorf("SessionReasoningOverride after switch = %q, want empty", got)
	}
}
