package agent

import (
	"context"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/tools"
)

func TestNewRuntimeRegistersEditToolDefault(t *testing.T) {
	// Tests that NewRuntime populates CatalogConfig correctly with default settings,
	// verifying that edit tool is registered when config.EditEnabled(settings) is true.
	ctx := context.Background()
	cfg := RuntimeConfig{
		WorkDir:     t.TempDir(),
		Settings:    &config.Settings{},
		EditEnabled: true, // As derived from config.EditEnabled(emptySettings, nil) in main.go
	}

	rt, err := NewRuntime(ctx, cfg)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	defer rt.Close()

	if _, ok := rt.Catalog.BuildRegistry().Lookup(tools.EditToolName); !ok {
		t.Errorf("%s should be registered at startup with default settings", tools.EditToolName)
	}
}
