package settingsdialog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/ui/scopedialog"
)

type fakeSettingsDeps struct {
	entries []SettingEntry

	lastKey      string
	lastVal      string
	setErr       error
	clearedKeys  []string
	statusFor    map[string]string
	statusErr    error
	statusCalls  int
	statusCtxNil bool
}

func (f *fakeSettingsDeps) ListSettings(config.SettingScope) []SettingEntry { return f.entries }
func (f *fakeSettingsDeps) SetValue(_ context.Context, _ config.SettingScope, key, value string) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.lastKey = key
	f.lastVal = value
	return nil
}
func (f *fakeSettingsDeps) ClearValue(_ context.Context, _ config.SettingScope, key string) error {
	f.clearedKeys = append(f.clearedKeys, key)
	return nil
}

func (f *fakeSettingsDeps) SecretStatus(ctx context.Context, key string) (string, error) {
	f.statusCalls++
	f.statusCtxNil = ctx == nil
	return f.statusFor[key], f.statusErr
}
func (f *fakeSettingsDeps) ProjectAvailable() bool { return false }

// secretDeps builds deps around a single secret row.
func secretDeps(status string) *fakeSettingsDeps {
	const key = "secrets.braveApiKey"
	return &fakeSettingsDeps{
		entries:   []SettingEntry{{Key: key, Label: "Brave Search API key", Kind: KindSecret}},
		statusFor: map[string]string{key: status},
	}
}

func TestStartEditUnsetRowLeavesInputEmpty(t *testing.T) {
	t.Parallel()
	e := SettingEntry{
		Key:          "sagittarius.maxToolRounds",
		Kind:         KindInt,
		DefaultValue: "100",
	}
	m, _ := New(context.Background(), &fakeSettingsDeps{entries: []SettingEntry{e}})
	m, _ = m.startEdit(e)
	if got := m.input.Value(); got != "" {
		t.Fatalf("input value = %q, want empty", got)
	}
}

func TestSettingDisplayThreeStates(t *testing.T) {
	t.Parallel()
	set := SettingEntry{Value: "true", DefinedHere: true, DefaultValue: "false"}
	val, suf := settingDisplay(set, config.ScopeGlobal)
	if val != "true" || suf != "" {
		t.Fatalf("set: %q %q", val, suf)
	}
	inherited := SettingEntry{MergedValue: "true", Inherited: true, DefaultValue: "false"}
	val, suf = settingDisplay(inherited, config.ScopeProject)
	if val != "true" || suf != suffixFromGlobal {
		t.Fatalf("inherited: %q %q", val, suf)
	}
	unset := SettingEntry{DefaultValue: "true"}
	val, suf = settingDisplay(unset, config.ScopeGlobal)
	if val != "true" || suf != suffixDefault {
		t.Fatalf("default: %q %q", val, suf)
	}
}

func TestActivateCurrentUnsetDefaultTrueWritesFalse(t *testing.T) {
	t.Parallel()
	deps := &fakeSettingsDeps{
		entries: []SettingEntry{{
			Key:          "sagittarius.edit.enabled",
			Kind:         KindBool,
			DefaultValue: "true",
		}},
	}
	m, _ := New(context.Background(), deps)
	_, _ = m.activateCurrent("enter")
	if deps.lastKey != "sagittarius.edit.enabled" || deps.lastVal != "false" {
		t.Fatalf("saved %s=%s, want sagittarius.edit.enabled=false", deps.lastKey, deps.lastVal)
	}
}

// TestNewDefersSecretProbe pins the AD-062 rule for the credentials probe:
// reading the OS keychain can block for seconds, so New must hand the caller a
// command rather than calling SecretStatus on the Update goroutine.
func TestNewDefersSecretProbe(t *testing.T) {
	t.Parallel()
	deps := secretDeps("stored securely")

	m, cmd := New(context.Background(), deps)
	if deps.statusCalls != 0 {
		t.Fatalf("SecretStatus called %d times during New, want 0", deps.statusCalls)
	}
	if cmd == nil {
		t.Fatal("New returned no command, so the secret row would never resolve")
	}
	if got := m.entries[0].StatusText; got != "" {
		t.Fatalf("StatusText = %q before the probe, want empty", got)
	}

	m, _ = m.Update(cmd())
	if deps.statusCalls != 1 {
		t.Fatalf("SecretStatus called %d times, want 1", deps.statusCalls)
	}
	if got := m.entries[0].StatusText; got != "stored securely" {
		t.Fatalf("StatusText = %q, want %q", got, "stored securely")
	}
}

func TestNewSkipsProbeWithoutSecretRows(t *testing.T) {
	t.Parallel()
	deps := &fakeSettingsDeps{entries: []SettingEntry{{Key: "ui.theme", Kind: KindEnum}}}

	if _, cmd := New(context.Background(), deps); cmd != nil {
		t.Fatal("New returned a probe command for a list with no secret rows")
	}
}

func TestSecretRowRendersStatusNotValue(t *testing.T) {
	t.Parallel()
	unset := SettingEntry{Kind: KindSecret}
	if val, suf := settingDisplay(unset, config.ScopeGlobal); val != statusChecking || suf != "" {
		t.Fatalf("unprobed secret: %q %q, want %q and no suffix", val, suf, statusChecking)
	}
	// DefaultValue and MergedValue must not leak into a secret row: it has no
	// scope value, so the usual (default)/(from global) suffixes are wrong.
	probed := SettingEntry{Kind: KindSecret, StatusText: "not set", DefaultValue: "x", MergedValue: "y"}
	if val, suf := settingDisplay(probed, config.ScopeProject); val != "not set" || suf != "" {
		t.Fatalf("probed secret: %q %q, want %q and no suffix", val, suf, "not set")
	}
}

func TestStartEditSecretMasksAndDoesNotPrefill(t *testing.T) {
	t.Parallel()
	e := SettingEntry{Key: "secrets.braveApiKey", Kind: KindSecret, StatusText: "stored securely"}
	m, _ := New(context.Background(), secretDeps("stored securely"))

	m, _ = m.startEdit(e)
	if got := m.input.Value(); got != "" {
		t.Fatalf("input prefilled with %q, want empty for a write-only secret", got)
	}
	if m.input.EchoMode != textinput.EchoPassword {
		t.Fatalf("EchoMode = %v, want EchoPassword", m.input.EchoMode)
	}
}

// TestSaveSecretNeverEchoesValue is the disclosure guard: the status line is
// rendered into the scrollback and must not carry the key.
func TestSaveSecretNeverEchoesValue(t *testing.T) {
	t.Parallel()
	const secret = "BSA-super-secret-token"
	deps := secretDeps("not set")
	m, _ := New(context.Background(), deps)

	m, _ = m.startEdit(m.entries[0])
	m.input.SetValue(secret)
	m, _ = m.commitEdit()

	if deps.lastVal != secret {
		t.Fatalf("saved value = %q, want the typed secret", deps.lastVal)
	}
	if strings.Contains(m.info, secret) {
		t.Fatalf("info line %q leaks the secret", m.info)
	}
	if !strings.Contains(m.info, "secure storage") {
		t.Fatalf("info line %q does not say where the key went", m.info)
	}
	// The scope radio is meaningless for a credential; claiming one would imply
	// a per-project key that does not exist.
	if strings.Contains(m.info, fmt.Sprint(config.ScopeGlobal)) {
		t.Fatalf("info line %q claims a scope", m.info)
	}
}

func TestSaveSecretReprobesStatus(t *testing.T) {
	t.Parallel()
	deps := secretDeps("not set")
	m, _ := New(context.Background(), deps)

	m, _ = m.startEdit(m.entries[0])
	m.input.SetValue("key")
	m, cmd := m.commitEdit()
	if cmd == nil {
		t.Fatal("commitEdit returned no re-probe command, so the row would stay stale")
	}

	deps.statusFor["secrets.braveApiKey"] = "stored securely"
	m, _ = m.Update(cmd())
	if got := m.entries[0].StatusText; got != "stored securely" {
		t.Fatalf("StatusText = %q after save, want the re-probed value", got)
	}
}

// TestCommitEmptySecretIsRejected keeps "save nothing" from reading as a
// removal: SetValue would either error deep in the store or, worse, look like
// it cleared a key that is still there.
func TestCommitEmptySecretIsRejected(t *testing.T) {
	t.Parallel()
	deps := secretDeps("stored securely")
	m, _ := New(context.Background(), deps)

	m, _ = m.startEdit(m.entries[0])
	m, _ = m.commitEdit()

	if deps.lastKey != "" {
		t.Fatalf("SetValue called with %q, want no call for an empty secret", deps.lastKey)
	}
	if !strings.Contains(m.errMsg, "Ctrl+L") {
		t.Fatalf("errMsg = %q, want the removal hint", m.errMsg)
	}
}

func TestClearSecretRemovesFromStore(t *testing.T) {
	t.Parallel()
	deps := secretDeps("stored securely")
	m, _ := New(context.Background(), deps)

	m, cmd := m.clearCurrent()
	if len(deps.clearedKeys) != 1 || deps.clearedKeys[0] != "secrets.braveApiKey" {
		t.Fatalf("cleared %v, want [secrets.braveApiKey]", deps.clearedKeys)
	}
	if !strings.Contains(m.info, "secure storage") {
		t.Fatalf("info = %q, want it to name secure storage", m.info)
	}
	if cmd == nil {
		t.Fatal("clearCurrent returned no re-probe command")
	}
}

func TestSecretProbeErrorSurfaces(t *testing.T) {
	t.Parallel()
	deps := secretDeps("")
	deps.statusErr = errors.New("keyring locked")

	m, cmd := New(context.Background(), deps)
	m, _ = m.Update(cmd())
	if !strings.Contains(m.errMsg, "keyring locked") {
		t.Fatalf("errMsg = %q, want the probe error surfaced", m.errMsg)
	}
}

// TestScopeChangeKeepsSecretStatus covers the rebuild path: ListSettings
// returns fresh rows with no StatusText, so a scope flip must not blank the
// probed state and leave the row stuck on "checking…".
func TestScopeChangeKeepsSecretStatus(t *testing.T) {
	t.Parallel()
	deps := secretDeps("stored securely")
	m, cmd := New(context.Background(), deps)
	m, _ = m.Update(cmd())

	m, _ = m.Update(scopedialog.ScopeChangedMsg{})
	if got := m.entries[0].StatusText; got != "stored securely" {
		t.Fatalf("StatusText = %q after scope change, want it preserved", got)
	}
}
