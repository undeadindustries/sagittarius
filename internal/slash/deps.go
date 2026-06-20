package slash

import (
	"context"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/provider"
)

// Hooks performs runner and credential side effects for slash commands.
type Hooks interface {
	RebuildRunner(ctx context.Context) (providerLabel, model string, err error)
	ReloadSystemInstruction(ctx context.Context) error
	DiscoverModels(ctx context.Context) []provider.ModelInfo
	SetProviderAPIKey(ctx context.Context, providerID, apiKey string) error
}

// Deps supplies slash command dependencies (injectable for tests).
type Deps struct {
	Loader   *config.Loader
	Settings *config.Settings
	Hooks    Hooks
}
