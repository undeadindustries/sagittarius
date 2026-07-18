package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// SettingScope identifies which settings file a change should be applied to.
type SettingScope int

const (
	ScopeGlobal  SettingScope = iota // ~/.sagittarius/settings.json
	ScopeProject                     // <workDir>/.sagittarius/settings.json
)

// String returns a human-readable label for the scope.
func (s SettingScope) String() string {
	if s == ScopeProject {
		return "project"
	}
	return "global"
}

// Documents holds both settings tiers (global + optional project) and a merged
// view where project values win. It is the canonical runtime settings source
// after startup; dialogs write to Global or Project via Save(scope) and refresh
// via ReloadMerged.
type Documents struct {
	// Global is the ~/.sagittarius/settings.json document. Always non-nil.
	Global *Settings
	// Project is the <workDir>/.sagittarius/settings.json document.
	// Nil when the file does not exist or no working directory was given.
	Project *Settings
	// merged is the effective settings computed by overlaying Project onto
	// Global (project wins). Read-only for runtime decisions; recomputed via
	// ReloadMerged after any write. Access it through Merged() so the pointer
	// swap is synchronized with the persistence writers that recompute it from
	// background goroutines (plan 04 moved live UI-pref saves off the UI thread).
	merged *Settings
	// mu serializes the persistence read-modify-write sequence (mutate Global →
	// Save → recompute merged) and the merged pointer swap/read, so concurrent
	// MutateGlobal calls from background tea.Cmd goroutines cannot interleave on
	// Global's map or lose the merged recompute.
	mu      sync.RWMutex
	workDir string
	loader  *Loader
}

// LoadDocuments loads both settings files for workDir (may be "") and returns
// a Documents with a pre-computed Merged view. A missing project file is not
// an error — Project will be nil and Merged == Global.
func LoadDocuments(workDir string) (*Documents, error) {
	loader, err := NewLoader()
	if err != nil {
		return nil, fmt.Errorf("load documents: %w", err)
	}
	global, err := loader.Load()
	if err != nil && !errors.Is(err, ErrSecretsInSettings) {
		return nil, fmt.Errorf("load documents: %w", err)
	}
	if global == nil {
		global = &Settings{Raw: map[string]json.RawMessage{}}
	}

	var project *Settings
	if workDir != "" {
		project, err = LoadProjectSettings(workDir)
		if err != nil {
			slog.Warn("could not load project settings", "error", err)
			project = nil
		}
	}

	d := &Documents{
		Global:  global,
		Project: project,
		workDir: workDir,
		loader:  loader,
	}
	d.merged = mergeSettings(global, project)
	return d, nil
}

// Merged returns the effective (global+project) settings view. It is safe to call
// concurrently with the persistence writers; the returned *Settings is read-only
// for runtime decisions (never mutate or persist through it).
func (d *Documents) Merged() *Settings {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.merged
}

// Loader returns the global-file Loader (for backward-compatible saves via
// Loader.Save).
func (d *Documents) Loader() *Loader {
	return d.loader
}

// WorkDir returns the working directory used when loading project settings.
func (d *Documents) WorkDir() string {
	return d.workDir
}

// Save writes the specified scope's Settings to disk and reloads Merged.
func (d *Documents) Save(scope SettingScope) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.saveLocked(scope)
}

// saveLocked is Save's body; callers must hold d.mu.
func (d *Documents) saveLocked(scope SettingScope) error {
	switch scope {
	case ScopeGlobal:
		if err := d.loader.Save(d.Global); err != nil {
			return err
		}
	case ScopeProject:
		if err := d.saveProjectLocked(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown scope %d", scope)
	}
	d.merged = mergeSettings(d.Global, d.Project)
	return nil
}

// MutateGlobal applies fn to the global document, persists it, and refreshes
// Merged. It is the single entry point for global scalar settings writes, so a
// write can never leave Merged stale (the root cause of the dual-scope bugs).
// The mutate→save→recompute sequence runs under the lock so concurrent callers
// (e.g. background Ctrl+T/Alt+T persists) cannot interleave on Global's map.
func (d *Documents) MutateGlobal(fn func(*Settings) error) error {
	if fn == nil {
		return fmt.Errorf("mutate global settings: nil mutator")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := fn(d.Global); err != nil {
		return err
	}
	return d.saveLocked(ScopeGlobal) // saveLocked recomputes merged
}

// SaveProject writes the project Settings to disk and reloads Merged. It
// creates the project directory if needed. If d.Project is nil a new empty
// Settings is initialised first.
func (d *Documents) SaveProject() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.saveProjectLocked(); err != nil {
		return err
	}
	d.merged = mergeSettings(d.Global, d.Project)
	return nil
}

func (d *Documents) saveProjectLocked() error {
	if d.workDir == "" {
		return fmt.Errorf("save project settings: no working directory")
	}
	if d.Project == nil {
		d.Project = &Settings{Raw: map[string]json.RawMessage{}}
	}
	path := ResolveProjectSettingsPath(d.workDir)
	out, err := encodeSettingsDocument(d.Project)
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
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create project settings dir %q: %w", dir, err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return fmt.Errorf("write temp project settings %q: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename project settings file: %w", err)
	}
	return nil
}

// ReloadMerged recomputes Merged from the current in-memory Global and Project.
// Call this after any in-memory mutation to Global or Project before the next
// read of Merged.
func (d *Documents) ReloadMerged() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.merged = mergeSettings(d.Global, d.Project)
}

// TargetSettings returns the mutable *Settings for the given scope. For
// ScopeProject, docs.Project is lazily initialised to an empty Settings if
// nil so callers can write without a nil-check (the file is only created on
// the next Save). Always returns docs.Global for ScopeGlobal.
func (d *Documents) TargetSettings(scope SettingScope) *Settings {
	if scope == ScopeProject {
		if d.Project == nil {
			d.Project = &Settings{Raw: map[string]json.RawMessage{}}
		}
		return d.Project
	}
	return d.Global
}

// IsDefined reports whether the given top-level JSON key is explicitly set in
// the given scope's settings file (not just inherited from the other tier).
func (d *Documents) IsDefined(scope SettingScope, key string) bool {
	var s *Settings
	switch scope {
	case ScopeGlobal:
		s = d.Global
	case ScopeProject:
		s = d.Project
	}
	return settingsHasKey(s, key)
}

// ScopeOf returns the scope that supplies the effective value for key, or
// ScopeGlobal when neither scope defines it (inheriting the default).
func (d *Documents) ScopeOf(key string) SettingScope {
	if settingsHasKey(d.Project, key) {
		return ScopeProject
	}
	return ScopeGlobal
}

// ScopeHint returns a short parenthetical note for dialogs showing where the
// effective value comes from versus where the user is currently editing.
func (d *Documents) ScopeHint(editingScope SettingScope, key string) string {
	switch editingScope {
	case ScopeProject:
		if settingsHasKey(d.Global, key) && settingsHasKey(d.Project, key) {
			return "(Also modified in Global)"
		}
		if settingsHasKey(d.Global, key) {
			return "(Inherited from Global)"
		}
	case ScopeGlobal:
		if settingsHasKey(d.Project, key) {
			return "(Modified in Project)"
		}
	}
	return ""
}

// ProjectPath returns the expected project settings file path, whether or not
// the file exists.
func (d *Documents) ProjectPath() string {
	if d.workDir == "" {
		return ""
	}
	return ResolveProjectSettingsPath(d.workDir)
}

// GlobalPath returns the global settings file path.
func (d *Documents) GlobalPath() string {
	return d.loader.Path()
}

// settingsHasKey reports whether s has the key set at the top level of the
// JSON document (typed sections or Raw passthrough).
func settingsHasKey(s *Settings, key string) bool {
	if s == nil {
		return false
	}
	switch key {
	case "providers":
		return s.Providers != nil
	case "sagittarius":
		return s.Sagittarius != nil
	case "security":
		return s.Security != nil
	default:
		_, ok := s.Raw[key]
		return ok
	}
}

// ─── Merge engine ────────────────────────────────────────────────────────────

// mergeSettings builds a merged Settings where project values win over global
// for most keys. mcpServers uses a shallow (per-server) merge; provider custom
// definitions stay global-only (credentials + catalog are user-wide).
// overlayStr returns project when it is set (non-empty), else global. It encodes
// the "project string field wins when present" rule shared by the merge* helpers.
// The ~string constraint also covers named string types (e.g. WireFormat).
func overlayStr[T ~string](global, project T) T {
	if project != "" {
		return project
	}
	return global
}

// overlayPtr returns project when it is set (non-nil), else global. Optional
// fields use a nil pointer to mean "inherit from the lower tier", so a non-nil
// project pointer (even one addressing the zero value) overrides global.
func overlayPtr[T any](global, project *T) *T {
	if project != nil {
		return project
	}
	return global
}

func mergeSettings(global, project *Settings) *Settings {
	if project == nil {
		return global
	}
	if global == nil {
		return project
	}
	merged := &Settings{
		Raw:         mergeRaw(global.Raw, project.Raw),
		Providers:   mergeProviders(global.Providers, project.Providers),
		Sagittarius: mergeSagittarius(global.Sagittarius, project.Sagittarius),
		Security:    mergeSecurity(global.Security, project.Security),
	}
	return merged
}

// mergeRaw merges two Raw maps. project wins per key, except mcpServers which
// uses a shallow per-server merge so project can add servers without replacing
// all global ones.
func mergeRaw(global, project map[string]json.RawMessage) map[string]json.RawMessage {
	if len(project) == 0 {
		return global
	}
	merged := make(map[string]json.RawMessage, len(global)+len(project))
	for k, v := range global {
		merged[k] = v
	}
	for k, v := range project {
		if k == "mcpServers" {
			merged[k] = mergeMCPServersRaw(global[k], v)
		} else {
			merged[k] = v
		}
	}
	return merged
}

// mergeMCPServersRaw shallow-merges two mcpServers JSON objects. Global is the
// base; project entries add or replace individual server entries by name.
func mergeMCPServersRaw(globalRaw, projectRaw json.RawMessage) json.RawMessage {
	if len(projectRaw) == 0 {
		return globalRaw
	}
	if len(globalRaw) == 0 {
		return projectRaw
	}
	var gServers, pServers map[string]json.RawMessage
	if err := json.Unmarshal(globalRaw, &gServers); err != nil {
		return globalRaw
	}
	if err := json.Unmarshal(projectRaw, &pServers); err != nil {
		return globalRaw
	}
	m := make(map[string]json.RawMessage, len(gServers)+len(pServers))
	for k, v := range gServers {
		m[k] = v
	}
	for k, v := range pServers {
		m[k] = v
	}
	out, err := json.Marshal(m)
	if err != nil {
		return globalRaw
	}
	return out
}

func mergeProviders(global, project *ProvidersSettings) *ProvidersSettings {
	if project == nil {
		return global
	}
	if global == nil {
		return project
	}
	// Start from a shallow copy of global so unknown Extra keys round-trip.
	merged := *global
	// A project-scoped /model pick sets Active in the project tier; use it so
	// endpoint resolution and mode routing see the project's chosen provider.
	merged.Active = overlayStr(global.Active, project.Active)
	// Per-instance settings can be project-overridden (model, activeModels, etc).
	// Custom provider definitions (connection params + catalog) stay global-only.
	merged.OpenAI = mergeProviderInstance(global.OpenAI, project.OpenAI)
	merged.GeminiAPIKey = mergeProviderInstance(global.GeminiAPIKey, project.GeminiAPIKey)
	merged.OpenAIResponses = mergeProviderInstance(global.OpenAIResponses, project.OpenAIResponses)
	// Extra holds raw custom provider instance blocks; project can override per block.
	merged.Extra = mergeRaw(global.Extra, project.Extra)
	return &merged
}

// mergeProviderInstance overlays non-zero project fields onto a copy of global.
// The models map is shallow-merged by model id; activeModels is replaced atomically
// when project sets it (a curated list is intentionally all-or-nothing).
func mergeProviderInstance(global, project *ProviderInstanceConfig) *ProviderInstanceConfig {
	if project == nil {
		return global
	}
	if global == nil {
		return project
	}
	merged := *global
	merged.Model = overlayStr(global.Model, project.Model)
	merged.BaseURL = overlayStr(global.BaseURL, project.BaseURL)
	merged.ContextLimit = overlayPtr(global.ContextLimit, project.ContextLimit)
	merged.ContextLimitUserSet = overlayPtr(global.ContextLimitUserSet, project.ContextLimitUserSet)
	merged.CompressionThreshold = overlayPtr(global.CompressionThreshold, project.CompressionThreshold)
	merged.PreserveFraction = overlayPtr(global.PreserveFraction, project.PreserveFraction)
	merged.PromptMode = overlayStr(global.PromptMode, project.PromptMode)
	merged.EnableTools = overlayPtr(global.EnableTools, project.EnableTools)
	merged.Timeout = overlayPtr(global.Timeout, project.Timeout)
	merged.Temperature = overlayPtr(global.Temperature, project.Temperature)
	merged.ToolCallParsing = overlayStr(global.ToolCallParsing, project.ToolCallParsing)
	merged.SystemPromptOverride = overlayStr(global.SystemPromptOverride, project.SystemPromptOverride)
	merged.ReasoningEffort = overlayStr(global.ReasoningEffort, project.ReasoningEffort)
	merged.UseResponseChaining = overlayPtr(global.UseResponseChaining, project.UseResponseChaining)
	merged.WireFormat = overlayStr(global.WireFormat, project.WireFormat)
	merged.ShowThinking = overlayPtr(global.ShowThinking, project.ShowThinking)
	merged.Personality = overlayStr(global.Personality, project.Personality)
	merged.ToolOutputMaskingEnabled = overlayPtr(global.ToolOutputMaskingEnabled, project.ToolOutputMaskingEnabled)
	merged.ToolOutputMaskingProtectionFraction = overlayPtr(global.ToolOutputMaskingProtectionFraction, project.ToolOutputMaskingProtectionFraction)
	merged.ToolOutputMaskingPrunableFraction = overlayPtr(global.ToolOutputMaskingPrunableFraction, project.ToolOutputMaskingPrunableFraction)
	merged.ToolOutputMaskingProtectLatestTurn = overlayPtr(global.ToolOutputMaskingProtectLatestTurn, project.ToolOutputMaskingProtectLatestTurn)
	// activeModels: project replaces when non-empty (curated list is atomic).
	if len(project.ActiveModels) > 0 {
		merged.ActiveModels = project.ActiveModels
	}
	// models: shallow merge by model id — project per-model config wins per entry.
	if len(project.Models) > 0 {
		mergedModels := make(map[string]ProviderModelConfig, len(global.Models)+len(project.Models))
		for k, v := range global.Models {
			mergedModels[k] = v
		}
		for k, v := range project.Models {
			mergedModels[k] = v
		}
		merged.Models = mergedModels
	}
	merged.Extra = mergeRaw(global.Extra, project.Extra)
	return &merged
}

func mergeSagittarius(global, project *SagittariusSettings) *SagittariusSettings {
	if project == nil {
		return global
	}
	if global == nil {
		return project
	}
	merged := *global
	merged.DefaultModel = overlayStr(global.DefaultModel, project.DefaultModel)
	if len(project.DefaultModels) > 0 {
		merged.DefaultModels = project.DefaultModels
	}
	merged.DefaultMode = overlayStr(global.DefaultMode, project.DefaultMode)
	merged.MaxToolRounds = overlayPtr(global.MaxToolRounds, project.MaxToolRounds)
	merged.Modes = mergeModes(global.Modes, project.Modes)
	merged.SystemPrompt = mergeSystemPromptConfig(global.SystemPrompt, project.SystemPrompt)
	merged.Snapshots = mergeSnapshotConfig(global.Snapshots, project.Snapshots)
	merged.Verify = mergeVerifyConfig(global.Verify, project.Verify)
	merged.Symbols = mergeSymbolsConfig(global.Symbols, project.Symbols)
	// Web, Tools, Compression, Subagents: global-only in Phase 1.
	return &merged
}

func mergeModes(global, project *SagittariusModes) *SagittariusModes {
	if project == nil {
		return global
	}
	if global == nil {
		return project
	}
	merged := *global
	merged.Agent = mergeModeConfig(global.Agent, project.Agent)
	merged.Plan = mergeModeConfig(global.Plan, project.Plan)
	merged.Ask = mergeModeConfig(global.Ask, project.Ask)
	merged.Debug = mergeModeConfig(global.Debug, project.Debug)
	return &merged
}

func mergeModeConfig(global, project *SagittariusModeConfig) *SagittariusModeConfig {
	if project == nil {
		return global
	}
	if global == nil {
		return project
	}
	merged := *global
	merged.Model = overlayStr(global.Model, project.Model)
	merged.Provider = overlayStr(global.Provider, project.Provider)
	merged.SystemPromptSuffix = overlayStr(global.SystemPromptSuffix, project.SystemPromptSuffix)
	return &merged
}

func mergeSystemPromptConfig(global, project *SagittariusSystemPromptConfig) *SagittariusSystemPromptConfig {
	if project == nil {
		return global
	}
	if global == nil {
		return project
	}
	merged := *global
	merged.Personality = overlayStr(global.Personality, project.Personality)
	merged.Variant = overlayStr(global.Variant, project.Variant)
	return &merged
}

func mergeSnapshotConfig(global, project *SagittariusSnapshotConfig) *SagittariusSnapshotConfig {
	if project == nil {
		return global
	}
	if global == nil {
		return project
	}
	merged := *global
	merged.Enabled = overlayPtr(global.Enabled, project.Enabled)
	merged.MaxFileBytes = overlayPtr(global.MaxFileBytes, project.MaxFileBytes)
	return &merged
}

func mergeVerifyConfig(global, project *SagittariusVerifyConfig) *SagittariusVerifyConfig {
	if project == nil {
		return global
	}
	if global == nil {
		return project
	}
	merged := *global
	merged.SuggestAfterWrite = overlayPtr(global.SuggestAfterWrite, project.SuggestAfterWrite)
	merged.AllowFix = overlayPtr(global.AllowFix, project.AllowFix)
	return &merged
}

func mergeSymbolsConfig(global, project *SagittariusSymbolsConfig) *SagittariusSymbolsConfig {
	if project == nil {
		return global
	}
	if global == nil {
		return project
	}
	merged := *global
	merged.Enabled = overlayPtr(global.Enabled, project.Enabled)
	merged.PreferGopls = overlayPtr(global.PreferGopls, project.PreferGopls)
	return &merged
}

func mergeSecurity(global, project *SecuritySettings) *SecuritySettings {
	if project == nil {
		return global
	}
	if global == nil {
		return project
	}
	merged := *global
	if project.ProjectBoundary != nil {
		if global.ProjectBoundary == nil {
			merged.ProjectBoundary = project.ProjectBoundary
		} else {
			pb := *global.ProjectBoundary
			pb.Enforce = overlayPtr(global.ProjectBoundary.Enforce, project.ProjectBoundary.Enforce)
			merged.ProjectBoundary = &pb
		}
	}
	merged.Extra = mergeRaw(global.Extra, project.Extra)
	return &merged
}
