package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Loader reads and writes settings.json with fork-compatible paths and behavior.
type Loader struct {
	path     string
	notifier *ReloadNotifier
}

// LoaderOption configures a Loader.
type LoaderOption func(*Loader)

// WithSettingsPath overrides the settings file path (for tests).
func WithSettingsPath(path string) LoaderOption {
	return func(l *Loader) {
		l.path = path
	}
}

// WithReloadNotifier attaches a reload notifier invoked after successful Load/Reload.
func WithReloadNotifier(n *ReloadNotifier) LoaderOption {
	return func(l *Loader) {
		l.notifier = n
	}
}

// NewLoader constructs a Loader using ResolveSettingsPath unless overridden.
func NewLoader(opts ...LoaderOption) (*Loader, error) {
	l := &Loader{}
	for _, opt := range opts {
		opt(l)
	}
	if l.path == "" {
		path, err := ResolveSettingsPath()
		if err != nil {
			return nil, fmt.Errorf("resolve settings path: %w", err)
		}
		l.path = path
	}
	return l, nil
}

// Path returns the settings file path this loader uses.
func (l *Loader) Path() string {
	return l.path
}

// Load reads settings.json, strips secrets, applies legacy migration stubs, and
// returns typed settings. ErrSecretsInSettings is returned (via errors.Join) when
// secrets were stripped from the file.
func (l *Loader) Load() (*Settings, error) {
	raw, err := os.ReadFile(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			s := &Settings{Raw: map[string]json.RawMessage{}}
			migration := MigrateLegacyLocalSettings(s)
			if migration.Migrated {
				if saveErr := l.saveDocument(s); saveErr != nil {
					return s, saveErr
				}
			}
			return s, nil
		}
		return nil, fmt.Errorf("read settings %q: %w", l.path, err)
	}

	cleaned, stripped, err := StripSecretsFromDocument(raw)
	if err != nil {
		return nil, err
	}

	s, err := decodeSettingsDocument(cleaned)
	if err != nil {
		return nil, err
	}

	migration := MigrateLegacyLocalSettings(s)
	if migration.Migrated {
		if err := l.saveDocument(s); err != nil {
			return s, fmt.Errorf("persist legacy migration: %w", err)
		}
	}

	if l.notifier != nil {
		l.notifier.Notify()
	}

	if len(stripped) > 0 {
		return s, fmt.Errorf("%w: stripped %v", ErrSecretsInSettings, stripped)
	}
	return s, nil
}

// Save writes settings to disk, rejecting documents that still contain secrets.
func (l *Loader) Save(s *Settings) error {
	if s == nil {
		return errors.New("save settings: nil settings")
	}
	out, err := encodeSettingsDocument(s)
	if err != nil {
		return err
	}
	found, err := FindSecretFields(out)
	if err != nil {
		return err
	}
	if len(found) > 0 {
		return fmt.Errorf("%w: %v", ErrSecretsInSettings, found)
	}
	return l.writeFile(out)
}

// Reload re-reads settings from disk and notifies subscribers.
func (l *Loader) Reload() (*Settings, error) {
	return l.Load()
}

// LoadProjectSettings reads <workDir>/.sagittarius/settings.json when present.
// It returns (nil, nil) when the file does not exist. Secrets are stripped (and
// ignored) — project settings are read-only here and never written back, so a
// project file cannot leak credentials into the global document. Only the
// security and sagittarius sections are consumed by callers (see
// ProjectBoundaryEnforced / SnapshotsEnabled); other sections round-trip but
// are not merged.
func LoadProjectSettings(workDir string) (*Settings, error) {
	path := ResolveProjectSettingsPath(workDir)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read project settings %q: %w", path, err)
	}
	cleaned, _, err := StripSecretsFromDocument(raw)
	if err != nil {
		return nil, err
	}
	return decodeSettingsDocument(cleaned)
}

func (l *Loader) saveDocument(s *Settings) error {
	out, err := encodeSettingsDocument(s)
	if err != nil {
		return err
	}
	return l.writeFile(out)
}

func (l *Loader) writeFile(data []byte) error {
	dir := filepath.Dir(l.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create settings dir %q: %w", dir, err)
	}
	tmp := l.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write temp settings %q: %w", tmp, err)
	}
	if err := os.Rename(tmp, l.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename settings file: %w", err)
	}
	return nil
}
