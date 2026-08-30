package settingsdialog

import (
	"context"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
)

type fakeSettingsDeps struct {
	entries []SettingEntry
	lastKey string
	lastVal string
}

func (f *fakeSettingsDeps) ListSettings(config.SettingScope) []SettingEntry { return f.entries }
func (f *fakeSettingsDeps) SetValue(_ context.Context, _ config.SettingScope, key, value string) error {
	f.lastKey = key
	f.lastVal = value
	return nil
}
func (f *fakeSettingsDeps) ClearValue(context.Context, config.SettingScope, string) error {
	return nil
}
func (f *fakeSettingsDeps) ProjectAvailable() bool { return false }

func TestStartEditUnsetRowLeavesInputEmpty(t *testing.T) {
	t.Parallel()
	e := SettingEntry{
		Key:          "sagittarius.maxToolRounds",
		Kind:         KindInt,
		DefaultValue: "100",
	}
	m := New(context.Background(), &fakeSettingsDeps{entries: []SettingEntry{e}})
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
	m := New(context.Background(), deps)
	_, _ = m.activateCurrent("enter")
	if deps.lastKey != "sagittarius.edit.enabled" || deps.lastVal != "false" {
		t.Fatalf("saved %s=%s, want sagittarius.edit.enabled=false", deps.lastKey, deps.lastVal)
	}
}
