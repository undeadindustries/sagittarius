package main

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/credentials"
)

// memStore is an in-memory credentials.Store so resolveWebFlags can be exercised
// against a "keychain" without touching the host's real one.
type memStore struct {
	mu     sync.Mutex
	values map[string]string
}

func (m *memStore) Get(_ context.Context, account string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.values[account], nil
}

func (m *memStore) Set(_ context.Context, account, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.values[account] = value
	return nil
}

func (m *memStore) Delete(_ context.Context, account string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.values, account)
	return nil
}

func (m *memStore) Available(context.Context) bool { return true }

// useStoredGeminiKey points credential resolution at an in-memory store and,
// when key is non-empty, pre-seeds a stored Gemini key. Env vars are cleared so
// only the store can satisfy the lookup.
func useStoredGeminiKey(t *testing.T, key string) {
	t.Helper()
	// LockTestGlobals must be registered first so LIFO cleanup keeps the lock
	// held across ResetForTesting (see internal/credentials/doc.go).
	t.Cleanup(credentials.LockTestGlobals())

	for _, env := range []string{"GEMINI_API_KEY", "GOOGLE_API_KEY"} {
		prev, had := os.LookupEnv(env)
		if err := os.Unsetenv(env); err != nil {
			t.Fatalf("Unsetenv(%q): %v", env, err)
		}
		if had {
			t.Cleanup(func() { _ = os.Setenv(env, prev) })
		}
	}

	store := &memStore{values: map[string]string{}}
	credentials.SetStoreFactoryForTesting(func(string) credentials.Store { return store })
	t.Cleanup(credentials.ResetForTesting)

	if key != "" {
		if err := credentials.SetProviderAPIKey(context.Background(),
			string(config.BuiltInGeminiAPIKey), key); err != nil {
			t.Fatalf("SetProviderAPIKey: %v", err)
		}
	}
}

// TestResolveWebFlagsDefaultsOnWithoutKey asserts web tools default on even without a Gemini key.
func TestResolveWebFlagsDefaultsOnWithoutKey(t *testing.T) {
	useStoredGeminiKey(t, "")

	searchEnabled, fetchEnabled := resolveWebFlags(context.Background(), &config.Settings{})
	if !searchEnabled {
		t.Error("search should default on even without a Gemini key")
	}
	if !fetchEnabled {
		t.Error("fetch should default on")
	}
}

// TestResolveWebFlagsExplicitSettingWins asserts an explicit searchEnabled is
// honored.
func TestResolveWebFlagsExplicitSettingWins(t *testing.T) {
	useStoredGeminiKey(t, "")
	off := false

	searchEnabled, _ := resolveWebFlags(context.Background(), &config.Settings{
		Sagittarius: &config.SagittariusSettings{
			Web: &config.SagittariusWebConfig{SearchEnabled: &off},
		},
	})
	if searchEnabled {
		t.Error("an explicit searchEnabled=false should be honored")
	}
}
