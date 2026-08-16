package hooks

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// TrustRecord holds metadata about a trusted project hook.
type TrustRecord struct {
	ProjectRoot string `json:"project_root"`
	HookKey     string `json:"hook_key"`
	Command     string `json:"command"`
	TrustedAt   string `json:"trusted_at"`
}

type trustFile struct {
	Trusted map[string]TrustRecord `json:"trusted"`
}

// TrustManager verifies and stores project hook fingerprints.
type TrustManager struct {
	globalHome string
	mu         sync.RWMutex
}

// NewTrustManager returns a TrustManager initialized with globalHome (~/.sagittarius).
func NewTrustManager(globalHome string) *TrustManager {
	return &TrustManager{globalHome: globalHome}
}

func (m *TrustManager) filePath() string {
	if m.globalHome == "" {
		return ""
	}
	return filepath.Join(m.globalHome, "trusted_hooks.json")
}

// Fingerprint calculates a SHA256 hex hash for a project hook.
func Fingerprint(projectRoot string, hook HookConfig) string {
	h := sha256.New()
	key := hook.Key()
	_, _ = fmt.Fprintf(h, "%s|%s|%s", projectRoot, key, hook.Command)
	return hex.EncodeToString(h.Sum(nil))
}

// IsTrusted checks whether a hook is trusted. Non-project hooks are always trusted.
func (m *TrustManager) IsTrusted(projectRoot string, hook HookConfig) bool {
	if hook.Source != SourceProject {
		return true
	}
	if projectRoot == "" {
		return false
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	tf := m.loadLocked()
	fp := Fingerprint(projectRoot, hook)
	_, ok := tf.Trusted[fp]
	return ok
}

// TrustHook records a project hook fingerprint in the global trust store.
func (m *TrustManager) TrustHook(projectRoot string, hook HookConfig) error {
	if hook.Source != SourceProject || projectRoot == "" {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	tf := m.loadLocked()
	if tf.Trusted == nil {
		tf.Trusted = make(map[string]TrustRecord)
	}

	fp := Fingerprint(projectRoot, hook)
	tf.Trusted[fp] = TrustRecord{
		ProjectRoot: projectRoot,
		HookKey:     hook.Key(),
		Command:     hook.Command,
		TrustedAt:   time.Now().UTC().Format(time.RFC3339),
	}

	return m.saveLocked(tf)
}

// FilterUntrusted returns untrusted project hooks from a list.
func (m *TrustManager) FilterUntrusted(projectRoot string, hooks []HookConfig) []HookConfig {
	if projectRoot == "" {
		return nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	tf := m.loadLocked()
	var untrusted []HookConfig

	for _, h := range hooks {
		if h.Source != SourceProject {
			continue
		}
		fp := Fingerprint(projectRoot, h)
		if _, ok := tf.Trusted[fp]; !ok {
			untrusted = append(untrusted, h)
		}
	}

	return untrusted
}

func (m *TrustManager) loadLocked() trustFile {
	p := m.filePath()
	if p == "" {
		return trustFile{Trusted: make(map[string]TrustRecord)}
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return trustFile{Trusted: make(map[string]TrustRecord)}
	}
	var tf trustFile
	if err := json.Unmarshal(b, &tf); err != nil || tf.Trusted == nil {
		return trustFile{Trusted: make(map[string]TrustRecord)}
	}
	return tf
}

func (m *TrustManager) saveLocked(tf trustFile) error {
	p := m.filePath()
	if p == "" {
		return fmt.Errorf("no global home directory set for trust manager")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(tf, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}
