package config

import (
	"encoding/json"
	"fmt"
	"strings"
)

// SagittariusSettings holds Sagittarius-specific settings under the top-level
// "sagittarius" key in settings.json. Unknown sub-keys round-trip via Extra.
type SagittariusSettings struct {
	// DefaultModel is the legacy single global default. It now sits at the bottom
	// of the resolution chain (below the provider instance default) so it never
	// clobbers an active provider's configured model (see modes.ResolveModel).
	DefaultModel string `json:"defaultModel,omitempty"`
	// DefaultModels maps a normalized provider id to its preferred default model.
	// It is the provider-scoped successor to DefaultModel and sits just below the
	// per-mode override, so it beats the raw provider instance / built-in default
	// while still yielding to an explicit mode model (see modes.ResolveModel).
	DefaultModels map[string]string     `json:"defaultModels,omitempty"`
	DefaultMode   string                `json:"defaultMode,omitempty"`
	Modes         *SagittariusModes     `json:"modes,omitempty"`
	Subagents     *SagittariusSubagents `json:"subagents,omitempty"`
	// Compression overrides the model used for context compression /
	// summarization. Empty Model means it follows the live mode-resolved model.
	Compression *SagittariusUtilityConfig `json:"compression,omitempty"`
	// Tools overrides the model used for tool-utility calls. Empty Model means
	// it follows the live mode-resolved model. (Reserved: no model-using tool
	// consumes it yet.)
	Tools *SagittariusUtilityConfig  `json:"tools,omitempty"`
	Extra map[string]json.RawMessage `json:"-"`
}

// SagittariusUtilityConfig overrides the model for an auxiliary role (context
// compression, tool-utility calls). An empty Model means the role follows the
// live mode-resolved model rather than a fixed override.
type SagittariusUtilityConfig struct {
	Model string                     `json:"model,omitempty"`
	Extra map[string]json.RawMessage `json:"-"`
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
