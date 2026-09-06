package credentials

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// useMemoryBackends routes every hybridStore through in-memory stores keyed by
// service, so the search-key tests never touch the real keychain.
func useMemoryBackends(t *testing.T) map[string]*memoryStore {
	t.Helper()
	stores := map[string]*memoryStore{}
	SetActiveBackendForTesting(func(_ context.Context, service string) Store {
		if _, ok := stores[service]; !ok {
			stores[service] = newMemoryStore(service)
		}
		return stores[service]
	})
	t.Cleanup(ResetForTesting)
	return stores
}

func TestBraveKeyRoundTrip(t *testing.T) {
	t.Cleanup(LockTestGlobals())
	ctx := testContext(t)
	stores := useMemoryBackends(t)
	withoutEnv(t, EnvBraveAPIKey)

	const key = "brave-secret"
	if err := SetBraveAPIKey(ctx, key); err != nil {
		t.Fatalf("SetBraveAPIKey() error = %v", err)
	}

	got, err := ResolveBraveAPIKey(ctx)
	if err != nil {
		t.Fatalf("ResolveBraveAPIKey() error = %v", err)
	}
	if got != key {
		t.Fatalf("ResolveBraveAPIKey() = %q, want %q", got, key)
	}

	// The key must land in its own service, not a provider or MCP slot.
	svc := SearchServiceName("brave")
	if stores[svc] == nil || stores[svc].values["brave"] != key {
		t.Fatalf("key not stored under service %q: %v", svc, stores)
	}
	if strings.HasPrefix(svc, keychainServicePrefix) || strings.HasPrefix(svc, mcpServicePrefix) {
		t.Fatalf("search service %q collides with the provider or MCP namespace", svc)
	}

	if err := DeleteBraveAPIKey(ctx); err != nil {
		t.Fatalf("DeleteBraveAPIKey() error = %v", err)
	}
	got, err = ResolveBraveAPIKey(ctx)
	if err != nil || got != "" {
		t.Fatalf("after delete: got %q err %v, want empty", got, err)
	}
}

// TestBraveEnvWinsOverStored pins the precedence the /settings row advertises.
func TestBraveEnvWinsOverStored(t *testing.T) {
	t.Cleanup(LockTestGlobals())
	ctx := testContext(t)
	useMemoryBackends(t)
	withEnv(t, EnvBraveAPIKey, "env-key")

	if err := SetBraveAPIKey(ctx, "stored-key"); err != nil {
		t.Fatalf("SetBraveAPIKey() error = %v", err)
	}

	got, err := ResolveBraveAPIKey(ctx)
	if err != nil {
		t.Fatalf("ResolveBraveAPIKey() error = %v", err)
	}
	if got != "env-key" {
		t.Fatalf("ResolveBraveAPIKey() = %q, want the env value", got)
	}

	status, err := BraveAPIKeyStatus(ctx)
	if err != nil {
		t.Fatalf("BraveAPIKeyStatus() error = %v", err)
	}
	if !status.EnvSet || !status.Stored {
		t.Fatalf("status = %+v, want both set so the UI can report the shadowing", status)
	}
}

func TestBraveKeyStatusStates(t *testing.T) {
	t.Cleanup(LockTestGlobals())
	ctx := testContext(t)
	useMemoryBackends(t)
	withoutEnv(t, EnvBraveAPIKey)

	status, err := BraveAPIKeyStatus(ctx)
	if err != nil {
		t.Fatalf("BraveAPIKeyStatus() error = %v", err)
	}
	if status.EnvSet || status.Stored {
		t.Fatalf("status = %+v on a clean store, want both false", status)
	}

	if err := SetBraveAPIKey(ctx, "stored-key"); err != nil {
		t.Fatalf("SetBraveAPIKey() error = %v", err)
	}
	status, err = BraveAPIKeyStatus(ctx)
	if err != nil {
		t.Fatalf("BraveAPIKeyStatus() error = %v", err)
	}
	if status.EnvSet || !status.Stored {
		t.Fatalf("status = %+v, want Stored only", status)
	}
}

// TestSetBraveKeyRejectsEmpty keeps a blank save from silently overwriting a
// working key with nothing.
func TestSetBraveKeyRejectsEmpty(t *testing.T) {
	t.Cleanup(LockTestGlobals())
	ctx := testContext(t)
	useMemoryBackends(t)

	for _, in := range []string{"", "   ", "\t\n"} {
		if err := SetBraveAPIKey(ctx, in); err == nil {
			t.Fatalf("SetBraveAPIKey(%q) = nil, want an error", in)
		}
	}
}

func TestDeleteBraveKeyIsIdempotent(t *testing.T) {
	t.Cleanup(LockTestGlobals())
	ctx := testContext(t)
	useMemoryBackends(t)

	if err := DeleteBraveAPIKey(ctx); err != nil {
		t.Fatalf("DeleteBraveAPIKey() on an empty store = %v, want nil", err)
	}
}

// TestBraveResolveReportsStoreFailure proves a broken keychain is surfaced
// rather than being indistinguishable from "no key configured".
func TestBraveResolveReportsStoreFailure(t *testing.T) {
	t.Cleanup(LockTestGlobals())
	ctx := testContext(t)
	withoutEnv(t, EnvBraveAPIKey)

	boom := errors.New("keyring locked")
	SetActiveBackendForTesting(func(context.Context, string) Store {
		return unavailableStore{err: boom}
	})
	t.Cleanup(ResetForTesting)

	if _, err := ResolveBraveAPIKey(ctx); !errors.Is(err, boom) {
		t.Fatalf("ResolveBraveAPIKey() error = %v, want %v wrapped", err, boom)
	}
	if _, err := BraveAPIKeyStatus(ctx); !errors.Is(err, boom) {
		t.Fatalf("BraveAPIKeyStatus() error = %v, want %v wrapped", err, boom)
	}
}
