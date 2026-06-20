package provider

import (
	"fmt"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/config"
)

// IsOpenAIChatMode reports whether the active provider uses openai-chat wire format.
// Phase 11 context layers key off this hook (fork isLocalMode semantics).
func IsOpenAIChatMode(settings *config.Settings) bool {
	endpoint, err := ResolveEndpointConfig(settings)
	if err != nil {
		return false
	}
	return endpoint.WireFormat == config.WireFormatOpenAIChat
}

// SetActiveProvider updates providers.active to providerID.
func SetActiveProvider(settings *config.Settings, providerID string) error {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return fmt.Errorf("set active provider: id is required")
	}
	if settings == nil {
		return fmt.Errorf("set active provider: settings are required")
	}
	if settings.Providers == nil {
		settings.Providers = &config.ProvidersSettings{}
	}
	if _, ok := config.LookupBuiltInProvider(providerID); !ok {
		if settings.Providers.Custom == nil {
			return fmt.Errorf("set active provider: unknown provider %q", providerID)
		}
		if _, ok := settings.Providers.Custom[providerID]; !ok {
			return fmt.Errorf("set active provider: unknown provider %q", providerID)
		}
	}
	settings.Providers.Active = providerID
	return nil
}

// SaveActiveProvider sets the active provider and persists settings via loader.
func SaveActiveProvider(loader *config.Loader, settings *config.Settings, providerID string) error {
	if loader == nil {
		return fmt.Errorf("save active provider: loader is required")
	}
	if err := SetActiveProvider(settings, providerID); err != nil {
		return err
	}
	return loader.Save(settings)
}
