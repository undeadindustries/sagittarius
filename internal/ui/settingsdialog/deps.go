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
type SettingEntry struct {
	Key         string      // dotted key path, e.g. "sagittarius.maxToolRounds"
	Label       string      // human-readable display label
	Description string      // brief description shown in the footer
	Value       string      // raw value from the selected scope, or "(not set)"
	DefinedHere bool        // true if this key is explicitly set in the selected scope
	MergedValue string      // effective merged value (from the other scope or default)
	Kind        SettingKind
	Choices     []string // for KindEnum only
	ReadOnly    bool     // show but do not allow editing
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
