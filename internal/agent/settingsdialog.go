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
	strVal := func(p *string) string {
		if p == nil {
			return notSet
		}
		return *p
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
	var verifyAutoCheck, verifyAutoCheckMerged string
	var verifyModuleWide, verifyModuleWideMerged string
	var verifyTimeout, verifyTimeoutMerged string
	var verifyRepoLocal, verifyRepoLocalMerged string
	var verifyEditLoop, verifyEditLoopMerged string
	var verifyDefined bool
	if scopeSettings.Sagittarius != nil && scopeSettings.Sagittarius.Verify != nil {
		v := scopeSettings.Sagittarius.Verify
		verifyFix = boolVal(v.AllowFix)
		verifySuggest = boolVal(v.SuggestAfterWrite)
		verifyAutoCheck = boolVal(v.AutoCheckAfterWrite)
		verifyModuleWide = boolVal(v.AutoCheckModuleWide)
		verifyTimeout = intVal(v.AutoCheckTimeoutSeconds)
		verifyRepoLocal = strVal(v.RepoLocalTools)
		verifyEditLoop = intVal(v.EditLoopThreshold)
		verifyDefined = true
	} else {
		verifyFix = notSet
		verifySuggest = notSet
		verifyAutoCheck = notSet
		verifyModuleWide = notSet
		verifyTimeout = notSet
		verifyRepoLocal = notSet
		verifyEditLoop = notSet
	}
	if merged.Sagittarius != nil && merged.Sagittarius.Verify != nil {
		v := merged.Sagittarius.Verify
		verifyFixMerged = boolVal(v.AllowFix)
		verifySuggestMerged = boolVal(v.SuggestAfterWrite)
		verifyAutoCheckMerged = boolVal(v.AutoCheckAfterWrite)
		verifyModuleWideMerged = boolVal(v.AutoCheckModuleWide)
		verifyTimeoutMerged = intVal(v.AutoCheckTimeoutSeconds)
		verifyRepoLocalMerged = strVal(v.RepoLocalTools)
		verifyEditLoopMerged = intVal(v.EditLoopThreshold)
	} else {
		verifyFixMerged = notSet
		verifySuggestMerged = notSet
		verifyAutoCheckMerged = notSet
		verifyModuleWideMerged = notSet
		verifyTimeoutMerged = notSet
		verifyRepoLocalMerged = notSet
		verifyEditLoopMerged = notSet
	}
	// --- Subagents ---
	var subEnabled, subEnabledMerged string
	var subDefined bool
	if scopeSettings.Sagittarius != nil && scopeSettings.Sagittarius.Subagents != nil {
		s := scopeSettings.Sagittarius.Subagents
		subEnabled = boolVal(s.Enabled)
		subDefined = true
	} else {
		subEnabled = notSet
	}
	if merged.Sagittarius != nil && merged.Sagittarius.Subagents != nil {
		s := merged.Sagittarius.Subagents
		subEnabledMerged = boolVal(s.Enabled)
	} else {
		subEnabledMerged = notSet
	}

	// --- Sessions ---
	var sessAutoTitle, sessAutoTitleMerged string
	var sessDefined bool
	if scopeSettings.Sagittarius != nil && scopeSettings.Sagittarius.Sessions != nil {
		s := scopeSettings.Sagittarius.Sessions
		sessAutoTitle = strVal(s.AutoTitle)
		sessDefined = true
	} else {
		sessAutoTitle = notSet
	}
	if merged.Sagittarius != nil && merged.Sagittarius.Sessions != nil {
		s := merged.Sagittarius.Sessions
		sessAutoTitleMerged = strVal(s.AutoTitle)
	} else {
		sessAutoTitleMerged = notSet
	}

	// --- Edit ---
	var editEnabled, editEnabledMerged string
	var editDefined bool
	if scopeSettings.Sagittarius != nil && scopeSettings.Sagittarius.Edit != nil {
		s := scopeSettings.Sagittarius.Edit
		editEnabled = boolVal(s.Enabled)
		editDefined = true
	} else {
		editEnabled = notSet
	}
	if merged.Sagittarius != nil && merged.Sagittarius.Edit != nil {
		s := merged.Sagittarius.Edit
		editEnabledMerged = boolVal(s.Enabled)
	} else {
		editEnabledMerged = notSet
	}

	// --- Symbols ---
	var symEnabled, symEnabledMerged string
	var symGopls, symGoplsMerged string
	var symDefined bool
	if scopeSettings.Sagittarius != nil && scopeSettings.Sagittarius.Symbols != nil {
		s := scopeSettings.Sagittarius.Symbols
		symEnabled = boolVal(s.Enabled)
		symGopls = boolVal(s.PreferGopls)
		symDefined = true
	} else {
		symEnabled = notSet
		symGopls = notSet
	}
	if merged.Sagittarius != nil && merged.Sagittarius.Symbols != nil {
		s := merged.Sagittarius.Symbols
		symEnabledMerged = boolVal(s.Enabled)
		symGoplsMerged = boolVal(s.PreferGopls)
	} else {
		symEnabledMerged = notSet
		symGoplsMerged = notSet
	}

	sagDefined := scopeSettings.Sagittarius != nil

	modeVal := func(m string) string {
		if m == "" {
			return notSet
		}
		return m
	}
	var scopeDefaultMode, mergedDefaultMode string
	if sagDefined {
		scopeDefaultMode = scopeSettings.Sagittarius.DefaultMode
	}
	if merged.Sagittarius != nil {
		mergedDefaultMode = merged.Sagittarius.DefaultMode
	}

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
		{
			Key:         "sagittarius.defaultMode",
			Label:       "Default interaction mode",
			Description: "Interaction mode a new session starts in (compiled-in fallback is agent)",
			Value:       modeVal(scopeDefaultMode),
			DefinedHere: sagDefined && scopeDefaultMode != "",
			MergedValue: modeVal(mergedDefaultMode),
			Kind:        settingsdialog.KindEnum,
			Choices:     []string{"agent", "plan", "ask", "debug"},
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
		{
			Key:         "ui.toolkitChecklistDismissed",
			Label:       "Dismiss toolkit checklist",
			Description: "Never show the host toolkit checklist on startup",
			Value:       strconv.FormatBool(scopeUI.ToolkitChecklistDismissed),
			DefinedHere: uiDefined,
			MergedValue: strconv.FormatBool(mergedUI.ToolkitChecklistDismissed),
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
		{
			Key:         "sagittarius.verify.autoCheckAfterWrite",
			Label:       "Auto-check after write",
			Description: "Automatically run read-only lint/format checks on files after edits",
			Value:       verifyAutoCheck,
			DefinedHere: verifyDefined,
			MergedValue: verifyAutoCheckMerged,
			Kind:        settingsdialog.KindBool,
		},
		{
			Key:         "sagittarius.verify.autoCheckModuleWide",
			Label:       "Auto-check module-wide",
			Description: "Include whole-module checks (vet, tsc) in automatic post-write checks",
			Value:       verifyModuleWide,
			DefinedHere: verifyDefined,
			MergedValue: verifyModuleWideMerged,
			Kind:        settingsdialog.KindBool,
		},
		{
			Key:         "sagittarius.verify.autoCheckTimeoutSeconds",
			Label:       "Auto-check timeout (sec)",
			Description: "Maximum time allowed for post-write checks before aborting",
			Value:       verifyTimeout,
			DefinedHere: verifyDefined,
			MergedValue: verifyTimeoutMerged,
			Kind:        settingsdialog.KindInt,
		},
		{
			Key:         "sagittarius.verify.repoLocalTools",
			Label:       "Repo-local tools policy",
			Description: "Policy for running repo-local linters (e.g. node_modules/.bin)",
			Value:       verifyRepoLocal,
			DefinedHere: verifyDefined,
			MergedValue: verifyRepoLocalMerged,
			Kind:        settingsdialog.KindEnum,
			Choices:     []string{"prompt", "allow", "deny"},
		},
		{
			Key:         "sagittarius.verify.editLoopThreshold",
			Label:       "Edit loop threshold",
			Description: "Number of failing edits to a single file before triggering a stop-and-re-evaluate nudge (0 to disable)",
			Value:       verifyEditLoop,
			DefinedHere: verifyDefined,
			MergedValue: verifyEditLoopMerged,
			Kind:        settingsdialog.KindInt,
		},

		{Label: "Subagents", Kind: settingsdialog.KindHeader},
		{
			Key:         "sagittarius.subagents.enabled",
			Label:       "Research subagents (task)",
			Description: "Enable the task tool for launching read-only context-isolated research subagents (default off)",
			Value:       subEnabled,
			DefinedHere: subDefined,
			MergedValue: subEnabledMerged,
			Kind:        settingsdialog.KindBool,
		},
		{Label: "Sessions", Kind: settingsdialog.KindHeader},
		{
			Key:         "sagittarius.sessions.autoTitle",
			Label:       "Auto-title sessions",
			Description: "Title the conversation after the first exchange: prompt (confirm), auto (silent), or off",
			Value:       sessAutoTitle,
			DefinedHere: sessDefined,
			MergedValue: sessAutoTitleMerged,
			Kind:        settingsdialog.KindEnum,
			Choices:     []string{"prompt", "auto", "off"},
		},
		{Label: "Edit Tool", Kind: settingsdialog.KindHeader},
		{
			Key:         "sagittarius.edit.enabled",
			Label:       "Edit file (edit)",
			Description: "Register the edit tool (default on; off to fall back to full write_file only)",
			Value:       editEnabled,
			DefinedHere: editDefined,
			MergedValue: editEnabledMerged,
			Kind:        settingsdialog.KindBool,
		},
		{Label: "Symbols", Kind: settingsdialog.KindHeader},
		{
			Key:         "sagittarius.symbols.enabled",
			Label:       "Symbol navigation (find_symbol)",
			Description: "Register the find_symbol code-navigation tool (default on; off to use an external MCP)",
			Value:       symEnabled,
			DefinedHere: symDefined,
			MergedValue: symEnabledMerged,
			Kind:        settingsdialog.KindBool,
		},
		{
			Key:         "sagittarius.symbols.preferGopls",
			Label:       "Prefer gopls for Go",
			Description: "Note gopls MCP tools in find_symbol's description on Go modules (prompt-only)",
			Value:       symGopls,
			DefinedHere: symDefined,
			MergedValue: symGoplsMerged,
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
	case "sagittarius.defaultMode":
		switch value {
		case "agent", "plan", "ask", "debug":
		default:
			return fmt.Errorf("defaultMode must be 'agent', 'plan', 'ask', or 'debug'")
		}
		if s.Sagittarius == nil {
			s.Sagittarius = &config.SagittariusSettings{}
		}
		s.Sagittarius.DefaultMode = value
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
	case "ui.toolkitChecklistDismissed":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("toolkitChecklistDismissed must be true/false: %w", err)
		}
		return s.SetUIToolkitChecklistDismissed(b)
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
	case "sagittarius.verify.autoCheckAfterWrite":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("autoCheckAfterWrite must be true/false: %w", err)
		}
		if s.Sagittarius == nil {
			s.Sagittarius = &config.SagittariusSettings{}
		}
		if s.Sagittarius.Verify == nil {
			s.Sagittarius.Verify = &config.SagittariusVerifyConfig{}
		}
		s.Sagittarius.Verify.AutoCheckAfterWrite = &b
	case "sagittarius.verify.autoCheckModuleWide":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("autoCheckModuleWide must be true/false: %w", err)
		}
		if s.Sagittarius == nil {
			s.Sagittarius = &config.SagittariusSettings{}
		}
		if s.Sagittarius.Verify == nil {
			s.Sagittarius.Verify = &config.SagittariusVerifyConfig{}
		}
		s.Sagittarius.Verify.AutoCheckModuleWide = &b
	case "sagittarius.verify.autoCheckTimeoutSeconds":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("autoCheckTimeoutSeconds must be an integer: %w", err)
		}
		if s.Sagittarius == nil {
			s.Sagittarius = &config.SagittariusSettings{}
		}
		if s.Sagittarius.Verify == nil {
			s.Sagittarius.Verify = &config.SagittariusVerifyConfig{}
		}
		s.Sagittarius.Verify.AutoCheckTimeoutSeconds = &n
	case "sagittarius.verify.repoLocalTools":
		if value != "prompt" && value != "allow" && value != "deny" {
			return fmt.Errorf("repoLocalTools must be 'prompt', 'allow', or 'deny'")
		}
		if s.Sagittarius == nil {
			s.Sagittarius = &config.SagittariusSettings{}
		}
		if s.Sagittarius.Verify == nil {
			s.Sagittarius.Verify = &config.SagittariusVerifyConfig{}
		}
		s.Sagittarius.Verify.RepoLocalTools = &value
	case "sagittarius.sessions.autoTitle":
		if value != "prompt" && value != "auto" && value != "off" {
			return fmt.Errorf("autoTitle must be 'prompt', 'auto', or 'off'")
		}
		if s.Sagittarius == nil {
			s.Sagittarius = &config.SagittariusSettings{}
		}
		if s.Sagittarius.Sessions == nil {
			s.Sagittarius.Sessions = &config.SagittariusSessionsConfig{}
		}
		s.Sagittarius.Sessions.AutoTitle = &value
	case "sagittarius.verify.editLoopThreshold":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("editLoopThreshold must be an integer: %w", err)
		}
		if s.Sagittarius == nil {
			s.Sagittarius = &config.SagittariusSettings{}
		}
		if s.Sagittarius.Verify == nil {
			s.Sagittarius.Verify = &config.SagittariusVerifyConfig{}
		}
		s.Sagittarius.Verify.EditLoopThreshold = &n
	case "sagittarius.subagents.enabled":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("enabled must be true/false: %w", err)
		}
		if s.Sagittarius == nil {
			s.Sagittarius = &config.SagittariusSettings{}
		}
		if s.Sagittarius.Subagents == nil {
			s.Sagittarius.Subagents = &config.SagittariusSubagents{}
		}
		s.Sagittarius.Subagents.Enabled = &b
	case "sagittarius.edit.enabled":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("enabled must be true/false: %w", err)
		}
		if s.Sagittarius == nil {
			s.Sagittarius = &config.SagittariusSettings{}
		}
		if s.Sagittarius.Edit == nil {
			s.Sagittarius.Edit = &config.SagittariusEditConfig{}
		}
		s.Sagittarius.Edit.Enabled = &b
	case "sagittarius.symbols.enabled":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("enabled must be true/false: %w", err)
		}
		if s.Sagittarius == nil {
			s.Sagittarius = &config.SagittariusSettings{}
		}
		if s.Sagittarius.Symbols == nil {
			s.Sagittarius.Symbols = &config.SagittariusSymbolsConfig{}
		}
		s.Sagittarius.Symbols.Enabled = &b
	case "sagittarius.symbols.preferGopls":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("preferGopls must be true/false: %w", err)
		}
		if s.Sagittarius == nil {
			s.Sagittarius = &config.SagittariusSettings{}
		}
		if s.Sagittarius.Symbols == nil {
			s.Sagittarius.Symbols = &config.SagittariusSymbolsConfig{}
		}
		s.Sagittarius.Symbols.PreferGopls = &b
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
	case "sagittarius.defaultMode":
		if s.Sagittarius != nil {
			s.Sagittarius.DefaultMode = ""
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
	case "ui.toolkitChecklistDismissed":
		return s.SetUIToolkitChecklistDismissed(false)
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
	case "sagittarius.verify.autoCheckAfterWrite":
		if s.Sagittarius != nil && s.Sagittarius.Verify != nil {
			s.Sagittarius.Verify.AutoCheckAfterWrite = nil
		}
	case "sagittarius.verify.autoCheckModuleWide":
		if s.Sagittarius != nil && s.Sagittarius.Verify != nil {
			s.Sagittarius.Verify.AutoCheckModuleWide = nil
		}
	case "sagittarius.verify.autoCheckTimeoutSeconds":
		if s.Sagittarius != nil && s.Sagittarius.Verify != nil {
			s.Sagittarius.Verify.AutoCheckTimeoutSeconds = nil
		}
	case "sagittarius.verify.repoLocalTools":
		if s.Sagittarius != nil && s.Sagittarius.Verify != nil {
			s.Sagittarius.Verify.RepoLocalTools = nil
		}
	case "sagittarius.subagents.enabled":
		if s.Sagittarius != nil && s.Sagittarius.Subagents != nil {
			s.Sagittarius.Subagents.Enabled = nil
		}
	case "sagittarius.edit.enabled":
		if s.Sagittarius != nil && s.Sagittarius.Edit != nil {
			s.Sagittarius.Edit.Enabled = nil
		}
	case "sagittarius.sessions.autoTitle":
		if s.Sagittarius != nil && s.Sagittarius.Sessions != nil {
			s.Sagittarius.Sessions.AutoTitle = nil
		}
	case "sagittarius.verify.editLoopThreshold":
		if s.Sagittarius != nil && s.Sagittarius.Verify != nil {
			s.Sagittarius.Verify.EditLoopThreshold = nil
		}
	case "sagittarius.symbols.enabled":
		if s.Sagittarius != nil && s.Sagittarius.Symbols != nil {
			s.Sagittarius.Symbols.Enabled = nil
		}
	case "sagittarius.symbols.preferGopls":
		if s.Sagittarius != nil && s.Sagittarius.Symbols != nil {
			s.Sagittarius.Symbols.PreferGopls = nil
		}
	default:
		return fmt.Errorf("unknown setting key %q", key)
	}
	return nil
}
