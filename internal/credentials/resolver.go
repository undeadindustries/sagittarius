package credentials

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

var (
	storeFactoryMu sync.RWMutex
	storeFactory   = func(providerID string) Store { return newProviderStore(providerID) }
)

// SetStoreFactoryForTesting replaces the per-provider store factory.
func SetStoreFactoryForTesting(factory func(string) Store) {
	storeFactoryMu.Lock()
	defer storeFactoryMu.Unlock()
	storeFactory = factory
}

func providerStore(providerID string) Store {
	storeFactoryMu.RLock()
	fn := storeFactory
	storeFactoryMu.RUnlock()
	return fn(providerID)
}

// ResolveProviderAPIKey returns the API key for providerID.
// Resolution order: environment variable, secure storage, error.
func ResolveProviderAPIKey(ctx context.Context, providerID string) (string, error) {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return "", fmt.Errorf("resolve api key: provider id is required")
	}

	if key, ok := apiKeyFromEnv(providerID); ok {
		return key, nil
	}

	key, err := loadStoredAPIKey(ctx, providerID)
	if err != nil {
		slog.Debug("stored api key lookup failed",
			"provider", providerID,
			"error", err.Error(),
		)
	}
	if key != "" {
		return key, nil
	}

	return "", missingAPIKeyError(providerID)
}

func loadStoredAPIKey(ctx context.Context, providerID string) (string, error) {
	if cached, ok := apiKeyCache.get(providerID); ok {
		return cached, nil
	}

	store := providerStore(providerID)
	raw, err := store.Get(ctx, providerID)
	if err != nil {
		return "", err
	}
	if raw == "" {
		return "", nil
	}
	key := decodeStoredAPIKey(raw)
	apiKeyCache.set(providerID, key)
	return key, nil
}

// SetProviderAPIKey persists apiKey for providerID in secure storage.
func SetProviderAPIKey(ctx context.Context, providerID, apiKey string) error {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return fmt.Errorf("set api key: provider id is required")
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return DeleteProviderAPIKey(ctx, providerID)
	}

	apiKeyCache.delete(providerID)
	stored := encodeStoredAPIKey(providerID, apiKey)
	if err := providerStore(providerID).Set(ctx, providerID, stored); err != nil {
		return saveAPIKeyError(providerID, err)
	}
	apiKeyCache.set(providerID, apiKey)
	return nil
}

// DeleteProviderAPIKey removes the stored API key for providerID.
func DeleteProviderAPIKey(ctx context.Context, providerID string) error {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return nil
	}
	apiKeyCache.delete(providerID)
	return providerStore(providerID).Delete(ctx, providerID)
}

// Redact returns a safe placeholder for logs when value must not appear.
func Redact(value string) string {
	if value == "" {
		return "<empty>"
	}
	return "<redacted>"
}
