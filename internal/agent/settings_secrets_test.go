package agent

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/credentials"
	"github.com/undeadindustries/sagittarius/internal/ui/settingsdialog"
)

// memoryCredStore is an in-memory credentials.Store so the secret tests never
// touch the real keychain.
type memoryCredStore struct {
	mu     sync.Mutex
	values map[string]string
}

func (m *memoryCredStore) Get(_ context.Context, account string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.values[account], nil
}

func (m *memoryCredStore) Set(_ context.Context, account, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.values[account] = value
	return nil
}

func (m *memoryCredStore) Delete(_ context.Context, account string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.values, account)
	return nil
}

func (m *memoryCredStore) Available(context.Context) bool { return true }

// braveSecretDeps wires the settings adapter to in-memory credential stores and
// returns the two directories the settings documents are written into.
func braveSecretDeps(t *testing.T) (deps *settingsDialogDeps, docs *config.Documents, dirs []string) {
	t.Helper()
	t.Cleanup(credentials.LockTestGlobals())

	stores := map[string]*memoryCredStore{}
	var mu sync.Mutex
	credentials.SetActiveBackendForTesting(func(_ context.Context, service string) credentials.Store {
		mu.Lock()
		defer mu.Unlock()
		if stores[service] == nil {
			stores[service] = &memoryCredStore{values: map[string]string{}}
		}
		return stores[service]
	})
	t.Cleanup(credentials.ResetForTesting)
	t.Setenv(credentials.EnvBraveAPIKey, "")

	home, project := t.TempDir(), t.TempDir()
	t.Setenv("SAGITTARIUS_HOME", home)
	docs, err := config.LoadDocuments(project)
	if err != nil {
		t.Fatalf("LoadDocuments: %v", err)
	}
	return &settingsDialogDeps{baseDialogDeps{app: &App{docs: docs}}}, docs, []string{home, project}
}

// assertNoFileContains fails if any file under dirs holds needle.
func assertNoFileContains(t *testing.T, dirs []string, needle string) {
	t.Helper()
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			body, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			if strings.Contains(string(body), needle) {
				t.Errorf("%s contains the secret", path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
}

// TestBraveKeyRowIsASecret pins the row's shape: a credential must never carry
// a value or a default into the list, or the dialog would render the key.
func TestBraveKeyRowIsASecret(t *testing.T) {
	docs := loadEmptyDocs(t)

	e := mustEntry(t, listSettings(docs, config.ScopeGlobal), braveAPIKeySetting)
	if e.Kind != settingsdialog.KindSecret {
		t.Fatalf("Kind = %v, want KindSecret", e.Kind)
	}
	if e.Value != "" || e.DefaultValue != "" || e.MergedValue != "" {
		t.Fatalf("secret row carries values: %+v", e)
	}
	if !strings.Contains(e.Description, "never in settings.json") {
		t.Fatalf("Description = %q, want it to say the key stays out of settings.json", e.Description)
	}
}

// TestBraveKeyNeverReachesSettingsJSON is the disclosure guard: settings
// documents are plaintext on disk, so the key must appear in neither.
func TestBraveKeyNeverReachesSettingsJSON(t *testing.T) {
	deps, docs, dirs := braveSecretDeps(t)
	ctx := context.Background()
	const secret = "BSA-plaintext-would-be-a-leak"

	if err := deps.SetValue(ctx, config.ScopeGlobal, braveAPIKeySetting, secret); err != nil {
		t.Fatalf("SetValue() error = %v", err)
	}
	// Force both documents to disk; a routing bug would surface as a saved key.
	for _, scope := range []config.SettingScope{config.ScopeGlobal, config.ScopeProject} {
		if err := docs.Save(scope); err != nil {
			t.Fatalf("Save(%v): %v", scope, err)
		}
	}
	assertNoFileContains(t, dirs, secret)

	got, err := credentials.ResolveBraveAPIKey(ctx)
	if err != nil {
		t.Fatalf("ResolveBraveAPIKey() error = %v", err)
	}
	if got != secret {
		t.Fatalf("stored key = %q, want the saved secret", got)
	}
}

// TestBraveKeyIgnoresScope covers the project-scope path: secure storage has no
// scopes, so a project-scoped save must still write the one shared key rather
// than silently doing nothing or creating a project settings entry.
func TestBraveKeyIgnoresScope(t *testing.T) {
	deps, _, _ := braveSecretDeps(t)
	ctx := context.Background()

	if err := deps.SetValue(ctx, config.ScopeProject, braveAPIKeySetting, "scoped-key"); err != nil {
		t.Fatalf("SetValue(project) error = %v", err)
	}
	got, err := credentials.ResolveBraveAPIKey(ctx)
	if err != nil || got != "scoped-key" {
		t.Fatalf("resolved %q err %v, want the key stored regardless of scope", got, err)
	}
}

func TestBraveKeyClearRemovesFromStore(t *testing.T) {
	deps, _, _ := braveSecretDeps(t)
	ctx := context.Background()

	if err := deps.SetValue(ctx, config.ScopeGlobal, braveAPIKeySetting, "key"); err != nil {
		t.Fatalf("SetValue() error = %v", err)
	}
	if err := deps.ClearValue(ctx, config.ScopeGlobal, braveAPIKeySetting); err != nil {
		t.Fatalf("ClearValue() error = %v", err)
	}

	got, err := credentials.ResolveBraveAPIKey(ctx)
	if err != nil || got != "" {
		t.Fatalf("resolved %q err %v after clear, want empty", got, err)
	}
}

func TestBraveSecretStatusReportsState(t *testing.T) {
	deps, _, _ := braveSecretDeps(t)
	ctx := context.Background()

	status, err := deps.SecretStatus(ctx, braveAPIKeySetting)
	if err != nil {
		t.Fatalf("SecretStatus() error = %v", err)
	}
	if status != "not set" {
		t.Fatalf("status = %q on a clean store, want %q", status, "not set")
	}

	if err := deps.SetValue(ctx, config.ScopeGlobal, braveAPIKeySetting, "key"); err != nil {
		t.Fatalf("SetValue() error = %v", err)
	}
	status, err = deps.SecretStatus(ctx, braveAPIKeySetting)
	if err != nil {
		t.Fatalf("SecretStatus() error = %v", err)
	}
	if status != "stored securely" {
		t.Fatalf("status = %q, want %q", status, "stored securely")
	}
}

// TestBraveSecretStatusNamesEnvShadowing is the anti-confusion case: an env var
// silently wins over a saved key, so the row has to say the save had no effect.
func TestBraveSecretStatusNamesEnvShadowing(t *testing.T) {
	deps, _, _ := braveSecretDeps(t)
	ctx := context.Background()
	t.Setenv(credentials.EnvBraveAPIKey, "env-key")

	if err := deps.SetValue(ctx, config.ScopeGlobal, braveAPIKeySetting, "stored-key"); err != nil {
		t.Fatalf("SetValue() error = %v", err)
	}
	status, err := deps.SecretStatus(ctx, braveAPIKeySetting)
	if err != nil {
		t.Fatalf("SecretStatus() error = %v", err)
	}
	if !strings.Contains(status, credentials.EnvBraveAPIKey) || !strings.Contains(status, "ignored") {
		t.Fatalf("status = %q, want it to name %s and say the stored key is ignored", status, credentials.EnvBraveAPIKey)
	}
}

// TestBraveSecretStatusDoesNotClaimAbsenceOnReadError guards the AD-129 lesson:
// an unreadable keyring is not an unconfigured key, and saying "not set" would
// invite the user to re-enter a key that is already stored.
func TestBraveSecretStatusDoesNotClaimAbsenceOnReadError(t *testing.T) {
	readErr := errors.New("keyring is locked")

	got := describeBraveKey(credentials.BraveKeyStatus{}, readErr)
	if strings.Contains(got, "not set") {
		t.Fatalf("status = %q on a failed read, want it to not claim absence", got)
	}
	if !strings.Contains(got, "unknown") {
		t.Fatalf("status = %q, want it to report the state as unknown", got)
	}

	// An env var answers on its own, so it stays authoritative.
	got = describeBraveKey(credentials.BraveKeyStatus{EnvSet: true}, readErr)
	if !strings.Contains(got, credentials.EnvBraveAPIKey) {
		t.Fatalf("status = %q, want it to still name %s", got, credentials.EnvBraveAPIKey)
	}
}

func TestSecretStatusRejectsUnknownKey(t *testing.T) {
	deps, _, _ := braveSecretDeps(t)

	if _, err := deps.SecretStatus(context.Background(), "ui.theme"); err == nil {
		t.Fatal(`SecretStatus("ui.theme") = nil error, want a rejection`)
	}
}

// TestBraveKeyIsNotASettingsField keeps the pseudo-key out of the settings.json
// writers: routing happens in SetValue, and a leak past it must fail loudly
// rather than land the secret in a document.
func TestBraveKeyIsNotASettingsField(t *testing.T) {
	s := &config.Settings{}

	if err := applySettingValue(s, braveAPIKeySetting, "secret"); err == nil {
		t.Fatal("applySettingValue accepted the credential key")
	}
	if err := clearSettingValue(s, braveAPIKeySetting); err == nil {
		t.Fatal("clearSettingValue accepted the credential key")
	}
}
