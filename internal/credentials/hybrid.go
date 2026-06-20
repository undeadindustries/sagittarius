package credentials

import (
	"context"
	"sync"
)

var (
	backendMu       sync.RWMutex
	activeBackendFn = defaultActiveBackend
)

type hybridStore struct {
	service string
}

func newProviderStore(providerID string) Store {
	return &hybridStore{service: ProviderServiceName(providerID)}
}

func (h *hybridStore) Get(ctx context.Context, account string) (string, error) {
	return activeBackend(ctx, h.service).Get(ctx, account)
}

func (h *hybridStore) Set(ctx context.Context, account, value string) error {
	return activeBackend(ctx, h.service).Set(ctx, account, value)
}

func (h *hybridStore) Delete(ctx context.Context, account string) error {
	return activeBackend(ctx, h.service).Delete(ctx, account)
}

func (h *hybridStore) Available(ctx context.Context) bool {
	return activeBackend(ctx, h.service).Available(ctx)
}

func defaultActiveBackend(ctx context.Context, service string) Store {
	if forceFileStorage() {
		store, err := sharedEncryptedFileStore(service)
		if err != nil {
			return unavailableStore{err: err}
		}
		return store
	}
	kr := newKeyringStore(service)
	if kr.Available(ctx) {
		return kr
	}
	store, err := sharedEncryptedFileStore(service)
	if err != nil {
		return unavailableStore{err: err}
	}
	return store
}

type unavailableStore struct {
	err error
}

func (u unavailableStore) Get(context.Context, string) (string, error) {
	return "", u.err
}

func (u unavailableStore) Set(context.Context, string, string) error {
	return u.err
}

func (u unavailableStore) Delete(context.Context, string) error {
	return u.err
}

func (u unavailableStore) Available(context.Context) bool {
	return false
}

// SetActiveBackendForTesting replaces backend selection (restore with ResetForTesting).
func SetActiveBackendForTesting(fn func(context.Context, string) Store) {
	backendMu.Lock()
	defer backendMu.Unlock()
	activeBackendFn = fn
}

// ResetForTesting restores default factories/backends and clears the API key cache.
func ResetForTesting() {
	backendMu.Lock()
	activeBackendFn = defaultActiveBackend
	backendMu.Unlock()
	storeFactoryMu.Lock()
	storeFactory = func(providerID string) Store { return newProviderStore(providerID) }
	storeFactoryMu.Unlock()
	resetAPIKeyCache()
	apiKeyCache.mu.Lock()
	apiKeyCache.ttl = defaultCacheTTL
	apiKeyCache.mu.Unlock()
	credentialsPathForTesting = ""
	sharedFileStore = nil
	sharedFileStoreErr = nil
	sharedFileStoreOnce = sync.Once{}
}

func activeBackend(ctx context.Context, service string) Store {
	backendMu.RLock()
	fn := activeBackendFn
	backendMu.RUnlock()
	return fn(ctx, service)
}
