// Package modes implements Sagittarius interaction modes and model routing.
//
// Interaction modes (plan, ask, debug, agent) control model selection and optional
// system-prompt suffixes. They are orthogonal to fork approval-mode tool policy.
package modes

import (
	"fmt"
	"strings"
	"sync"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/log"
)

// Mode is a Sagittarius interaction mode.
type Mode int

const (
	// ModeAgent is normal agent operation (default when unset).
	ModeAgent Mode = iota
	// ModePlan routes to a planning-oriented model override when configured.
	ModePlan
	// ModeAsk routes to a read-only Q&A model override when configured.
	ModeAsk
	// ModeDebug enables extra verbose logging; model override optional.
	ModeDebug
)

// String returns the settings.json mode name.
func (m Mode) String() string {
	switch m {
	case ModePlan:
		return "plan"
	case ModeAsk:
		return "ask"
	case ModeDebug:
		return "debug"
	default:
		return "agent"
	}
}

// ParseMode converts a user-facing mode name to Mode.
func ParseMode(name string) (Mode, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "agent", "default", "normal":
		return ModeAgent, nil
	case "plan":
		return ModePlan, nil
	case "ask":
		return ModeAsk, nil
	case "debug":
		return ModeDebug, nil
	default:
		return ModeAgent, fmt.Errorf("unknown interaction mode %q (want agent, plan, ask, or debug)", name)
	}
}

// DefaultFromSettings returns the configured default mode or ModeAgent.
func DefaultFromSettings(s *config.Settings) Mode {
	if s == nil || s.Sagittarius == nil {
		return ModeAgent
	}
	m, err := ParseMode(s.Sagittarius.DefaultMode)
	if err != nil {
		return ModeAgent
	}
	return m
}

// State tracks the active interaction mode for a session (thread-safe).
type State struct {
	mu   sync.RWMutex
	mode Mode
}

// NewState constructs session mode state with an initial mode.
func NewState(initial Mode) *State {
	return &State{mode: initial}
}

// Mode returns the active interaction mode.
func (s *State) Mode() Mode {
	if s == nil {
		return ModeAgent
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mode
}

// SetMode updates the active interaction mode.
func (s *State) SetMode(mode Mode) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.mode = mode
	s.mu.Unlock()
}

// ResolveModel selects the model for the main agent loop.
// Fallback: modes.<mode>.model → sagittarius.defaultModel → providerDefault.
func ResolveModel(mode Mode, cfg *config.SagittariusSettings, providerDefault string) string {
	if m := modeModel(cfg, mode); m != "" {
		return m
	}
	if cfg != nil && strings.TrimSpace(cfg.DefaultModel) != "" {
		return strings.TrimSpace(cfg.DefaultModel)
	}
	return strings.TrimSpace(providerDefault)
}

// ResolveSubagentModel selects a model for a named subagent.
// Fallback: subagents.<name>.model → subagents.default.model → defaultModel → providerDefault.
func ResolveSubagentModel(name string, cfg *config.SagittariusSettings, providerDefault string) string {
	name = strings.TrimSpace(name)
	if cfg != nil && cfg.Subagents != nil {
		if entry, ok := cfg.Subagents.Named[name]; ok {
			if m := strings.TrimSpace(entry.Model); m != "" {
				return m
			}
		}
		if m := strings.TrimSpace(cfg.Subagents.Default.Model); m != "" {
			return m
		}
	}
	if cfg != nil && strings.TrimSpace(cfg.DefaultModel) != "" {
		return strings.TrimSpace(cfg.DefaultModel)
	}
	return strings.TrimSpace(providerDefault)
}

// SystemPromptSuffix returns an optional suffix for the active mode.
func SystemPromptSuffix(mode Mode, cfg *config.SagittariusSettings) string {
	mc := modeConfig(cfg, mode)
	if mc == nil {
		return ""
	}
	return strings.TrimSpace(mc.SystemPromptSuffix)
}

// LogModeSelection emits verbose diagnostics when debug mode is active.
func LogModeSelection(mode Mode, model, providerDefault string) {
	if mode != ModeDebug {
		return
	}
	log.Default.Info("interaction mode",
		"mode", mode.String(),
		"resolved_model", model,
		"provider_default", providerDefault,
	)
}

func modeModel(cfg *config.SagittariusSettings, mode Mode) string {
	mc := modeConfig(cfg, mode)
	if mc == nil {
		return ""
	}
	return strings.TrimSpace(mc.Model)
}

func modeConfig(cfg *config.SagittariusSettings, mode Mode) *config.SagittariusModeConfig {
	if cfg == nil || cfg.Modes == nil {
		return nil
	}
	switch mode {
	case ModePlan:
		return cfg.Modes.Plan
	case ModeAsk:
		return cfg.Modes.Ask
	case ModeDebug:
		return cfg.Modes.Debug
	case ModeAgent:
		return cfg.Modes.Agent
	default:
		return nil
	}
}

// CycleMode advances agent → plan → ask → debug → agent.
func CycleMode(current Mode) Mode {
	switch current {
	case ModeAgent:
		return ModePlan
	case ModePlan:
		return ModeAsk
	case ModeAsk:
		return ModeDebug
	default:
		return ModeAgent
	}
}

// DescribeMode returns a short human-readable summary for /mode show.
func DescribeMode(mode Mode, model string) string {
	return fmt.Sprintf("Interaction mode: %s (model: %s)", mode.String(), model)
}
