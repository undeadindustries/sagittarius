package provider

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/undeadindustries/sagittarius/internal/config"
)

// generatorCacheKey captures all material connection parameters that uniquely
// identify a ContentGenerator. Model and Temperature are excluded because they
// are passed per-request and do not affect client construction. Including the
// resolved bearer token means any credential change produces a cache miss
// automatically, without requiring explicit invalidation.
type generatorCacheKey struct {
	providerID           string
	wireFormat           config.WireFormat
	baseURL              string
	bearer               string
	timeout              time.Duration
	toolCallParsing      config.ToolCallParsingMode
	reasoningEffort      string
	useResponseChaining  bool
	systemPromptOverride string
	toolsEnabled         bool
}

// GeneratorCache is a session-scoped cache of ContentGenerator instances.
// Its primary purpose is eliminating the repeated client initialisation cost
// (DNS lookup + TLS handshake + genai.NewClient) incurred every time
// RebuildRunner is called — most visibly during /mode switches that involve a
// provider override. The cache is self-invalidating: any change to connection
// parameters (URL, credential, timeout, etc.) produces a cache miss because all
// material parameters are encoded into the key.
type GeneratorCache struct {
	mu      sync.Mutex
	entries map[generatorCacheKey]ContentGenerator
}

// NewGeneratorCache returns an empty, ready-to-use cache.
func NewGeneratorCache() *GeneratorCache {
	return &GeneratorCache{entries: make(map[generatorCacheKey]ContentGenerator)}
}

// GetOrCreate returns a ContentGenerator for the settings' active provider,
// building and caching one on first use. Subsequent calls with identical
// connection parameters (including the resolved credential) return the cached
// instance with no network round-trip.
//
// The resolved credential is incorporated into the cache key: if the user
// updates an API key via /providers, the key changes and the next call
// constructs a fresh generator automatically.
func (c *GeneratorCache) GetOrCreate(ctx context.Context, settings *config.Settings) (ContentGenerator, error) {
	if settings == nil {
		return nil, fmt.Errorf("content generator: settings are required")
	}

	endpoint, err := ResolveEndpointConfig(settings)
	if err != nil {
		return nil, fmt.Errorf("content generator: %w", err)
	}

	// Resolve the credential now (fast local keychain/env lookup) so it can be
	// incorporated into the key. Errors are intentionally ignored here: if the
	// key is missing or invalid, NewContentGenerator will fail and surface the
	// error with proper context — we don't want to suppress that.
	var bearer string
	if key, kErr := resolveAPIKey(ctx, endpoint.ProviderID); kErr == nil {
		bearer = key
	}

	key := generatorCacheKey{
		providerID:           endpoint.ProviderID,
		wireFormat:           endpoint.WireFormat,
		baseURL:              endpoint.BaseURL,
		bearer:               bearer,
		timeout:              endpoint.Timeout,
		toolCallParsing:      endpoint.ToolCallParsing,
		reasoningEffort:      endpoint.ReasoningEffort,
		useResponseChaining:  endpoint.UseResponseChaining,
		systemPromptOverride: endpoint.SystemPromptOverride,
		toolsEnabled:         endpoint.ToolsEnabled,
	}

	c.mu.Lock()
	gen, ok := c.entries[key]
	c.mu.Unlock()
	if ok {
		return gen, nil
	}

	// Cache miss: construct the generator (potentially slow for Gemini due to
	// genai.NewClient initialisation) and store it.
	gen, err = NewContentGenerator(ctx, settings)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.entries[key] = gen
	c.mu.Unlock()
	return gen, nil
}

// InvalidateAll discards every cached entry, forcing fresh construction on the
// next call. Use after bulk settings resets or test teardown.
func (c *GeneratorCache) InvalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.entries)
}
