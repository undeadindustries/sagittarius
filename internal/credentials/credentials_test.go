package credentials

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/undeadindustries/sagittarius/internal/config"
)

type memoryStore struct {
	mu      sync.Mutex
	service string
	values  map[string]string
	avail   bool
}

func newMemoryStore(service string) *memoryStore {
	return &memoryStore{
		service: service,
		values:  map[string]string{},
		avail:   true,
	}
}

func (m *memoryStore) Get(_ context.Context, account string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.values[account], nil
}

func (m *memoryStore) Set(_ context.Context, account, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.values[account] = value
	return nil
}

func (m *memoryStore) Delete(_ context.Context, account string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.values, account)
	return nil
}

func (m *memoryStore) Available(context.Context) bool {
	return m.avail
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func withEnv(t *testing.T, key, value string) {
	t.Helper()
	prev, had := os.LookupEnv(key)
	if err := os.Setenv(key, value); err != nil {
		t.Fatalf("Setenv(%q): %v", key, err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, prev)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

func withoutEnv(t *testing.T, key string) {
	t.Helper()
	prev, had := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("Unsetenv(%q): %v", key, err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, prev)
		}
	})
}

func useMemoryStores(t *testing.T) map[string]*memoryStore {
	t.Helper()
	stores := map[string]*memoryStore{}
	SetStoreFactoryForTesting(func(providerID string) Store {
		svc := ProviderServiceName(providerID)
		if _, ok := stores[svc]; !ok {
			stores[svc] = newMemoryStore(svc)
		}
		return stores[svc]
	})
	t.Cleanup(ResetForTesting)
	return stores
}

func TestEnvOverridesKeychain(t *testing.T) {
	ctx := testContext(t)
	stores := useMemoryStores(t)

	const providerID = "openai"
	const envKey = "env-wins-key"
	withEnv(t, "OPENAI_API_KEY", envKey)

	if err := SetProviderAPIKey(ctx, providerID, "stored-key"); err != nil {
		t.Fatalf("SetProviderAPIKey: %v", err)
	}

	got, err := ResolveProviderAPIKey(ctx, providerID)
	if err != nil {
		t.Fatalf("ResolveProviderAPIKey: %v", err)
	}
	if got != envKey {
		t.Errorf("got %q, want env key %q", got, envKey)
	}
	if stores[ProviderServiceName(providerID)].values[providerID] == "" {
		t.Fatal("expected stored key in memory store")
	}
}

func TestKeychainRoundTrip(t *testing.T) {
	ctx := testContext(t)
	useMemoryStores(t)

	withoutEnv(t, "OPENAI_API_KEY")

	const providerID = "openai"
	const apiKey = "sk-test-roundtrip-key-12345"

	if err := SetProviderAPIKey(ctx, providerID, apiKey); err != nil {
		t.Fatalf("SetProviderAPIKey: %v", err)
	}

	got, err := ResolveProviderAPIKey(ctx, providerID)
	if err != nil {
		t.Fatalf("ResolveProviderAPIKey: %v", err)
	}
	if got != apiKey {
		t.Errorf("got %q, want %q", got, apiKey)
	}

	if err := DeleteProviderAPIKey(ctx, providerID); err != nil {
		t.Fatalf("DeleteProviderAPIKey: %v", err)
	}

	_, err = ResolveProviderAPIKey(ctx, providerID)
	if !errors.Is(err, ErrAPIKeyMissing) {
		t.Fatalf("expected ErrAPIKeyMissing, got %v", err)
	}
}

func TestFileFallbackWhenNoKeyring(t *testing.T) {
	ctx := testContext(t)

	dir := t.TempDir()
	credPath := filepath.Join(dir, fileCredentialsName)
	SetCredentialsPathForTesting(credPath)
	t.Cleanup(ResetForTesting)

	SetActiveBackendForTesting(func(ctx context.Context, service string) Store {
		store, err := sharedEncryptedFileStore(service)
		if err != nil {
			t.Fatalf("sharedEncryptedFileStore: %v", err)
		}
		store.service = service
		return store
	})

	withoutEnv(t, "OPENAI_API_KEY")

	const providerID = "openai"
	const apiKey = "sk-file-fallback-key-abcdef"

	if err := SetProviderAPIKey(ctx, providerID, apiKey); err != nil {
		t.Fatalf("SetProviderAPIKey: %v", err)
	}
	if _, err := os.Stat(credPath); err != nil {
		t.Fatalf("credentials file not created: %v", err)
	}

	ResetForTesting()
	SetCredentialsPathForTesting(credPath)
	SetActiveBackendForTesting(func(ctx context.Context, service string) Store {
		store, err := sharedEncryptedFileStore(service)
		if err != nil {
			t.Fatalf("sharedEncryptedFileStore: %v", err)
		}
		store.service = service
		return store
	})

	got, err := ResolveProviderAPIKey(ctx, providerID)
	if err != nil {
		t.Fatalf("ResolveProviderAPIKey after reload: %v", err)
	}
	if got != apiKey {
		t.Errorf("got %q, want %q", got, apiKey)
	}
}

func TestCacheTTL(t *testing.T) {
	ctx := testContext(t)
	stores := useMemoryStores(t)
	SetCacheTTLForTesting(20 * time.Millisecond)

	withoutEnv(t, "OPENAI_API_KEY")

	const providerID = "openai"
	if err := SetProviderAPIKey(ctx, providerID, "first-key"); err != nil {
		t.Fatalf("SetProviderAPIKey: %v", err)
	}
	got, err := ResolveProviderAPIKey(ctx, providerID)
	if err != nil || got != "first-key" {
		t.Fatalf("first resolve: got %q err %v", got, err)
	}

	svc := ProviderServiceName(providerID)
	if err := stores[svc].Set(ctx, providerID, encodeStoredAPIKey(providerID, "second-key")); err != nil {
		t.Fatalf("update stored key: %v", err)
	}

	got, err = ResolveProviderAPIKey(ctx, providerID)
	if err != nil || got != "first-key" {
		t.Fatalf("cached resolve: got %q err %v", got, err)
	}

	time.Sleep(25 * time.Millisecond)

	got, err = ResolveProviderAPIKey(ctx, providerID)
	if err != nil || got != "second-key" {
		t.Fatalf("after ttl: got %q err %v", got, err)
	}
}

func TestGeminiAPIKeyResolution(t *testing.T) {
	tests := []struct {
		name       string
		setupEnv   func(t *testing.T)
		setupStore func(t *testing.T, ctx context.Context)
		want       string
		wantErr    bool
	}{
		{
			name: "GEMINI_API_KEY wins",
			setupEnv: func(t *testing.T) {
				withEnv(t, "GEMINI_API_KEY", "gemini-env-key")
				withoutEnv(t, "GOOGLE_API_KEY")
			},
			want: "gemini-env-key",
		},
		{
			name: "GOOGLE_API_KEY when GEMINI unset",
			setupEnv: func(t *testing.T) {
				withoutEnv(t, "GEMINI_API_KEY")
				withEnv(t, "GOOGLE_API_KEY", "google-env-key")
			},
			want: "google-env-key",
		},
		{
			name: "GEMINI_API_KEY wins over GOOGLE",
			setupEnv: func(t *testing.T) {
				withEnv(t, "GEMINI_API_KEY", "gemini-first")
				withEnv(t, "GOOGLE_API_KEY", "google-second")
			},
			want: "gemini-first",
		},
		{
			name: "keychain when env empty",
			setupEnv: func(t *testing.T) {
				withoutEnv(t, "GEMINI_API_KEY")
				withoutEnv(t, "GOOGLE_API_KEY")
			},
			setupStore: func(t *testing.T, ctx context.Context) {
				if err := SetProviderAPIKey(ctx, string(config.BuiltInGeminiAPIKey), "stored-gemini-key"); err != nil {
					t.Fatalf("SetProviderAPIKey: %v", err)
				}
			},
			want: "stored-gemini-key",
		},
		{
			name: "missing key error",
			setupEnv: func(t *testing.T) {
				withoutEnv(t, "GEMINI_API_KEY")
				withoutEnv(t, "GOOGLE_API_KEY")
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ResetForTesting()
			useMemoryStores(t)
			ctx := testContext(t)
			if tc.setupEnv != nil {
				tc.setupEnv(t)
			}
			if tc.setupStore != nil {
				tc.setupStore(t, ctx)
			}

			got, err := ResolveProviderAPIKey(ctx, string(config.BuiltInGeminiAPIKey))
			if tc.wantErr {
				if !errors.Is(err, ErrAPIKeyMissing) {
					t.Fatalf("expected ErrAPIKeyMissing, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveProviderAPIKey: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDecodeStoredAPIKeyForkFormat(t *testing.T) {
	t.Parallel()
	raw := encodeStoredAPIKey("gemini-apikey", "fork-compat-key")
	if got := decodeStoredAPIKey(raw); got != "fork-compat-key" {
		t.Errorf("decodeStoredAPIKey = %q, want fork-compat-key", got)
	}
}

func TestRedact(t *testing.T) {
	t.Parallel()
	if Redact("secret") != "<redacted>" {
		t.Fatal("expected redacted placeholder")
	}
	if Redact("") != "<empty>" {
		t.Fatal("expected empty placeholder")
	}
}

func TestProviderServiceName(t *testing.T) {
	t.Parallel()
	got := ProviderServiceName("openai")
	want := "sagittarius-provider-openai"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
