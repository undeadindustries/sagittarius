package provider

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/credentials"
)

const geminiProviderID = string(config.BuiltInGeminiAPIKey)

var resolveAPIKey = credentials.ResolveProviderAPIKey

// NewContentGenerator selects and constructs the active provider ContentGenerator.
func NewContentGenerator(ctx context.Context, settings *config.Settings) (ContentGenerator, error) {
	if settings == nil {
		return nil, fmt.Errorf("content generator: settings are required")
	}

	active := settings.ActiveProvider()
	if active == "" {
		return nil, fmt.Errorf("content generator: no active provider configured")
	}

	if active != geminiProviderID {
		return nil, fmt.Errorf("content generator: provider %q is not supported yet (Phase 06+)", active)
	}

	wireFormat, err := geminiWireFormat(settings)
	if err != nil {
		return nil, err
	}
	if wireFormat != config.WireFormatGemini {
		return nil, fmt.Errorf("content generator: provider %q uses wire format %q, expected %q",
			active, wireFormat, config.WireFormatGemini)
	}

	apiKey, err := resolveAPIKey(ctx, geminiProviderID)
	if err != nil {
		if errors.Is(err, credentials.ErrAPIKeyMissing) {
			return nil, err
		}
		return nil, fmt.Errorf("resolve gemini api key: %w", err)
	}

	model := resolveGeminiModel(settings)
	timeout := resolveGeminiTimeout(settings)

	gen, err := NewGeminiGenerator(ctx, GeminiConfig{
		APIKey:  apiKey,
		Model:   model,
		Timeout: timeout,
	})
	if err != nil {
		return nil, fmt.Errorf("create gemini generator: %w", err)
	}
	return gen, nil
}

func geminiWireFormat(settings *config.Settings) (config.WireFormat, error) {
	def, ok := config.LookupBuiltInProvider(geminiProviderID)
	if !ok {
		return "", fmt.Errorf("unknown built-in provider %q", geminiProviderID)
	}
	if settings.Providers != nil && settings.Providers.GeminiAPIKey != nil {
		if wf := settings.Providers.GeminiAPIKey.WireFormat; wf != "" {
			return wf, nil
		}
	}
	return def.WireFormat, nil
}

func resolveGeminiModel(settings *config.Settings) string {
	if settings.Providers != nil && settings.Providers.GeminiAPIKey != nil {
		if model := settings.Providers.GeminiAPIKey.Model; model != "" {
			return model
		}
	}
	if def, ok := config.LookupBuiltInProvider(geminiProviderID); ok {
		return def.DefaultModel
	}
	return "gemini-2.5-pro"
}

func resolveGeminiTimeout(settings *config.Settings) time.Duration {
	if settings.Providers != nil && settings.Providers.GeminiAPIKey != nil {
		if settings.Providers.GeminiAPIKey.Timeout != nil && *settings.Providers.GeminiAPIKey.Timeout > 0 {
			return time.Duration(*settings.Providers.GeminiAPIKey.Timeout) * time.Second
		}
	}
	return 0
}
