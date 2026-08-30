package config

import "strings"

// SubagentClass names an independently-toggled subagent class.
type SubagentClass string

const (
	// SubagentResearch is the read-only research class behind the task tool.
	SubagentResearch SubagentClass = "research"
	// SubagentCoding is the write-capable class behind the code_task tool.
	SubagentCoding SubagentClass = "coding"
)

// ResolveReadOnly returns whether the durable read-only inspection mode is enabled.
func ResolveReadOnly(cfg *Settings, fallback bool) bool {
	if cfg == nil || cfg.Sagittarius == nil || cfg.Sagittarius.ReadOnly == nil {
		return fallback
	}
	return *cfg.Sagittarius.ReadOnly
}

// ResearchSubagentsEnabled reports whether read-only research subagents (the
// task tool) are enabled. Project settings win over global. The legacy
// sagittarius.subagents.enabled switch is the fallback so existing
// configurations keep working. The default is false.
func ResearchSubagentsEnabled(global, project *Settings) bool {
	if v, ok := subagentClassEnabled(project, SubagentResearch); ok {
		return v
	}
	if v, ok := subagentClassEnabled(global, SubagentResearch); ok {
		return v
	}
	if v, ok := legacySubagentsEnabled(project); ok {
		return v
	}
	if v, ok := legacySubagentsEnabled(global); ok {
		return v
	}
	return false
}

// CodingSubagentsEnabled reports whether write-capable coding subagents (the
// code_task tool) are enabled. Project settings win over global. The legacy
// sagittarius.subagents.enabled switch is deliberately NOT a fallback: it
// predates leased writes and never meant "may modify files". The default is
// false.
func CodingSubagentsEnabled(global, project *Settings) bool {
	if v, ok := subagentClassEnabled(project, SubagentCoding); ok {
		return v
	}
	if v, ok := subagentClassEnabled(global, SubagentCoding); ok {
		return v
	}
	return false
}

// SubagentModel selects the model for a subagent class.
//
// Resolution order (first non-empty wins):
//  1. sagittarius.subagents.<class>.model
//  2. sagittarius.subagents.default.model
//  3. liveModel — the model the parent loop is currently using
//
// Pinning the result keeps a mode override (which may route the parent's own
// mode to an unsuitable engine) from capturing the child.
func SubagentModel(class SubagentClass, cfg *SagittariusSettings, liveModel string) string {
	if cfg != nil && cfg.Subagents != nil {
		if cls := subagentClass(cfg.Subagents, class); cls != nil {
			if m := strings.TrimSpace(cls.Model); m != "" {
				return m
			}
		}
		if m := strings.TrimSpace(cfg.Subagents.Default.Model); m != "" {
			return m
		}
	}
	return strings.TrimSpace(liveModel)
}

func subagentClass(s *SagittariusSubagents, class SubagentClass) *SagittariusSubagentClass {
	if s == nil {
		return nil
	}
	switch class {
	case SubagentResearch:
		return s.Research
	case SubagentCoding:
		return s.Coding
	}
	return nil
}

func subagentClassEnabled(s *Settings, class SubagentClass) (bool, bool) {
	if s == nil || s.Sagittarius == nil {
		return false, false
	}
	cls := subagentClass(s.Sagittarius.Subagents, class)
	if cls == nil || cls.Enabled == nil {
		return false, false
	}
	return *cls.Enabled, true
}

func legacySubagentsEnabled(s *Settings) (bool, bool) {
	if s == nil || s.Sagittarius == nil || s.Sagittarius.Subagents == nil {
		return false, false
	}
	if v := s.Sagittarius.Subagents.Enabled; v != nil {
		return *v, true
	}
	return false, false
}
