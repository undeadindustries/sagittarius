package credentials

import (
	"context"
	"os"
)

const (
	keychainServicePrefix  = "sagittarius-provider-"
	forceFileStorageEnvVar = "SAGITTARIUS_FORCE_FILE_STORAGE"
)

// Store persists secrets for a single keychain service (sagittarius-provider-<id>).
// Account names follow the fork providerCredentialStorage layout (the provider id).
type Store interface {
	Get(ctx context.Context, account string) (string, error)
	Set(ctx context.Context, account, value string) error
	Delete(ctx context.Context, account string) error
	Available(ctx context.Context) bool
}

// ProviderServiceName returns the OS keychain service name for providerID.
func ProviderServiceName(providerID string) string {
	return keychainServicePrefix + providerID
}

func forceFileStorage() bool {
	return os.Getenv(forceFileStorageEnvVar) == "true"
}
