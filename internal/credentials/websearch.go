package credentials

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// searchServicePrefix namespaces web-search keys away from provider and MCP
// credentials, so a custom provider can never share a slot with a search key.
const searchServicePrefix = "sagittarius-search-"

// braveSearchAccount is both the service suffix and the account name for the
// Brave Search key, mirroring the provider layout (one account per service).
const braveSearchAccount = "brave"

// EnvBraveAPIKey is the environment variable that supplies a Brave Search key.
// It takes precedence over a stored key, matching provider-key resolution.
const EnvBraveAPIKey = "BRAVE_API_KEY"

// SearchServiceName returns the OS keychain service for a web-search key.
func SearchServiceName(name string) string { return searchServicePrefix + name }

// BraveKeyStatus reports where a Brave Search key would come from without
// exposing the secret. Both fields can be true; EnvSet wins at resolution.
type BraveKeyStatus struct {
	EnvSet bool
	Stored bool
}

// ResolveBraveAPIKey returns the Brave Search API key, or "" when none is
// configured. The environment variable wins over secure storage. A storage
// failure is returned so the caller can report it rather than silently
// degrading to the key-free search backend.
func ResolveBraveAPIKey(ctx context.Context) (string, error) {
	if key := braveKeyFromEnv(); key != "" {
		return key, nil
	}
	key, err := newSearchStore(braveSearchAccount).Get(ctx, braveSearchAccount)
	if err != nil {
		return "", fmt.Errorf("resolve brave search key: %w", err)
	}
	return strings.TrimSpace(key), nil
}

// SetBraveAPIKey stores the Brave Search API key in the OS keychain, falling
// back to the encrypted file store. It is never written to settings.json.
func SetBraveAPIKey(ctx context.Context, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("brave search key is empty")
	}
	if err := newSearchStore(braveSearchAccount).Set(ctx, braveSearchAccount, key); err != nil {
		return fmt.Errorf("store brave search key: %w", err)
	}
	return nil
}

// DeleteBraveAPIKey removes the stored Brave Search API key. Deleting a key
// that was never stored succeeds; it cannot clear EnvBraveAPIKey.
func DeleteBraveAPIKey(ctx context.Context) error {
	if err := newSearchStore(braveSearchAccount).Delete(ctx, braveSearchAccount); err != nil {
		return fmt.Errorf("delete brave search key: %w", err)
	}
	return nil
}

// BraveAPIKeyStatus probes for a configured Brave Search key. It reads the
// keychain, which can block for seconds on a locked or absent keyring, so
// callers on a UI thread must run it off that thread.
func BraveAPIKeyStatus(ctx context.Context) (BraveKeyStatus, error) {
	status := BraveKeyStatus{EnvSet: braveKeyFromEnv() != ""}
	key, err := newSearchStore(braveSearchAccount).Get(ctx, braveSearchAccount)
	if err != nil {
		return status, fmt.Errorf("read brave search key: %w", err)
	}
	status.Stored = strings.TrimSpace(key) != ""
	return status, nil
}

func newSearchStore(name string) Store {
	return &hybridStore{service: SearchServiceName(name)}
}

func braveKeyFromEnv() string {
	return strings.TrimSpace(os.Getenv(EnvBraveAPIKey))
}
