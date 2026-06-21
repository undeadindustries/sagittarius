package agent

import (
	"context"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/credentials"
)

func TestNeedsProviderSetup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	if !NeedsProviderSetup(ctx, nil) {
		t.Fatal("nil settings should need setup")
	}

	empty := &config.Settings{}
	if !NeedsProviderSetup(ctx, empty) {
		t.Fatal("empty active provider should need setup")
	}

	withProvider := &config.Settings{
		Providers: &config.ProvidersSettings{Active: string(config.BuiltInGeminiAPIKey)},
	}
	withEmptyCredentials(t, func(t *testing.T) {
		if !NeedsProviderSetup(ctx, withProvider) {
			t.Fatal("missing API key should need setup")
		}
	})

	withKey := &config.Settings{
		Providers: &config.ProvidersSettings{Active: string(config.BuiltInGeminiAPIKey)},
	}
	credentials.SetStoreFactoryForTesting(func(string) credentials.Store {
		return stubCredentialStore{values: map[string]string{
			string(config.BuiltInGeminiAPIKey): "test-key",
		}}
	})
	t.Cleanup(credentials.ResetForTesting)
	if NeedsProviderSetup(ctx, withKey) {
		t.Fatal("configured provider with key should not need setup")
	}
}

func withEmptyCredentials(t *testing.T, fn func(t *testing.T)) {
	t.Helper()
	credentials.SetStoreFactoryForTesting(func(string) credentials.Store {
		return stubCredentialStore{}
	})
	t.Cleanup(credentials.ResetForTesting)
	fn(t)
}

type stubCredentialStore struct {
	values map[string]string
}

func (s stubCredentialStore) Get(_ context.Context, service string) (string, error) {
	if s.values == nil {
		return "", nil
	}
	return s.values[service], nil
}

func (s stubCredentialStore) Set(context.Context, string, string) error { return nil }
func (s stubCredentialStore) Delete(context.Context, string) error      { return nil }
func (s stubCredentialStore) Available(context.Context) bool            { return true }
