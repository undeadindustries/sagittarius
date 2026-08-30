// Package settingsdialog implements the /settings curated settings browser.
// It shows a flat list of key settings grouped by category, with a scope radio
// (Global/Project) at the bottom. Keys marked with * are explicitly defined in
// the selected scope. Ctrl+L clears the value from the selected scope.
package settingsdialog

import (
	"context"

	"github.com/undeadindustries/sagittarius/internal/config"
)

// SettingKind classifies the type of a setting for editing.
type SettingKind int

const (
	KindHeader SettingKind = iota // section header, not editable
	KindBool
	KindInt
	KindString
	KindEnum
)

// SettingEntry is one row in the settings list.
//
// Value is the selected scope's raw value. An empty Value means the key is
// unset in that scope — never a sentinel string. Consumers (render, startEdit,
// bool/enum toggle) treat Value == "" as unset.
type SettingEntry struct {
	Key          string // dotted key path, e.g. "sagittarius.maxToolRounds"
	Label        string // human-readable display label
	Description  string // brief description shown in the footer
	Value        string // scope raw value; empty means unset
	DefaultValue string // compiled-in default from the matching config resolver
	Inherited    bool   // effective value comes from the other scope
	DefinedHere  bool   // true if this key is explicitly set in the selected scope
	MergedValue  string // other-scope or merged raw value; empty if unset everywhere
	Kind         SettingKind
	Choices      []string // for KindEnum only
	ReadOnly     bool     // show but do not allow editing
}

// EffectiveValue is the value a toggle or cycle should flip: the scope value
// if set, otherwise the inherited value, otherwise the compiled-in default.
func (e SettingEntry) EffectiveValue() string {
	if e.Value != "" {
		return e.Value
	}
	if e.MergedValue != "" {
		return e.MergedValue
	}
	return e.DefaultValue
}

// Deps provides the data and save operations the settings dialog needs.
type Deps interface {
	// ListSettings returns the curated settings with values read from the given
	// scope (not merged). DefinedHere reflects whether the key is explicitly set
	// in that scope. MergedValue is the merged effective value.
	ListSettings(scope config.SettingScope) []SettingEntry
	// SetValue writes a setting to the specified scope and reloads merged.
	SetValue(ctx context.Context, scope config.SettingScope, key, value string) error
	// ClearValue removes a setting from the specified scope only, so it falls
	// back to the other scope or the built-in default.
	ClearValue(ctx context.Context, scope config.SettingScope, key string) error
	// ProjectAvailable reports whether the project scope is writable.
	ProjectAvailable() bool
}
