package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/goal"
	"github.com/undeadindustries/sagittarius/internal/modes"
	"github.com/undeadindustries/sagittarius/internal/tools"
	"github.com/undeadindustries/sagittarius/internal/ui/settingsdialog"
)

const defaultGoalMaxTurns = 25

// SettingsDialogDeps returns the side-effect adapter the /settings browser uses.
func (a *App) SettingsDialogDeps() settingsdialog.Deps {
	return &settingsDialogDeps{baseDialogDeps{app: a}}
}

type settingsDialogDeps struct{ baseDialogDeps }

func (d *settingsDialogDeps) docs() *config.Documents { return d.app.docs }

// ListSettings returns the curated settings list with values drawn from the
// given scope (not merged). DefinedHere is true when the key exists in that
// scope's file; MergedValue is the other-scope raw value when inherited.
func (d *settingsDialogDeps) ListSettings(scope config.SettingScope) []settingsdialog.SettingEntry {
	return listSettings(d.docs(), scope)
}

func listSettings(docs *config.Documents, scope config.SettingScope) []settingsdialog.SettingEntry {
	if docs == nil {
		return nil
	}
	scopeSettings := docs.TargetSettings(scope)
	global := docs.Global
	project := docs.Project

	row := func(e settingsdialog.SettingEntry, scopePresent, globalPresent, projectPresent string) settingsdialog.SettingEntry {
		e.DefinedHere = docs.IsDefined(scope, e.Key)
		if e.DefinedHere {
			e.Value = scopePresent
		}
		switch {
		case docs.IsDefined(config.ScopeProject, e.Key):
			e.MergedValue = projectPresent
		case docs.IsDefined(config.ScopeGlobal, e.Key):
			e.MergedValue = globalPresent
		}
		e.Inherited = e.Value == "" && e.MergedValue != ""
		return e
	}

	if global == nil {
		global = &config.Settings{}
	}

	return []settingsdialog.SettingEntry{
		{Label: "General", Kind: settingsdialog.KindHeader},
		row(settingsdialog.SettingEntry{
			Key:          "sagittarius.maxToolRounds",
			Label:        "Max tool rounds",
			Description:  "Maximum number of tool-use rounds per turn (0 = unlimited)",
			DefaultValue: strconv.Itoa(config.ResolveMaxToolRounds(nil, tools.MaxToolRounds)),
			Kind:         settingsdialog.KindInt,
		}, sagMaxRounds(scopeSettings), sagMaxRounds(global), sagMaxRounds(project)),
		row(settingsdialog.SettingEntry{
			Key:          "sagittarius.contextLimitPreferDiscovered",
			Label:        "Prefer discovered context limits",
			Description:  "Ignore manual pins and use API-reported limits (context_length)",
			DefaultValue: fmtBool(config.ResolveContextLimitPreferDiscovered(nil)),
			Kind:         settingsdialog.KindBool,
		}, sagPreferDiscovered(scopeSettings), sagPreferDiscovered(global), sagPreferDiscovered(project)),
		row(settingsdialog.SettingEntry{
			Key:          "sagittarius.defaultMode",
			Label:        "Default interaction mode",
			Description:  "Interaction mode a new session starts in (compiled-in fallback is agent)",
			DefaultValue: modes.DefaultFromSettings(nil).String(),
			Kind:         settingsdialog.KindEnum,
			Choices:      []string{"agent", "plan", "ask", "debug"},
		}, sagDefaultMode(scopeSettings), sagDefaultMode(global), sagDefaultMode(project)),

		{Label: "UI", Kind: settingsdialog.KindHeader},
		row(settingsdialog.SettingEntry{
			Key:          "ui.theme",
			Label:        "Theme",
			Description:  "Color theme: default (purple) or greyscale",
			DefaultValue: "default",
			Kind:         settingsdialog.KindEnum,
			Choices:      []string{"default", "greyscale"},
		}, scopeSettings.UI().Theme, global.UI().Theme, projectUITheme(project)),
		row(settingsdialog.SettingEntry{
			Key:          "ui.showThinking",
			Label:        "Show thinking box",
			Description:  "Show the reasoning/thinking box when the model supports it",
			DefaultValue: fmtBool(config.ResolveShowThinking(nil, "", "")),
			Kind:         settingsdialog.KindBool,
		}, uiBool(scopeSettings, "showThinking"), uiBool(global, "showThinking"), uiBool(project, "showThinking")),
		row(settingsdialog.SettingEntry{
			Key:          "ui.hideBanner",
			Label:        "Hide launch banner",
			Description:  "Suppress the ASCII art banner at startup",
			DefaultValue: "false",
			Kind:         settingsdialog.KindBool,
		}, uiBool(scopeSettings, "hideBanner"), uiBool(global, "hideBanner"), uiBool(project, "hideBanner")),
		row(settingsdialog.SettingEntry{
			Key:          "ui.toolkitChecklistDismissed",
			Label:        "Dismiss toolkit checklist",
			Description:  "Never show the host toolkit checklist on startup",
			DefaultValue: "false",
			Kind:         settingsdialog.KindBool,
		}, uiBool(scopeSettings, "toolkitChecklistDismissed"), uiBool(global, "toolkitChecklistDismissed"), uiBool(project, "toolkitChecklistDismissed")),
		row(settingsdialog.SettingEntry{
			Key:          "ui.escapeAtOnPaste",
			Label:        "Escape @ on paste",
			Description:  "Auto-escape @ to \\@ when pasting into composer to prevent unwanted file expansion",
			DefaultValue: "false",
			Kind:         settingsdialog.KindBool,
		}, uiBool(scopeSettings, "escapeAtOnPaste"), uiBool(global, "escapeAtOnPaste"), uiBool(project, "escapeAtOnPaste")),

		{Label: "Security", Kind: settingsdialog.KindHeader},
		row(settingsdialog.SettingEntry{
			Key:          "security.projectBoundary.enforce",
			Label:        "Project boundary",
			Description:  "Prevent file writes and risky shell commands outside the project root",
			DefaultValue: fmtBool(config.ProjectBoundaryEnforced(nil, nil)),
			Kind:         settingsdialog.KindBool,
		}, boundaryEnforce(scopeSettings), boundaryEnforce(global), boundaryEnforce(project)),

		{Label: "Snapshots", Kind: settingsdialog.KindHeader},
		row(settingsdialog.SettingEntry{
			Key:          "sagittarius.snapshots.enabled",
			Label:        "Snapshots enabled",
			Description:  "Capture file snapshots before write_file for /diff and /undo",
			DefaultValue: fmtBool(config.SnapshotsEnabled(nil, nil)),
			Kind:         settingsdialog.KindBool,
		}, snapEnabled(scopeSettings), snapEnabled(global), snapEnabled(project)),
		row(settingsdialog.SettingEntry{
			Key:          "sagittarius.snapshots.maxFileBytes",
			Label:        "Snapshot max file size",
			Description:  "Maximum file size to snapshot (bytes; 0 = no limit)",
			DefaultValue: strconv.Itoa(config.SnapshotMaxFileBytes(nil, nil)),
			Kind:         settingsdialog.KindInt,
		}, snapMaxBytes(scopeSettings), snapMaxBytes(global), snapMaxBytes(project)),

		{Label: "Verify", Kind: settingsdialog.KindHeader},
		row(settingsdialog.SettingEntry{
			Key:          "sagittarius.verify.allowFix",
			Label:        "Allow fix mode",
			Description:  "Allow run_project_checks to apply auto-fixes (mutates files)",
			DefaultValue: fmtBool(config.VerifyAllowFix(nil, nil)),
			Kind:         settingsdialog.KindBool,
		}, verifyAllowFix(scopeSettings), verifyAllowFix(global), verifyAllowFix(project)),
		row(settingsdialog.SettingEntry{
			Key:          "sagittarius.verify.suggestAfterWrite",
			Label:        "Suggest verify after write",
			Description:  "Emit a one-line hint to run checks after write_file edits",
			DefaultValue: fmtBool(config.VerifySuggestAfterWrite(nil, nil)),
			Kind:         settingsdialog.KindBool,
		}, verifySuggest(scopeSettings), verifySuggest(global), verifySuggest(project)),
		row(settingsdialog.SettingEntry{
			Key:          "sagittarius.verify.autoCheckAfterWrite",
			Label:        "Auto-check after write",
			Description:  "Automatically run read-only lint/format checks on files after edits",
			DefaultValue: fmtBool(config.VerifyAutoCheckAfterWrite(nil, nil)),
			Kind:         settingsdialog.KindBool,
		}, verifyAutoCheck(scopeSettings), verifyAutoCheck(global), verifyAutoCheck(project)),
		row(settingsdialog.SettingEntry{
			Key:          "sagittarius.verify.autoCheckModuleWide",
			Label:        "Auto-check module-wide",
			Description:  "Include whole-module checks (vet, tsc) in automatic post-write checks",
			DefaultValue: fmtBool(config.VerifyAutoCheckModuleWide(nil, nil)),
			Kind:         settingsdialog.KindBool,
		}, verifyModuleWide(scopeSettings), verifyModuleWide(global), verifyModuleWide(project)),
		row(settingsdialog.SettingEntry{
			Key:          "sagittarius.verify.autoCheckTimeoutSeconds",
			Label:        "Auto-check timeout (sec)",
			Description:  "Maximum time allowed for post-write checks before aborting",
			DefaultValue: strconv.Itoa(config.VerifyAutoCheckTimeoutSeconds(nil, nil)),
			Kind:         settingsdialog.KindInt,
		}, verifyTimeout(scopeSettings), verifyTimeout(global), verifyTimeout(project)),
		row(settingsdialog.SettingEntry{
			Key:          "sagittarius.verify.repoLocalTools",
			Label:        "Repo-local tools policy",
			Description:  "Policy for running repo-local linters (e.g. node_modules/.bin)",
			DefaultValue: string(config.VerifyRepoLocalTools(nil, nil)),
			Kind:         settingsdialog.KindEnum,
			Choices:      []string{"prompt", "allow", "deny"},
		}, verifyRepoLocal(scopeSettings), verifyRepoLocal(global), verifyRepoLocal(project)),
		row(settingsdialog.SettingEntry{
			Key:          "sagittarius.verify.editLoopThreshold",
			Label:        "Edit loop threshold",
			Description:  "Number of failing edits to a single file before triggering a stop-and-re-evaluate nudge (0 to disable)",
			DefaultValue: strconv.Itoa(config.VerifyEditLoopThreshold(nil, nil)),
			Kind:         settingsdialog.KindInt,
		}, verifyEditLoop(scopeSettings), verifyEditLoop(global), verifyEditLoop(project)),

		{Label: "Goal", Kind: settingsdialog.KindHeader},
		row(settingsdialog.SettingEntry{
			Key:          "sagittarius.goal.maxTurns",
			Label:        "Max goal turns",
			Description:  "Cap on autonomous /goal loop iterations (default 25)",
			DefaultValue: strconv.Itoa(defaultGoalMaxTurns),
			Kind:         settingsdialog.KindInt,
		}, goalMaxTurns(scopeSettings), goalMaxTurns(global), goalMaxTurns(project)),
		row(settingsdialog.SettingEntry{
			Key:         "sagittarius.goal.evaluatorProvider",
			Label:       "Evaluator provider",
			Description: "Provider for the /goal judge. Prefer a different family from the worker — same-family models share self-approval. Empty uses the worker.",
			Kind:        settingsdialog.KindString,
		}, goalEvalProvider(scopeSettings), goalEvalProvider(global), goalEvalProvider(project)),
		row(settingsdialog.SettingEntry{
			Key:         "sagittarius.goal.evaluatorModel",
			Label:       "Evaluator model",
			Description: "Model that judges goal completion. Prefer a different family from your worker — it catches self-approval a same-family model shares. Stronger helps; empty means the worker grades its own work.",
			Kind:        settingsdialog.KindString,
		}, goalEvalModel(scopeSettings), goalEvalModel(global), goalEvalModel(project)),
		row(settingsdialog.SettingEntry{
			Key:          "sagittarius.goal.evaluatorTimeout",
			Label:        "Evaluator timeout (sec)",
			Description:  "Cap on the judge's tool loop (default 120)",
			DefaultValue: strconv.Itoa(goal.DefaultEvaluatorTimeoutSeconds),
			Kind:         settingsdialog.KindInt,
		}, goalEvalTimeout(scopeSettings), goalEvalTimeout(global), goalEvalTimeout(project)),

		{Label: "Subagents", Kind: settingsdialog.KindHeader},
		row(settingsdialog.SettingEntry{
			Key:          "sagittarius.subagents.research.enabled",
			Label:        "Research subagents (task)",
			Description:  "Enable the task tool for launching read-only context-isolated research subagents (default off)",
			DefaultValue: fmtBool(config.ResearchSubagentsEnabled(nil, nil)),
			Kind:         settingsdialog.KindBool,
		}, subResearchEnabled(scopeSettings), subResearchEnabled(global), subResearchEnabled(project)),
		row(settingsdialog.SettingEntry{
			Key:          "sagittarius.subagents.coding.enabled",
			Label:        "Coding subagents (code_task)",
			Description:  "Enable the code_task tool for launching write-capable subagents. Each declares the paths it may write and siblings run in parallel, so a wrong lease is a wrong edit (default off)",
			DefaultValue: fmtBool(config.CodingSubagentsEnabled(nil, nil)),
			Kind:         settingsdialog.KindBool,
		}, subCodingEnabled(scopeSettings), subCodingEnabled(global), subCodingEnabled(project)),
		row(settingsdialog.SettingEntry{
			Key:          "sagittarius.subagents.enabled",
			Label:        "Subagents (deprecated alias)",
			Description:  "Old single switch. It now only stands in for research subagents when the research row above is unset, and never enables coding subagents",
			DefaultValue: fmtBool(false),
			Kind:         settingsdialog.KindBool,
		}, subEnabled(scopeSettings), subEnabled(global), subEnabled(project)),
		row(settingsdialog.SettingEntry{
			Key:          "sagittarius.scriptToolEnabled",
			Label:        "Script collapsing (run_script)",
			Description:  "Enable run_script to batch read-only tools in one turn (default off)",
			DefaultValue: fmtBool(config.ScriptToolEnabled(nil, nil)),
			Kind:         settingsdialog.KindBool,
		}, scriptEnabled(scopeSettings), scriptEnabled(global), scriptEnabled(project)),
		{Label: "Sessions", Kind: settingsdialog.KindHeader},
		row(settingsdialog.SettingEntry{
			Key:          "sagittarius.sessions.autoTitle",
			Label:        "Auto-title sessions",
			Description:  "Title the conversation after the first exchange: prompt (confirm), auto (silent), or off",
			DefaultValue: string(config.SessionsAutoTitle(nil, nil)),
			Kind:         settingsdialog.KindEnum,
			Choices:      []string{"prompt", "auto", "off"},
		}, sessAutoTitle(scopeSettings), sessAutoTitle(global), sessAutoTitle(project)),
		{Label: "Edit Tool", Kind: settingsdialog.KindHeader},
		row(settingsdialog.SettingEntry{
			Key:          "sagittarius.edit.enabled",
			Label:        "Edit file (edit)",
			Description:  "Register the edit tool (default on; off to fall back to full write_file only)",
			DefaultValue: fmtBool(config.EditEnabled(nil, nil)),
			Kind:         settingsdialog.KindBool,
		}, editEnabled(scopeSettings), editEnabled(global), editEnabled(project)),
		{Label: "Symbols", Kind: settingsdialog.KindHeader},
		row(settingsdialog.SettingEntry{
			Key:          "sagittarius.symbols.enabled",
			Label:        "Symbol navigation (find_symbol)",
			Description:  "Register the find_symbol code-navigation tool (default on; off to use an external MCP)",
			DefaultValue: fmtBool(config.SymbolsEnabled(nil, nil)),
			Kind:         settingsdialog.KindBool,
		}, symbolsEnabled(scopeSettings), symbolsEnabled(global), symbolsEnabled(project)),
		row(settingsdialog.SettingEntry{
			Key:          "sagittarius.symbols.preferGopls",
			Label:        "Prefer gopls for Go",
			Description:  "Note gopls MCP tools in find_symbol's description on Go modules (prompt-only)",
			DefaultValue: fmtBool(config.SymbolsPreferGopls(nil, nil)),
			Kind:         settingsdialog.KindBool,
		}, symbolsPreferGopls(scopeSettings), symbolsPreferGopls(global), symbolsPreferGopls(project)),
		{Label: "MCP", Kind: settingsdialog.KindHeader},
		row(settingsdialog.SettingEntry{
			Key:          "sagittarius.mcp.pruneToolSchemas",
			Label:        "Prune tool schemas",
			Description:  "Truncate descriptions of MCP tools sent to the model to save tokens",
			DefaultValue: fmtBool(config.PruneToolSchemasEnabled(nil, nil)),
			Kind:         settingsdialog.KindBool,
		}, pruneSchemas(scopeSettings), pruneSchemas(global), pruneSchemas(project)),
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
		if n < 0 {
			return fmt.Errorf("maxToolRounds must be >= 0 (0 = unlimited)")
		}
		if s.Sagittarius == nil {
			s.Sagittarius = &config.SagittariusSettings{}
		}
		s.Sagittarius.MaxToolRounds = &n
	case "sagittarius.contextLimitPreferDiscovered":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("must be true/false: %w", err)
		}
		if s.Sagittarius == nil {
			s.Sagittarius = &config.SagittariusSettings{}
		}
		s.Sagittarius.ContextLimitPreferDiscovered = &b
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
	case "ui.escapeAtOnPaste":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("escapeAtOnPaste must be true/false: %w", err)
		}
		return s.SetUIEscapeAtOnPaste(b)
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
	case "sagittarius.scriptToolEnabled":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("must be true/false: %w", err)
		}
		if s.Sagittarius == nil {
			s.Sagittarius = &config.SagittariusSettings{}
		}
		s.Sagittarius.ScriptToolEnabled = &b
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
	case "sagittarius.subagents.research.enabled", "sagittarius.subagents.coding.enabled":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("enabled must be true/false: %w", err)
		}
		cls := subagentClassSlot(s, key)
		cls.Enabled = &b
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
	case "sagittarius.mcp.pruneToolSchemas":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("pruneToolSchemas must be true/false: %w", err)
		}
		if s.Sagittarius == nil {
			s.Sagittarius = &config.SagittariusSettings{}
		}
		if s.Sagittarius.MCP == nil {
			s.Sagittarius.MCP = &config.SagittariusMCPConfig{}
		}
		s.Sagittarius.MCP.PruneToolSchemas = &b
	case "sagittarius.goal.maxTurns":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("maxTurns must be an integer: %w", err)
		}
		ensureGoalConfig(s).MaxTurns = &n
	case "sagittarius.goal.evaluatorProvider":
		ensureGoalConfig(s).EvaluatorProvider = value
	case "sagittarius.goal.evaluatorModel":
		ensureGoalConfig(s).EvaluatorModel = value
	case "sagittarius.goal.evaluatorTimeout":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("evaluatorTimeout must be an integer: %w", err)
		}
		ensureGoalConfig(s).EvaluatorTimeout = &n
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
	case "sagittarius.contextLimitPreferDiscovered":
		if s.Sagittarius != nil {
			s.Sagittarius.ContextLimitPreferDiscovered = nil
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
		return s.ClearUIField("showThinking")
	case "ui.hideBanner":
		return s.ClearUIField("hideBanner")
	case "ui.toolkitChecklistDismissed":
		return s.ClearUIField("toolkitChecklistDismissed")
	case "ui.escapeAtOnPaste":
		return s.ClearUIField("escapeAtOnPaste")
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
	case "sagittarius.scriptToolEnabled":
		if s.Sagittarius != nil {
			s.Sagittarius.ScriptToolEnabled = nil
		}
	case "sagittarius.subagents.enabled":
		if s.Sagittarius != nil && s.Sagittarius.Subagents != nil {
			s.Sagittarius.Subagents.Enabled = nil
		}
	case "sagittarius.subagents.research.enabled":
		if cls := existingSubagentClass(s, config.SubagentResearch); cls != nil {
			cls.Enabled = nil
		}
	case "sagittarius.subagents.coding.enabled":
		if cls := existingSubagentClass(s, config.SubagentCoding); cls != nil {
			cls.Enabled = nil
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
	case "sagittarius.mcp.pruneToolSchemas":
		if s.Sagittarius != nil && s.Sagittarius.MCP != nil {
			s.Sagittarius.MCP.PruneToolSchemas = nil
		}
	case "sagittarius.goal.maxTurns":
		if s.Sagittarius != nil && s.Sagittarius.Goal != nil {
			s.Sagittarius.Goal.MaxTurns = nil
		}
	case "sagittarius.goal.evaluatorProvider":
		if s.Sagittarius != nil && s.Sagittarius.Goal != nil {
			s.Sagittarius.Goal.EvaluatorProvider = ""
		}
	case "sagittarius.goal.evaluatorModel":
		if s.Sagittarius != nil && s.Sagittarius.Goal != nil {
			s.Sagittarius.Goal.EvaluatorModel = ""
		}
	case "sagittarius.goal.evaluatorTimeout":
		if s.Sagittarius != nil && s.Sagittarius.Goal != nil {
			s.Sagittarius.Goal.EvaluatorTimeout = nil
		}
	default:
		return fmt.Errorf("unknown setting key %q", key)
	}
	return nil
}

func ensureGoalConfig(s *config.Settings) *config.SagittariusGoalConfig {
	if s.Sagittarius == nil {
		s.Sagittarius = &config.SagittariusSettings{}
	}
	if s.Sagittarius.Goal == nil {
		s.Sagittarius.Goal = &config.SagittariusGoalConfig{}
	}
	return s.Sagittarius.Goal
}

func fmtBool(b bool) string { return strconv.FormatBool(b) }

func fmtPtrBool(p *bool) string {
	if p == nil {
		return ""
	}
	return strconv.FormatBool(*p)
}

func fmtPtrInt(p *int) string {
	if p == nil {
		return ""
	}
	return strconv.Itoa(*p)
}

func fmtPtrStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func sagOf(s *config.Settings) *config.SagittariusSettings {
	if s == nil {
		return nil
	}
	return s.Sagittarius
}

func sagMaxRounds(s *config.Settings) string {
	if sag := sagOf(s); sag != nil {
		return fmtPtrInt(sag.MaxToolRounds)
	}
	return ""
}

func sagPreferDiscovered(s *config.Settings) string {
	if sag := sagOf(s); sag != nil {
		return fmtPtrBool(sag.ContextLimitPreferDiscovered)
	}
	return ""
}

func sagDefaultMode(s *config.Settings) string {
	if sag := sagOf(s); sag != nil {
		return sag.DefaultMode
	}
	return ""
}

func projectUITheme(s *config.Settings) string {
	if s == nil {
		return ""
	}
	return s.UI().Theme
}

func uiBool(s *config.Settings, field string) string {
	if s == nil {
		return ""
	}
	ui := s.UI()
	switch field {
	case "showThinking":
		return fmtBool(ui.ShowThinking)
	case "hideBanner":
		return fmtBool(ui.HideBanner)
	case "toolkitChecklistDismissed":
		return fmtBool(ui.ToolkitChecklistDismissed)
	case "escapeAtOnPaste":
		return fmtBool(ui.EscapeAtOnPaste)
	default:
		return ""
	}
}

func boundaryEnforce(s *config.Settings) string {
	if s == nil || s.Security == nil || s.Security.ProjectBoundary == nil {
		return ""
	}
	return fmtPtrBool(s.Security.ProjectBoundary.Enforce)
}

func snapCfg(s *config.Settings) *config.SagittariusSnapshotConfig {
	if sag := sagOf(s); sag != nil {
		return sag.Snapshots
	}
	return nil
}

func snapEnabled(s *config.Settings) string {
	if c := snapCfg(s); c != nil {
		return fmtPtrBool(c.Enabled)
	}
	return ""
}

func snapMaxBytes(s *config.Settings) string {
	if c := snapCfg(s); c != nil {
		return fmtPtrInt(c.MaxFileBytes)
	}
	return ""
}

func verifyCfg(s *config.Settings) *config.SagittariusVerifyConfig {
	if sag := sagOf(s); sag != nil {
		return sag.Verify
	}
	return nil
}

func verifyAllowFix(s *config.Settings) string {
	if c := verifyCfg(s); c != nil {
		return fmtPtrBool(c.AllowFix)
	}
	return ""
}

func verifySuggest(s *config.Settings) string {
	if c := verifyCfg(s); c != nil {
		return fmtPtrBool(c.SuggestAfterWrite)
	}
	return ""
}

func verifyAutoCheck(s *config.Settings) string {
	if c := verifyCfg(s); c != nil {
		return fmtPtrBool(c.AutoCheckAfterWrite)
	}
	return ""
}

func verifyModuleWide(s *config.Settings) string {
	if c := verifyCfg(s); c != nil {
		return fmtPtrBool(c.AutoCheckModuleWide)
	}
	return ""
}

func verifyTimeout(s *config.Settings) string {
	if c := verifyCfg(s); c != nil {
		return fmtPtrInt(c.AutoCheckTimeoutSeconds)
	}
	return ""
}

func verifyRepoLocal(s *config.Settings) string {
	if c := verifyCfg(s); c != nil {
		return fmtPtrStr(c.RepoLocalTools)
	}
	return ""
}

func verifyEditLoop(s *config.Settings) string {
	if c := verifyCfg(s); c != nil {
		return fmtPtrInt(c.EditLoopThreshold)
	}
	return ""
}

func goalCfg(s *config.Settings) *config.SagittariusGoalConfig {
	if sag := sagOf(s); sag != nil {
		return sag.Goal
	}
	return nil
}

func goalMaxTurns(s *config.Settings) string {
	if c := goalCfg(s); c != nil {
		return fmtPtrInt(c.MaxTurns)
	}
	return ""
}

func goalEvalProvider(s *config.Settings) string {
	if c := goalCfg(s); c != nil {
		return c.EvaluatorProvider
	}
	return ""
}

func goalEvalModel(s *config.Settings) string {
	if c := goalCfg(s); c != nil {
		return c.EvaluatorModel
	}
	return ""
}

func goalEvalTimeout(s *config.Settings) string {
	if c := goalCfg(s); c != nil {
		return fmtPtrInt(c.EvaluatorTimeout)
	}
	return ""
}

func subEnabled(s *config.Settings) string {
	if sag := sagOf(s); sag != nil && sag.Subagents != nil {
		return fmtPtrBool(sag.Subagents.Enabled)
	}
	return ""
}

func subResearchEnabled(s *config.Settings) string {
	if cls := existingSubagentClass(s, config.SubagentResearch); cls != nil {
		return fmtPtrBool(cls.Enabled)
	}
	return ""
}

func subCodingEnabled(s *config.Settings) string {
	if cls := existingSubagentClass(s, config.SubagentCoding); cls != nil {
		return fmtPtrBool(cls.Enabled)
	}
	return ""
}

// existingSubagentClass reads a class block without creating one, so a read
// never marks the key as defined in this scope.
func existingSubagentClass(s *config.Settings, class config.SubagentClass) *config.SagittariusSubagentClass {
	sag := sagOf(s)
	if sag == nil || sag.Subagents == nil {
		return nil
	}
	if class == config.SubagentCoding {
		return sag.Subagents.Coding
	}
	return sag.Subagents.Research
}

// subagentClassSlot returns the class block for a settings key, creating the
// intermediate structs so a write to an untouched document works.
func subagentClassSlot(s *config.Settings, key string) *config.SagittariusSubagentClass {
	if s.Sagittarius == nil {
		s.Sagittarius = &config.SagittariusSettings{}
	}
	if s.Sagittarius.Subagents == nil {
		s.Sagittarius.Subagents = &config.SagittariusSubagents{}
	}
	subs := s.Sagittarius.Subagents
	if key == "sagittarius.subagents.coding.enabled" {
		if subs.Coding == nil {
			subs.Coding = &config.SagittariusSubagentClass{}
		}
		return subs.Coding
	}
	if subs.Research == nil {
		subs.Research = &config.SagittariusSubagentClass{}
	}
	return subs.Research
}

func scriptEnabled(s *config.Settings) string {
	if sag := sagOf(s); sag != nil {
		return fmtPtrBool(sag.ScriptToolEnabled)
	}
	return ""
}

func sessAutoTitle(s *config.Settings) string {
	if sag := sagOf(s); sag != nil && sag.Sessions != nil {
		return fmtPtrStr(sag.Sessions.AutoTitle)
	}
	return ""
}

func editEnabled(s *config.Settings) string {
	if sag := sagOf(s); sag != nil && sag.Edit != nil {
		return fmtPtrBool(sag.Edit.Enabled)
	}
	return ""
}

func symbolsEnabled(s *config.Settings) string {
	if sag := sagOf(s); sag != nil && sag.Symbols != nil {
		return fmtPtrBool(sag.Symbols.Enabled)
	}
	return ""
}

func symbolsPreferGopls(s *config.Settings) string {
	if sag := sagOf(s); sag != nil && sag.Symbols != nil {
		return fmtPtrBool(sag.Symbols.PreferGopls)
	}
	return ""
}

func pruneSchemas(s *config.Settings) string {
	if sag := sagOf(s); sag != nil && sag.MCP != nil {
		return fmtPtrBool(sag.MCP.PruneToolSchemas)
	}
	return ""
}
