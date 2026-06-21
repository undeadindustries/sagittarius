package onboardingdialog

import (
	"context"

	"github.com/undeadindustries/sagittarius/internal/config"
)

// Deps performs the settings and credential side effects for first-run setup.
// Implementations live in the agent layer.
type Deps interface {
	PrepareGemini(ctx context.Context, apiKey string) (providerID string, err error)
	PrepareOpenRouter(ctx context.Context, apiKey string) (providerID string, err error)
	PrepareCustom(ctx context.Context, baseURL, apiKey string) (providerID string, err error)
	DiscoverModels(ctx context.Context, providerID string) ([]string, error)
	CompleteSetup(ctx context.Context, providerID, model string) error
}

// OpenRouter preset constants (matches internal/provider/openai_test.go).
const (
	OpenRouterProviderID = "openrouter"
	OpenRouterBaseURL    = "https://openrouter.ai/api/v1/chat/completions"
	OpenRouterEnvVar     = "OPENROUTER_API_KEY"
)

// OpenRouterDefinition returns the fork-compatible custom provider block.
func OpenRouterDefinition() config.CustomProviderDefinition {
	return config.CustomProviderDefinition{
		DisplayName:  "OpenRouter",
		BaseURL:        OpenRouterBaseURL,
		APIKeyEnvVar:   OpenRouterEnvVar,
		WireFormat:     config.WireFormatOpenAIChat,
	}
}
