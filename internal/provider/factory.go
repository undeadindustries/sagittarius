package provider

import (
	"context"
	"errors"
	"fmt"

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

	endpoint, err := ResolveEndpointConfig(settings)
	if err != nil {
		return nil, fmt.Errorf("content generator: %w", err)
	}

	switch endpoint.WireFormat {
	case config.WireFormatGemini:
		return newGeminiGenerator(ctx, settings, endpoint)
	case config.WireFormatOpenAIChat:
		return newOpenAIChatGenerator(ctx, settings, endpoint)
	case config.WireFormatOpenAIResponses:
		return newOpenAIResponsesGenerator(ctx, settings, endpoint)
	default:
		return nil, fmt.Errorf("content generator: provider %q uses wire format %q, which is not supported yet",
			endpoint.ProviderID, endpoint.WireFormat)
	}
}

func newGeminiGenerator(ctx context.Context, settings *config.Settings, endpoint EndpointConfig) (ContentGenerator, error) {
	apiKey, err := resolveAPIKey(ctx, endpoint.ProviderID)
	if err != nil {
		if errors.Is(err, credentials.ErrAPIKeyMissing) {
			return nil, err
		}
		return nil, fmt.Errorf("resolve gemini api key: %w", err)
	}

	gen, err := NewGeminiGenerator(ctx, GeminiConfig{
		APIKey:  apiKey,
		Model:   endpoint.Model,
		Timeout: endpoint.Timeout,
	})
	if err != nil {
		return nil, fmt.Errorf("create gemini generator: %w", err)
	}
	_ = settings
	return gen, nil
}

func newOpenAIChatGenerator(ctx context.Context, settings *config.Settings, endpoint EndpointConfig) (ContentGenerator, error) {
	var bearer string
	if endpoint.RequiresAPIKey {
		apiKey, err := resolveAPIKey(ctx, endpoint.ProviderID)
		if err != nil {
			if errors.Is(err, credentials.ErrAPIKeyMissing) {
				return nil, err
			}
			return nil, fmt.Errorf("resolve openai api key: %w", err)
		}
		bearer = apiKey
	} else if key, err := resolveAPIKey(ctx, endpoint.ProviderID); err == nil && key != "" {
		// Optional Bearer for authenticated local endpoints.
		bearer = key
	}

	gen, err := NewOpenAIChatGenerator(OpenAIChatConfig{
		BaseURL:         endpoint.BaseURL,
		Model:           endpoint.Model,
		Timeout:         endpoint.Timeout,
		Bearer:          bearer,
		ToolCallParsing: endpoint.ToolCallParsing,
		Temperature:     endpoint.Temperature,
	})
	if err != nil {
		return nil, fmt.Errorf("create openai chat generator: %w", err)
	}
	_ = settings
	return gen, nil
}

func newOpenAIResponsesGenerator(ctx context.Context, settings *config.Settings, endpoint EndpointConfig) (ContentGenerator, error) {
	var bearer string
	if endpoint.RequiresAPIKey {
		apiKey, err := resolveAPIKey(ctx, endpoint.ProviderID)
		if err != nil {
			if errors.Is(err, credentials.ErrAPIKeyMissing) {
				return nil, err
			}
			return nil, fmt.Errorf("resolve openai responses api key: %w", err)
		}
		bearer = apiKey
	} else if key, err := resolveAPIKey(ctx, endpoint.ProviderID); err == nil && key != "" {
		bearer = key
	}

	gen, err := NewOpenAIResponsesGenerator(OpenAIResponsesConfig{
		BaseURL:              endpoint.BaseURL,
		Model:                endpoint.Model,
		Timeout:              endpoint.Timeout,
		Bearer:               bearer,
		ReasoningEffort:      endpoint.ReasoningEffort,
		UseResponseChaining:  endpoint.UseResponseChaining,
		Temperature:          endpoint.Temperature,
		SystemPromptOverride: endpoint.SystemPromptOverride,
		ToolsEnabled:         endpoint.ToolsEnabled,
	})
	if err != nil {
		return nil, fmt.Errorf("create openai responses generator: %w", err)
	}
	_ = settings
	return gen, nil
}
