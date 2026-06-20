package credentials

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/undeadindustries/sagittarius/internal/config"

	"golang.org/x/crypto/scrypt"
)

const (
	fileCredentialsName = "gemini-credentials.json"
	scryptPassword      = "gemini-cli-oauth"
	scryptSaltSuffix    = "gemini-cli"
)

// fileStore implements Store using an AES-256-GCM encrypted JSON file.
// Format mirrors fork FileKeychain for cross-tool compatibility.
type fileStore struct {
	service string
	path    string
	key     []byte
}

var (
	sharedFileStore           *fileStore
	sharedFileStoreErr        error
	sharedFileStoreOnce       sync.Once
	fileStoreMu               sync.Mutex
	credentialsPathForTesting string
)

// SetCredentialsPathForTesting overrides ~/.gemini/gemini-credentials.json for tests.
func SetCredentialsPathForTesting(path string) {
	credentialsPathForTesting = path
	sharedFileStore = nil
	sharedFileStoreErr = nil
	sharedFileStoreOnce = sync.Once{}
}

func sharedEncryptedFileStore(service string) (*fileStore, error) {
	sharedFileStoreOnce.Do(func() {
		var path string
		if credentialsPathForTesting != "" {
			path = credentialsPathForTesting
		} else {
			dir, err := config.ResolveGeminiDir()
			if err != nil {
				sharedFileStoreErr = fmt.Errorf("resolve gemini dir: %w", err)
				return
			}
			path = filepath.Join(dir, fileCredentialsName)
		}
		key, err := deriveFileEncryptionKey()
		if err != nil {
			sharedFileStoreErr = err
			return
		}
		sharedFileStore = &fileStore{
			path: path,
			key:  key,
		}
	})
	if sharedFileStoreErr != nil {
		return nil, sharedFileStoreErr
	}
	return &fileStore{
		service: service,
		path:    sharedFileStore.path,
		key:     sharedFileStore.key,
	}, nil
}

func deriveFileEncryptionKey() ([]byte, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("hostname: %w", err)
	}
	user, err := currentUsername()
	if err != nil {
		return nil, err
	}
	salt := hostname + "-" + user + "-" + scryptSaltSuffix
	return scrypt.Key([]byte(scryptPassword), []byte(salt), 16384, 8, 1, 32)
}

func currentUsername() (string, error) {
	if u := os.Getenv("USER"); u != "" {
		return u, nil
	}
	if u := os.Getenv("USERNAME"); u != "" {
		return u, nil
	}
	return os.UserHomeDir()
}

func (f *fileStore) Get(_ context.Context, account string) (string, error) {
	fileStoreMu.Lock()
	defer fileStoreMu.Unlock()

	data, err := f.loadData()
	if err != nil {
		return "", err
	}
	if svc, ok := data[f.service]; ok {
		if val, ok := svc[account]; ok {
			return val, nil
		}
	}
	return "", nil
}

func (f *fileStore) Set(_ context.Context, account, value string) error {
	fileStoreMu.Lock()
	defer fileStoreMu.Unlock()

	data, err := f.loadData()
	if err != nil {
		return err
	}
	if data[f.service] == nil {
		data[f.service] = map[string]string{}
	}
	data[f.service][account] = value
	return f.saveData(data)
}

func (f *fileStore) Delete(_ context.Context, account string) error {
	fileStoreMu.Lock()
	defer fileStoreMu.Unlock()

	data, err := f.loadData()
	if err != nil {
		return err
	}
	svc, ok := data[f.service]
	if !ok {
		return nil
	}
	if _, ok := svc[account]; !ok {
		return nil
	}
	delete(svc, account)
	if len(svc) == 0 {
		delete(data, f.service)
	}
	if len(data) == 0 {
		if err := os.Remove(f.path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove credentials file: %w", err)
		}
		return nil
	}
	return f.saveData(data)
}

func (f *fileStore) Available(context.Context) bool {
	return true
}

func (f *fileStore) loadData() (map[string]map[string]string, error) {
	raw, err := os.ReadFile(f.path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]map[string]string{}, nil
		}
		return nil, fmt.Errorf("read credentials file: %w", err)
	}
	plain, err := decryptFilePayload(raw, f.key)
	if err != nil {
		return nil, fmt.Errorf("decrypt credentials at %s: %w", f.path, err)
	}
	var data map[string]map[string]string
	if err := json.Unmarshal(plain, &data); err != nil {
		return nil, fmt.Errorf("parse credentials json: %w", err)
	}
	if data == nil {
		return map[string]map[string]string{}, nil
	}
	return data, nil
}

func (f *fileStore) saveData(data map[string]map[string]string) error {
	dir := filepath.Dir(f.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create credentials dir: %w", err)
	}
	plain, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}
	encrypted, err := encryptFilePayload(plain, f.key)
	if err != nil {
		return err
	}
	if err := os.WriteFile(f.path, encrypted, 0o600); err != nil {
		return fmt.Errorf("write credentials file: %w", err)
	}
	return nil
}

func encryptFilePayload(plain, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCMWithNonceSize(block, 16)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}
	iv := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, fmt.Errorf("random iv: %w", err)
	}
	ciphertext := gcm.Seal(nil, iv, plain, nil)
	authTag := ciphertext[len(ciphertext)-gcm.Overhead():]
	encrypted := ciphertext[:len(ciphertext)-gcm.Overhead()]
	out := hex.EncodeToString(iv) + ":" + hex.EncodeToString(authTag) + ":" + hex.EncodeToString(encrypted)
	return []byte(out), nil
}

func decryptFilePayload(raw, key []byte) ([]byte, error) {
	parts := splitEncryptedPayload(string(raw))
	if len(parts) != 3 {
		return nil, errors.New("invalid encrypted data format")
	}
	iv, err := hex.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("decode iv: %w", err)
	}
	authTag, err := hex.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode auth tag: %w", err)
	}
	encrypted, err := hex.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("decode ciphertext: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCMWithNonceSize(block, 16)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}
	ciphertext := append(append([]byte{}, encrypted...), authTag...)
	plain, err := gcm.Open(nil, iv, ciphertext, nil)
	if err != nil {
		return nil, errors.New("unable to authenticate data")
	}
	return plain, nil
}

func splitEncryptedPayload(s string) []string {
	return []string{
		segmentBefore(s, 0),
		segmentBefore(s, 1),
		segmentAfterSecondColon(s),
	}
}

func segmentBefore(s string, idx int) string {
	start := 0
	for i := 0; i <= idx; i++ {
		pos := indexFrom(s, ':', start)
		if pos < 0 {
			return ""
		}
		if i == idx {
			return s[start:pos]
		}
		start = pos + 1
	}
	return ""
}

func segmentAfterSecondColon(s string) string {
	start := 0
	for i := 0; i < 2; i++ {
		pos := indexFrom(s, ':', start)
		if pos < 0 {
			return ""
		}
		start = pos + 1
	}
	return s[start:]
}

func indexFrom(s string, sep byte, start int) int {
	for i := start; i < len(s); i++ {
		if s[i] == sep {
			return i
		}
	}
	return -1
}
