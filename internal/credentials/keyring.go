package credentials

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/zalando/go-keyring"
)

const keychainTestPrefix = "__keychain_test__"

// keyringStore implements Store using the OS credential manager.
type keyringStore struct {
	service string
}

func newKeyringStore(service string) *keyringStore {
	return &keyringStore{service: service}
}

func (k *keyringStore) Get(_ context.Context, account string) (string, error) {
	value, err := keyring.Get(k.service, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("keyring get %q/%q: %w", k.service, account, err)
	}
	return value, nil
}

func (k *keyringStore) Set(_ context.Context, account, value string) error {
	if err := keyring.Set(k.service, account, value); err != nil {
		return fmt.Errorf("keyring set %q/%q: %w", k.service, account, err)
	}
	return nil
}

func (k *keyringStore) Delete(_ context.Context, account string) error {
	if err := keyring.Delete(k.service, account); err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("keyring delete %q/%q: %w", k.service, account, err)
	}
	return nil
}

func (k *keyringStore) Available(ctx context.Context) bool {
	return probeKeyring(ctx, k.service)
}

func probeKeyring(ctx context.Context, service string) bool {
	randBytes := make([]byte, 8)
	if _, err := rand.Read(randBytes); err != nil {
		return false
	}
	testAccount := keychainTestPrefix + hex.EncodeToString(randBytes)
	testPassword := "test"

	done := make(chan bool, 1)
	go func() {
		ok := false
		if err := keyring.Set(service, testAccount, testPassword); err == nil {
			got, err := keyring.Get(service, testAccount)
			if err == nil && got == testPassword {
				_ = keyring.Delete(service, testAccount)
				ok = true
			}
		}
		done <- ok
	}()

	select {
	case <-ctx.Done():
		return false
	case ok := <-done:
		return ok
	case <-time.After(2 * time.Second):
		return false
	}
}
