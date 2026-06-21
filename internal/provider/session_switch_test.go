package provider

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
)

// TestSaveActiveProviderClearsSessionState verifies that switching the active
// provider invalidates session state scoped to the previous backend: the
// Responses API chaining id and the session reasoning override. Not parallel:
// it mutates the process-wide session singleton.
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

	SetLastResponseID("resp_abc123")
	SetSessionReasoningOverride("high")
	t.Cleanup(func() {
		ClearLastResponseID()
		ClearSessionReasoningOverride()
	})

	if err := SaveActiveProvider(loader, settings, string(config.BuiltInOpenAI)); err != nil {
		t.Fatalf("SaveActiveProvider: %v", err)
	}

	if got := LastResponseID(); got != "" {
		t.Errorf("LastResponseID after switch = %q, want empty", got)
	}
	if got := SessionReasoningOverride(); got != "" {
		t.Errorf("SessionReasoningOverride after switch = %q, want empty", got)
	}
}
