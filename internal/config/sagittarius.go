package config

import (
	"encoding/json"
	"fmt"
	"strings"
)

// SagittariusSettings holds Sagittarius-specific settings under the top-level
// "sagittarius" key in settings.json. Unknown sub-keys round-trip via Extra.
type SagittariusSettings struct {
	DefaultModel string                     `json:"defaultModel,omitempty"`
	DefaultMode  string                     `json:"defaultMode,omitempty"`
	Modes        *SagittariusModes          `json:"modes,omitempty"`
	Subagents    *SagittariusSubagents      `json:"subagents,omitempty"`
	Extra        map[string]json.RawMessage `json:"-"`
}

// SagittariusModes holds per-interaction-mode overrides.
type SagittariusModes struct {
	Plan  *SagittariusModeConfig     `json:"plan,omitempty"`
	Ask   *SagittariusModeConfig     `json:"ask,omitempty"`
	Debug *SagittariusModeConfig     `json:"debug,omitempty"`
	Agent *SagittariusModeConfig     `json:"agent,omitempty"`
	Extra map[string]json.RawMessage `json:"-"`
}

// SagittariusModeConfig configures one interaction mode.
type SagittariusModeConfig struct {
	Model              string                     `json:"model,omitempty"`
	SystemPromptSuffix string                     `json:"systemPromptSuffix,omitempty"`
	Extra              map[string]json.RawMessage `json:"-"`
}

// SagittariusSubagents holds subagent model routing defaults.
type SagittariusSubagents struct {
	Default SagittariusSubagentConfig            `json:"default,omitempty"`
	Named   map[string]SagittariusSubagentConfig `json:"-"`
	Extra   map[string]json.RawMessage           `json:"-"`
}

// SagittariusSubagentConfig configures one subagent's model override.
type SagittariusSubagentConfig struct {
	Model string                     `json:"model,omitempty"`
	Extra map[string]json.RawMessage `json:"-"`
}

var validInteractionModes = map[string]struct{}{
	"agent": {},
	"plan":  {},
	"ask":   {},
	"debug": {},
}

// ValidateSagittariusSettings checks typed sagittarius fields for obvious errors.
//
// Per-mode blocks need no validation: both fields are optional. A mode with only
// systemPromptSuffix and no model is valid — ResolveModel falls back to
// sagittarius.defaultModel or the provider default while the suffix still applies.
func ValidateSagittariusSettings(s *SagittariusSettings) error {
	if s == nil {
		return nil
	}
	if dm := strings.TrimSpace(s.DefaultMode); dm != "" {
		if _, ok := validInteractionModes[strings.ToLower(dm)]; !ok {
			return fmt.Errorf("sagittarius.defaultMode %q: want agent, plan, ask, or debug", dm)
		}
	}
	return nil
}
