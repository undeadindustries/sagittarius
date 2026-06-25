package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/ui/settingsdialog"
)

// SettingsDialogDeps returns the side-effect adapter the /settings browser uses.
func (a *App) SettingsDialogDeps() settingsdialog.Deps {
	return &settingsDialogDeps{baseDialogDeps{app: a}}
}

type settingsDialogDeps struct{ baseDialogDeps }

func (d *settingsDialogDeps) docs() *config.Documents { return d.app.docs }

// ListSettings returns the curated settings list with values drawn from the
// given scope (not merged). DefinedHere is true when the key exists in that
// scope's file; MergedValue is what the runtime actually uses.
func (d *settingsDialogDeps) ListSettings(scope config.SettingScope) []settingsdialog.SettingEntry {
	docs := d.docs()
	scopeSettings := docs.TargetSettings(scope)
	merged := docs.Merged()
	if merged == nil {
		merged = scopeSettings
	}

	notSet := "(not set)"

	boolVal := func(p *bool) string {
		if p == nil {
			return notSet
		}
		if *p {
			return "true"
		}
		return "false"
	}
	intVal := func(p *int) string {
		if p == nil {
			return notSet
		}
		return strconv.Itoa(*p)
	}

	// --- General ---
	var maxRounds, maxRoundsMerged string
	if scopeSettings.Sagittarius != nil {
		maxRounds = intVal(scopeSettings.Sagittarius.MaxToolRounds)
	} else {
		maxRounds = notSet
	}
	if merged.Sagittarius != nil {
		maxRoundsMerged = intVal(merged.Sagittarius.MaxToolRounds)
	} else {
		maxRoundsMerged = notSet
	}

	// --- UI ---
	scopeUI := scopeSettings.UI()
	mergedUI := merged.UI()
	uiDefined := scopeSettings.Raw != nil
	if _, ok := scopeSettings.Raw["ui"]; !ok {
		uiDefined = false
	}

	themeVal := func(s string) string {
		if s == "" {
			return notSet
		}
		return s
	}

	// --- Security ---
	var secEnforce, secEnforceMerged string
	var secDefined bool
	if scopeSettings.Security != nil && scopeSettings.Security.ProjectBoundary != nil {
		secEnforce = boolVal(scopeSettings.Security.ProjectBoundary.Enforce)
		secDefined = true
	} else {
		secEnforce = notSet
	}
	if merged.Security != nil && merged.Security.ProjectBoundary != nil {
		secEnforceMerged = boolVal(merged.Security.ProjectBoundary.Enforce)
	} else {
		secEnforceMerged = notSet
	}

	// --- Snapshots ---
	var snapEnabled, snapEnabledMerged string
	var snapMaxBytes, snapMaxBytesMerged string
	var snapDefined bool
	if scopeSettings.Sagittarius != nil && scopeSettings.Sagittarius.Snapshots != nil {
		s := scopeSettings.Sagittarius.Snapshots
		snapEnabled = boolVal(s.Enabled)
		snapMaxBytes = intVal(s.MaxFileBytes)
		snapDefined = true
	} else {
		snapEnabled = notSet
		snapMaxBytes = notSet
	}
	if merged.Sagittarius != nil && merged.Sagittarius.Snapshots != nil {
		s := merged.Sagittarius.Snapshots
		snapEnabledMerged = boolVal(s.Enabled)
		snapMaxBytesMerged = intVal(s.MaxFileBytes)
	} else {
		snapEnabledMerged = notSet
		snapMaxBytesMerged = notSet
	}

	// --- Verify ---
	var verifyFix, verifyFixMerged string
	var verifySuggest, verifySuggestMerged string
	var verifyDefined bool
	if scopeSettings.Sagittarius != nil && scopeSettings.Sagittarius.Verify != nil {
		v := scopeSettings.Sagittarius.Verify
		verifyFix = boolVal(v.AllowFix)
		verifySuggest = boolVal(v.SuggestAfterWrite)
		verifyDefined = true
	} else {
		verifyFix = notSet
		verifySuggest = notSet
	}
	if merged.Sagittarius != nil && merged.Sagittarius.Verify != nil {
		v := merged.Sagittarius.Verify
		verifyFixMerged = boolVal(v.AllowFix)
		verifySuggestMerged = boolVal(v.SuggestAfterWrite)
	} else {
		verifyFixMerged = notSet
		verifySuggestMerged = notSet
	}

	sagDefined := scopeSettings.Sagittarius != nil

	return []settingsdialog.SettingEntry{
		{Label: "General", Kind: settingsdialog.KindHeader},
		{
			Key:         "sagittarius.maxToolRounds",
			Label:       "Max tool rounds",
			Description: "Maximum number of tool-use rounds per turn (0 = unlimited)",
			Value:       maxRounds,
			DefinedHere: sagDefined && scopeSettings.Sagittarius.MaxToolRounds != nil,
			MergedValue: maxRoundsMerged,
			Kind:        settingsdialog.KindInt,
		},

		{Label: "UI", Kind: settingsdialog.KindHeader},
		{
			Key:         "ui.theme",
			Label:       "Theme",
			Description: "Color theme: default (purple) or greyscale",
			Value:       themeVal(scopeUI.Theme),
			DefinedHere: uiDefined && scopeUI.Theme != "",
			MergedValue: themeVal(mergedUI.Theme),
			Kind:        settingsdialog.KindEnum,
			Choices:     []string{"default", "greyscale"},
		},
		{
			Key:         "ui.showThinking",
			Label:       "Show thinking box",
			Description: "Show the reasoning/thinking box when the model supports it",
			Value:       strconv.FormatBool(scopeUI.ShowThinking),
			DefinedHere: uiDefined,
			MergedValue: strconv.FormatBool(mergedUI.ShowThinking),
			Kind:        settingsdialog.KindBool,
		},
		{
			Key:         "ui.hideBanner",
			Label:       "Hide launch banner",
			Description: "Suppress the ASCII art banner at startup",
			Value:       strconv.FormatBool(scopeUI.HideBanner),
			DefinedHere: uiDefined,
			MergedValue: strconv.FormatBool(mergedUI.HideBanner),
			Kind:        settingsdialog.KindBool,
		},

		{Label: "Security", Kind: settingsdialog.KindHeader},
		{
			Key:         "security.projectBoundary.enforce",
			Label:       "Project boundary",
			Description: "Prevent file writes and risky shell commands outside the project root",
			Value:       secEnforce,
			DefinedHere: secDefined,
			MergedValue: secEnforceMerged,
			Kind:        settingsdialog.KindBool,
		},

		{Label: "Snapshots", Kind: settingsdialog.KindHeader},
		{
			Key:         "sagittarius.snapshots.enabled",
			Label:       "Snapshots enabled",
			Description: "Capture file snapshots before write_file for /diff and /undo",
			Value:       snapEnabled,
			DefinedHere: snapDefined,
			MergedValue: snapEnabledMerged,
			Kind:        settingsdialog.KindBool,
		},
		{
			Key:         "sagittarius.snapshots.maxFileBytes",
			Label:       "Snapshot max file size",
			Description: "Maximum file size to snapshot (bytes; 0 = no limit)",
			Value:       snapMaxBytes,
			DefinedHere: snapDefined,
			MergedValue: snapMaxBytesMerged,
			Kind:        settingsdialog.KindInt,
		},

		{Label: "Verify", Kind: settingsdialog.KindHeader},
		{
			Key:         "sagittarius.verify.allowFix",
			Label:       "Allow fix mode",
			Description: "Allow run_project_checks to apply auto-fixes (mutates files)",
			Value:       verifyFix,
			DefinedHere: verifyDefined,
			MergedValue: verifyFixMerged,
			Kind:        settingsdialog.KindBool,
		},
		{
			Key:         "sagittarius.verify.suggestAfterWrite",
			Label:       "Suggest verify after write",
			Description: "Emit a one-line hint to run checks after write_file edits",
			Value:       verifySuggest,
			DefinedHere: verifyDefined,
			MergedValue: verifySuggestMerged,
			Kind:        settingsdialog.KindBool,
		},
	}
}

// SetValue persists a single setting key to the given scope.
func (d *settingsDialogDeps) SetValue(ctx context.Context, scope config.SettingScope, key, value string) error {
	docs := d.docs()
	if docs == nil {
		return fmt.Errorf("settings not loaded")
	}
	target := docs.TargetSettings(scope)
	if err := applySettingValue(target, key, value); err != nil {
		return err
	}
	if err := docs.Save(scope); err != nil {
		return err
	}
	// Rebuild so UI changes (theme, showThinking) take effect immediately.
	_, _, _ = d.app.deps.Hooks.RebuildRunner(ctx)
	return nil
}

// ClearValue removes a setting from the given scope's file.
func (d *settingsDialogDeps) ClearValue(ctx context.Context, scope config.SettingScope, key string) error {
	docs := d.docs()
	if docs == nil {
		return fmt.Errorf("settings not loaded")
	}
	target := docs.TargetSettings(scope)
	if err := clearSettingValue(target, key); err != nil {
		return err
	}
	if err := docs.Save(scope); err != nil {
		return err
	}
	_, _, _ = d.app.deps.Hooks.RebuildRunner(ctx)
	return nil
}

// applySettingValue mutates settings for the given dotted key and string value.
func applySettingValue(s *config.Settings, key, value string) error {
	if s == nil {
		return fmt.Errorf("settings not initialized")
	}
	switch key {
	case "sagittarius.maxToolRounds":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("maxToolRounds must be an integer: %w", err)
		}
		if s.Sagittarius == nil {
			s.Sagittarius = &config.SagittariusSettings{}
		}
		s.Sagittarius.MaxToolRounds = &n
	case "ui.theme":
		if err := s.SetUITheme(value); err != nil {
			return err
		}
	case "ui.showThinking":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("showThinking must be true/false: %w", err)
		}
		if err := s.SetUIShowThinking(b); err != nil {
			return err
		}
	case "ui.hideBanner":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("hideBanner must be true/false: %w", err)
		}
		return setUIBoolField(s, "hideBanner", b)
	case "security.projectBoundary.enforce":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("enforce must be true/false: %w", err)
		}
		if s.Security == nil {
			s.Security = &config.SecuritySettings{}
		}
		if s.Security.ProjectBoundary == nil {
			s.Security.ProjectBoundary = &config.ProjectBoundaryConfig{}
		}
		s.Security.ProjectBoundary.Enforce = &b
	case "sagittarius.snapshots.enabled":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("enabled must be true/false: %w", err)
		}
		if s.Sagittarius == nil {
			s.Sagittarius = &config.SagittariusSettings{}
		}
		if s.Sagittarius.Snapshots == nil {
			s.Sagittarius.Snapshots = &config.SagittariusSnapshotConfig{}
		}
		s.Sagittarius.Snapshots.Enabled = &b
	case "sagittarius.snapshots.maxFileBytes":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("maxFileBytes must be an integer: %w", err)
		}
		if s.Sagittarius == nil {
			s.Sagittarius = &config.SagittariusSettings{}
		}
		if s.Sagittarius.Snapshots == nil {
			s.Sagittarius.Snapshots = &config.SagittariusSnapshotConfig{}
		}
		s.Sagittarius.Snapshots.MaxFileBytes = &n
	case "sagittarius.verify.allowFix":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("allowFix must be true/false: %w", err)
		}
		if s.Sagittarius == nil {
			s.Sagittarius = &config.SagittariusSettings{}
		}
		if s.Sagittarius.Verify == nil {
			s.Sagittarius.Verify = &config.SagittariusVerifyConfig{}
		}
		s.Sagittarius.Verify.AllowFix = &b
	case "sagittarius.verify.suggestAfterWrite":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("suggestAfterWrite must be true/false: %w", err)
		}
		if s.Sagittarius == nil {
			s.Sagittarius = &config.SagittariusSettings{}
		}
		if s.Sagittarius.Verify == nil {
			s.Sagittarius.Verify = &config.SagittariusVerifyConfig{}
		}
		s.Sagittarius.Verify.SuggestAfterWrite = &b
	default:
		return fmt.Errorf("unknown setting key %q", key)
	}
	return nil
}

// setUIBoolField mutates a named bool field inside the raw "ui" JSON object.
func setUIBoolField(s *config.Settings, field string, value bool) error {
	if s.Raw == nil {
		s.Raw = make(map[string]json.RawMessage)
	}
	uiMap := make(map[string]json.RawMessage)
	if raw, ok := s.Raw["ui"]; ok {
		_ = json.Unmarshal(raw, &uiMap)
	}
	b, _ := json.Marshal(value)
	uiMap[field] = b
	out, err := json.Marshal(uiMap)
	if err != nil {
		return err
	}
	s.Raw["ui"] = out
	return nil
}

// clearSettingValue removes a single dotted-key from the settings in memory.
func clearSettingValue(s *config.Settings, key string) error {
	if s == nil {
		return nil
	}
	switch key {
	case "sagittarius.maxToolRounds":
		if s.Sagittarius != nil {
			s.Sagittarius.MaxToolRounds = nil
		}
	case "ui.theme":
		if err := s.SetUITheme(""); err != nil {
			return err
		}
	case "ui.showThinking":
		if err := s.SetUIShowThinking(false); err != nil {
			return err
		}
	case "ui.hideBanner":
		return setUIBoolField(s, "hideBanner", false)
	case "security.projectBoundary.enforce":
		if s.Security != nil && s.Security.ProjectBoundary != nil {
			s.Security.ProjectBoundary.Enforce = nil
		}
	case "sagittarius.snapshots.enabled":
		if s.Sagittarius != nil && s.Sagittarius.Snapshots != nil {
			s.Sagittarius.Snapshots.Enabled = nil
		}
	case "sagittarius.snapshots.maxFileBytes":
		if s.Sagittarius != nil && s.Sagittarius.Snapshots != nil {
			s.Sagittarius.Snapshots.MaxFileBytes = nil
		}
	case "sagittarius.verify.allowFix":
		if s.Sagittarius != nil && s.Sagittarius.Verify != nil {
			s.Sagittarius.Verify.AllowFix = nil
		}
	case "sagittarius.verify.suggestAfterWrite":
		if s.Sagittarius != nil && s.Sagittarius.Verify != nil {
			s.Sagittarius.Verify.SuggestAfterWrite = nil
		}
	default:
		return fmt.Errorf("unknown setting key %q", key)
	}
	return nil
}
