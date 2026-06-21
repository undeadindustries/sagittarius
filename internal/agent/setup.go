package agent

import (
	"context"
	"errors"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/credentials"
	"github.com/undeadindustries/sagittarius/internal/provider"
)

// NeedsProviderSetup reports whether interactive onboarding should run before the
// first chat turn: no active provider, or the active provider requires an API
// key that is not configured in the environment or secure store.
func NeedsProviderSetup(ctx context.Context, settings *config.Settings) bool {
	if settings == nil || settings.ActiveProvider() == "" {
		return true
	}
	_, err := provider.NewContentGenerator(ctx, settings)
	return errors.Is(err, credentials.ErrAPIKeyMissing)
}

// PlaceholderModel returns a stand-in model id used while onboarding is pending.
func PlaceholderModel() string {
	return config.BuiltInProviders[config.BuiltInGeminiAPIKey].DefaultModel
}
