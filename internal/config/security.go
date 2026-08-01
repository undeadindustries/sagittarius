package config

import "encoding/json"

// DefaultSnapshotMaxFileBytes caps the size of a file Sagittarius will snapshot
// before/after a write. Larger files are recorded as metadata-only changes so a
// single huge generated file cannot blow up memory or the on-disk index
// (mirrors the OpenCode 2 MiB snapshot limit).
const DefaultSnapshotMaxFileBytes = 2 * 1024 * 1024

// SecuritySettings is the typed top-level "security" section of settings.json.
// Unknown sub-keys round-trip via Extra.
type SecuritySettings struct {
	ProjectBoundary *ProjectBoundaryConfig     `json:"projectBoundary,omitempty"`
	Extra           map[string]json.RawMessage `json:"-"`
}

// ProjectBoundaryConfig controls whether tools may mutate files outside the
// project (workspace) root. Enforce is a pointer so "unset" is distinct from
// an explicit false (needed for project-over-global resolution).
type ProjectBoundaryConfig struct {
	Enforce *bool                      `json:"enforce,omitempty"`
	Extra   map[string]json.RawMessage `json:"-"`
}

// SagittariusSnapshotConfig toggles local file snapshots (powering /diff and
// /undo) and bounds the per-file snapshot size. Pointers distinguish "unset"
// from an explicit value for project-over-global resolution.
type SagittariusSnapshotConfig struct {
	Enabled      *bool                      `json:"enabled,omitempty"`
	MaxFileBytes *int                       `json:"maxFileBytes,omitempty"`
	Extra        map[string]json.RawMessage `json:"-"`
}

// ProjectBoundaryEnforced reports whether out-of-project file mutations should
// be blocked. The project-level setting wins over the global one when set;
// otherwise the default is false (backward compatible).
func ProjectBoundaryEnforced(global, project *Settings) bool {
	if v, ok := projectBoundaryValue(project); ok {
		return v
	}
	if v, ok := projectBoundaryValue(global); ok {
		return v
	}
	return false
}

func projectBoundaryValue(s *Settings) (bool, bool) {
	if s == nil || s.Security == nil || s.Security.ProjectBoundary == nil {
		return false, false
	}
	if s.Security.ProjectBoundary.Enforce == nil {
		return false, false
	}
	return *s.Security.ProjectBoundary.Enforce, true
}

// SnapshotsEnabled reports whether local file snapshots are enabled. Project
// wins over global; the default is true.
func SnapshotsEnabled(global, project *Settings) bool {
	if v, ok := snapshotsEnabledValue(project); ok {
		return v
	}
	if v, ok := snapshotsEnabledValue(global); ok {
		return v
	}
	return true
}

func snapshotsEnabledValue(s *Settings) (bool, bool) {
	cfg := snapshotConfig(s)
	if cfg == nil || cfg.Enabled == nil {
		return false, false
	}
	return *cfg.Enabled, true
}

// SnapshotMaxFileBytes resolves the per-file snapshot size cap. Project wins
// over global; the default is DefaultSnapshotMaxFileBytes. Non-positive values
// fall back to the default.
func SnapshotMaxFileBytes(global, project *Settings) int {
	if v, ok := snapshotMaxValue(project); ok {
		return v
	}
	if v, ok := snapshotMaxValue(global); ok {
		return v
	}
	return DefaultSnapshotMaxFileBytes
}

func snapshotMaxValue(s *Settings) (int, bool) {
	cfg := snapshotConfig(s)
	if cfg == nil || cfg.MaxFileBytes == nil || *cfg.MaxFileBytes <= 0 {
		return 0, false
	}
	return *cfg.MaxFileBytes, true
}

func snapshotConfig(s *Settings) *SagittariusSnapshotConfig {
	if s == nil || s.Sagittarius == nil {
		return nil
	}
	return s.Sagittarius.Snapshots
}

// VerifySuggestAfterWrite reports whether the runner should emit a one-line
// reminder to verify after a turn that wrote files. Project wins over global;
// the default is false.
func VerifySuggestAfterWrite(global, project *Settings) bool {
	if v, ok := verifyBoolValue(project, func(c *SagittariusVerifyConfig) *bool { return c.SuggestAfterWrite }); ok {
		return v
	}
	if v, ok := verifyBoolValue(global, func(c *SagittariusVerifyConfig) *bool { return c.SuggestAfterWrite }); ok {
		return v
	}
	return false
}

// VerifyAllowFix reports whether run_project_checks may run mutating
// formatters/auto-fixers. Project wins over global; the default is false.
func VerifyAllowFix(global, project *Settings) bool {
	if v, ok := verifyBoolValue(project, func(c *SagittariusVerifyConfig) *bool { return c.AllowFix }); ok {
		return v
	}
	if v, ok := verifyBoolValue(global, func(c *SagittariusVerifyConfig) *bool { return c.AllowFix }); ok {
		return v
	}
	return false
}

// VerifyAutoCheckAfterWrite reports whether the runner should automatically run
// read-only lint/format checks on files after they are written. Project wins over global;
// the default is true.
func VerifyAutoCheckAfterWrite(global, project *Settings) bool {
	if v, ok := verifyBoolValue(project, func(c *SagittariusVerifyConfig) *bool { return c.AutoCheckAfterWrite }); ok {
		return v
	}
	if v, ok := verifyBoolValue(global, func(c *SagittariusVerifyConfig) *bool { return c.AutoCheckAfterWrite }); ok {
		return v
	}
	return true
}

// VerifyAutoCheckModuleWide reports whether automatic post-write checks should include
// whole-module checks (like go vet, tsc) when no LSP server covers them. Project wins
// over global; the default is false because these are slow.
func VerifyAutoCheckModuleWide(global, project *Settings) bool {
	if v, ok := verifyBoolValue(project, func(c *SagittariusVerifyConfig) *bool { return c.AutoCheckModuleWide }); ok {
		return v
	}
	if v, ok := verifyBoolValue(global, func(c *SagittariusVerifyConfig) *bool { return c.AutoCheckModuleWide }); ok {
		return v
	}
	return false
}

// VerifyAutoCheckTimeoutSeconds returns the timeout for post-write checks.
// Project wins over global; the default is 60 seconds.
func VerifyAutoCheckTimeoutSeconds(global, project *Settings) int {
	if v, ok := verifyIntValue(project, func(c *SagittariusVerifyConfig) *int { return c.AutoCheckTimeoutSeconds }); ok {
		return v
	}
	if v, ok := verifyIntValue(global, func(c *SagittariusVerifyConfig) *int { return c.AutoCheckTimeoutSeconds }); ok {
		return v
	}
	return 60
}

// VerifyRepoLocalTools reports the policy for automatically running linters
// located under the workspace (e.g. node_modules/.bin). Project wins over global;
// the default is RepoLocalToolsPrompt.
func VerifyRepoLocalTools(global, project *Settings) RepoLocalToolsPolicy {
	if v, ok := verifyStringValue(project, func(c *SagittariusVerifyConfig) *string { return c.RepoLocalTools }); ok {
		return parseRepoLocalToolsPolicy(v)
	}
	if v, ok := verifyStringValue(global, func(c *SagittariusVerifyConfig) *string { return c.RepoLocalTools }); ok {
		return parseRepoLocalToolsPolicy(v)
	}
	return RepoLocalToolsPrompt
}

// VerifyEditLoopThreshold returns the number of failing edits to a single file
// in one turn that triggers a "stop and re-evaluate" nudge. Project wins over
// global; the default is 3. A value of 0 disables the detector.
func VerifyEditLoopThreshold(global, project *Settings) int {
	if v, ok := verifyIntValue(project, func(c *SagittariusVerifyConfig) *int { return c.EditLoopThreshold }); ok {
		return v
	}
	if v, ok := verifyIntValue(global, func(c *SagittariusVerifyConfig) *int { return c.EditLoopThreshold }); ok {
		return v
	}
	return 3
}

func verifyBoolValue(s *Settings, pick func(*SagittariusVerifyConfig) *bool) (bool, bool) {
	if s == nil || s.Sagittarius == nil || s.Sagittarius.Verify == nil {
		return false, false
	}
	if v := pick(s.Sagittarius.Verify); v != nil {
		return *v, true
	}
	return false, false
}

func verifyIntValue(s *Settings, pick func(*SagittariusVerifyConfig) *int) (int, bool) {
	if s == nil || s.Sagittarius == nil || s.Sagittarius.Verify == nil {
		return 0, false
	}
	if v := pick(s.Sagittarius.Verify); v != nil {
		return *v, true
	}
	return 0, false
}

func verifyStringValue(s *Settings, pick func(*SagittariusVerifyConfig) *string) (string, bool) {
	if s == nil || s.Sagittarius == nil || s.Sagittarius.Verify == nil {
		return "", false
	}
	if v := pick(s.Sagittarius.Verify); v != nil {
		return *v, true
	}
	return "", false
}
