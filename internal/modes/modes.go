// Package modes implements Sagittarius interaction modes and model routing.
//
// Interaction modes (plan, ask, debug, agent) control model selection,
// tool restrictions (plan/ask read-only gates), and optional system-prompt suffixes.
// They are orthogonal to fork approval-mode confirmation policy (default/autoEdit/yolo).
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
//
// Resolution order (first non-empty wins):
//  1. sagittarius.modes.<mode>.model  — the per-mode override always wins
//  2. sagittarius.defaultModels[providerID] — provider-scoped default
//  3. providerDefault — the provider instance / built-in default
//  4. sagittarius.defaultModel — legacy single global default (last resort)
//
// Putting providerDefault ahead of the legacy global default stops a global
// default from clobbering the active provider's configured model when switching
// providers, while the per-mode override still trumps everything below it.
func ResolveModel(mode Mode, cfg *config.SagittariusSettings, providerID, providerDefault string) string {
	if m := modeModel(cfg, mode); m != "" {
		return m
	}
	if m := providerScopedDefault(cfg, providerID); m != "" {
		return m
	}
	if m := strings.TrimSpace(providerDefault); m != "" {
		return m
	}
	return globalDefault(cfg)
}

// ResolveUtilityModel returns the model for an auxiliary role (context
// compression, tool-utility calls, subagents), defaulting to the live
// mode-resolved model unless a role-specific override is configured. This
// mirrors how mode overrides work: an explicit setting wins, otherwise the role
// follows whatever model the main loop is currently using.
func ResolveUtilityModel(override, liveModel string) string {
	if o := strings.TrimSpace(override); o != "" {
		return o
	}
	return strings.TrimSpace(liveModel)
}

// ResolveCompressionModel selects the model for context compression /
// summarization: sagittarius.compression.model overrides, else the live model.
func ResolveCompressionModel(cfg *config.SagittariusSettings, liveModel string) string {
	return ResolveUtilityModel(utilityModel(cfg, roleCompression), liveModel)
}

// ResolveToolsModel selects the model for tool-utility calls:
// sagittarius.tools.model overrides, else the live model.
func ResolveToolsModel(cfg *config.SagittariusSettings, liveModel string) string {
	return ResolveUtilityModel(utilityModel(cfg, roleTools), liveModel)
}

// ResolveSubagentModel selects a model for a named subagent.
//
// Resolution order (first non-empty wins):
//  1. sagittarius.subagents.<name>.model
//  2. sagittarius.subagents.default.model
//  3. liveModel — the live mode-resolved model the main loop is using
//
// The live model already encodes the full mode chain (mode override →
// defaultModels → provider default → legacy default), so a subagent without its
// own override simply follows the active model.
func ResolveSubagentModel(name string, cfg *config.SagittariusSettings, liveModel string) string {
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
	return ResolveUtilityModel("", liveModel)
}

type utilityRole int

const (
	roleCompression utilityRole = iota
	roleTools
)

func utilityModel(cfg *config.SagittariusSettings, role utilityRole) string {
	if cfg == nil {
		return ""
	}
	var uc *config.SagittariusUtilityConfig
	switch role {
	case roleCompression:
		uc = cfg.Compression
	case roleTools:
		uc = cfg.Tools
	}
	if uc == nil {
		return ""
	}
	return strings.TrimSpace(uc.Model)
}

// providerScopedDefault returns sagittarius.defaultModels[providerID], tolerating
// both the canonical id and the short display alias (e.g. "gemini").
func providerScopedDefault(cfg *config.SagittariusSettings, providerID string) string {
	if cfg == nil || len(cfg.DefaultModels) == 0 {
		return ""
	}
	id := strings.TrimSpace(providerID)
	if id == "" {
		return ""
	}
	if m := strings.TrimSpace(cfg.DefaultModels[id]); m != "" {
		return m
	}
	if norm := config.NormalizeProviderID(id); norm != id {
		if m := strings.TrimSpace(cfg.DefaultModels[norm]); m != "" {
			return m
		}
	}
	return ""
}

func globalDefault(cfg *config.SagittariusSettings) string {
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.DefaultModel)
}

// SystemPromptSuffix returns an optional suffix for the active mode. Built-in
// read-only guidance applies when no custom suffix is configured.
func SystemPromptSuffix(mode Mode, cfg *config.SagittariusSettings) string {
	custom := ""
	mc := modeConfig(cfg, mode)
	if mc != nil {
		custom = strings.TrimSpace(mc.SystemPromptSuffix)
	}
	builtin := builtinModeSuffix(mode)
	switch {
	case builtin != "" && custom != "":
		return builtin + "\n\n" + custom
	case custom != "":
		return custom
	default:
		return builtin
	}
}

func builtinModeSuffix(mode Mode) string {
	switch mode {
	case ModePlan:
		return "**CRITICAL: Plan mode ACTIVE** — you are in a read-only planning phase.\n\n" +
			"STRICTLY FORBIDDEN: modifying project source files, running shell commands, or any write except plan files under `docs/plans/`. " +
			"Do not use shell to manipulate files (no sed, tee, echo redirects, etc.). " +
			"This constraint overrides other instructions, including direct edit requests.\n\n" +
			"Allowed: read, search, and explore. You may write only to `docs/plans/`. " +
			"Switch to agent mode to implement."
	case ModeAsk:
		return "**CRITICAL: Ask mode ACTIVE** — read-only Q&A.\n\n" +
			"STRICTLY FORBIDDEN: writing files, running shell commands, or making any system changes. " +
			"Use read-only tools (`read_file`, `grep_search`, `list_directory`) to research and answer. " +
			"This constraint overrides other instructions.\n\n" +
			"Switch to agent mode to implement changes."
	default:
		return ""
	}
}

// LogModeSelection emits verbose diagnostics when debug mode is active.
func LogModeSelection(mode Mode, model, providerID, providerDefault string) {
	if mode != ModeDebug {
		return
	}
	log.Default.Info("interaction mode",
		"mode", mode.String(),
		"resolved_model", model,
		"provider_id", providerID,
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
