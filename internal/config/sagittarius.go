package config

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Personality ids recognized for the system prompt. Canonical home is config so
// both internal/prompt and internal/provider can validate without an import
// cycle (tools imports provider, so provider must not import prompt).
const (
	PersonalityProgrammer        = "programmer"
	PersonalitySysadmin          = "sysadmin"
	PersonalityPersonalAssistant = "personal-assistant"
	PersonalityCreativeAssistant = "creative-assistant"
	// PersonalityAssistant is the legacy generic id. It is accepted on read and
	// canonicalized to personal-assistant (see CanonicalPersonality).
	PersonalityAssistant = "assistant"
)

var knownPersonalities = map[string]struct{}{
	PersonalityProgrammer:        {},
	PersonalitySysadmin:          {},
	PersonalityPersonalAssistant: {},
	PersonalityCreativeAssistant: {},
	PersonalityAssistant:         {},
}

// KnownPersonality reports whether id is a recognized personality (case- and
// space-insensitive). Empty is not recognized.
func KnownPersonality(id string) bool {
	_, ok := knownPersonalities[strings.ToLower(strings.TrimSpace(id))]
	return ok
}

// CanonicalPersonality normalizes id to its canonical personality (lower-cased,
// trimmed, legacy "assistant" -> personal-assistant). Unknown or empty ids
// return the programmer default with ok=false.
func CanonicalPersonality(id string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case PersonalityProgrammer:
		return PersonalityProgrammer, true
	case PersonalitySysadmin:
		return PersonalitySysadmin, true
	case PersonalityPersonalAssistant:
		return PersonalityPersonalAssistant, true
	case PersonalityCreativeAssistant:
		return PersonalityCreativeAssistant, true
	case PersonalityAssistant:
		return PersonalityPersonalAssistant, true
	default:
		return PersonalityProgrammer, false
	}
}

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
	Tools *SagittariusUtilityConfig `json:"tools,omitempty"`
	// SystemPrompt sets the global default personality + variant for the system
	// prompt. Provider and per-model overrides take precedence (see
	// prompt.ResolvePersonality / ResolveVariant).
	SystemPrompt *SagittariusSystemPromptConfig `json:"systemPrompt,omitempty"`
	// Snapshots toggles local file snapshots (powering /diff and /undo) and
	// bounds the per-file snapshot size.
	Snapshots *SagittariusSnapshotConfig `json:"snapshots,omitempty"`
	Extra     map[string]json.RawMessage `json:"-"`
}

// SagittariusSystemPromptConfig is the global default for the system-prompt
// personality and variant. Empty fields fall back to the built-in defaults
// (programmer / full).
type SagittariusSystemPromptConfig struct {
	Personality string                     `json:"personality,omitempty"`
	Variant     string                     `json:"variant,omitempty"`
	Extra       map[string]json.RawMessage `json:"-"`
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
	// Provider, when non-empty, qualifies the model override with a specific
	// provider id. Entering the mode will switch to this (provider, model) pair
	// so the correct backend and wire-format are used.
	Provider           string                     `json:"provider,omitempty"`
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
	if s.SystemPrompt != nil {
		if v := strings.TrimSpace(s.SystemPrompt.Variant); v != "" {
			switch strings.ToLower(v) {
			case "full", "lite":
			default:
				return fmt.Errorf("sagittarius.systemPrompt.variant %q: want full or lite", v)
			}
		}
	}
	return nil
}
