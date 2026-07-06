package onboardingdialog

import (
	"context"
)

// Deps performs the settings and credential side effects for first-run setup.
// Implementations live in the agent layer.
type Deps interface {
	PrepareGemini(ctx context.Context, apiKey string) (providerID string, err error)
	// PreparePreset provisions a provider from a config.ProviderPreset id
	// (openrouter, anthropic, …), stores the key, and returns the resolved id.
	PreparePreset(ctx context.Context, presetID, apiKey string) (providerID string, err error)
	PrepareCustom(ctx context.Context, baseURL, apiKey string) (providerID string, err error)
	DiscoverModels(ctx context.Context, providerID string) ([]string, error)
	CompleteSetup(ctx context.Context, providerID, model string) error
}

// OpenRouterProviderID is retained for tests and legacy references; the preset
// table (config.ProviderPresets) is now the source of truth.
const OpenRouterProviderID = "openrouter"
