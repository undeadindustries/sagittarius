package contextmgmt

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// ManagerConfig configures a Manager. The agent runner builds one per provider
// swap; it is enabled only for the openai-chat wire format (AD-015).
type ManagerConfig struct {
	// Enabled gates every defense. False (e.g. gemini / openai-responses) makes
	// PrepareTurn a pure pass-through.
	Enabled bool
	// ContextLimit is the local model context window in tokens.
	ContextLimit int
	// SessionID keys adaptive state and offload directories.
	SessionID string
	// OutputDir is the base directory for offloaded tool output.
	OutputDir string

	// MaskingEnabled toggles tool-output masking.
	MaskingEnabled bool
	// MaskingProtectionFraction reserves a fraction of ContextLimit as protected.
	MaskingProtectionFraction float64
	// MaskingPrunableFraction buffers a fraction before masking fires.
	MaskingPrunableFraction float64
	// MaskingProtectLatestTurn skips the latest turn when true.
	MaskingProtectLatestTurn bool

	// EjectionEnabled toggles write-file content ejection.
	EjectionEnabled bool
	// EjectionMinAgeTurns is the minimum age before a write_file call is ejected.
	EjectionMinAgeTurns int
	// EjectionMinTokensPerCall skips small write_file payloads.
	EjectionMinTokensPerCall int

	// BudgetEnabled toggles proactive pre-turn budget compression.
	BudgetEnabled bool
	// ReservedResponseTokens is reserved for the model reply in budget math.
	ReservedResponseTokens int
	// ProactiveCompressAt is the projected-usage trigger fraction.
	ProactiveCompressAt float64

	// AdaptiveEnabled toggles adaptive threshold tightening.
	AdaptiveEnabled bool
	// AdaptiveCooldownTurns overrides the default cooldown when > 0.
	AdaptiveCooldownTurns int
	// AdaptiveFloor overrides the default floor when > 0.
	AdaptiveFloor float64

	// CompressionThreshold is the base compression trigger fraction.
	CompressionThreshold float64
	// CompressionThresholdUserSet disables adaptation when the user pinned it.
	CompressionThresholdUserSet bool
	// PreserveFraction is the fraction of recent history kept raw after compression.
	PreserveFraction float64

	// WriteFileToolName identifies write_file calls for ejection.
	WriteFileToolName string
	// ShellToolName enables shell-aware masking previews.
	ShellToolName string

	// OnWillCompress callback is invoked right before history is compressed.
	OnWillCompress func(ctx context.Context, trigger string)

	// Summarize performs compression summarization. Nil disables compression
	// (masking and ejection still run).
	Summarize Summarizer
	// EstimateSafetyFactor scales ContextLimit for budget math so the chars/4
	// estimator cannot fire thresholds after the provider's real tokenizer
	// has already overflowed. Zero (and values outside (0,1]) use
	// defaultEstimateSafetyFactor. ContextLimit() still returns the true window.
	EstimateSafetyFactor float64
	// Logger receives structured debug/warn logs; defaults to slog.Default.
	Logger *slog.Logger
}

// Manager orchestrates the local-context defenses for one provider session.
type Manager struct {
	cfg        ManagerConfig
	masker     *Masker
	compressor *Compressor
	adaptive   *AdaptiveTracker
	exempt     map[string]bool
	logger     *slog.Logger

	latchMu              sync.Mutex
	hasFailedCompression bool
}

const defaultEstimateSafetyFactor = 0.85

// NewManager builds a Manager from cfg. A disabled config yields a pass-through.
func NewManager(cfg ManagerConfig) *Manager {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	exempt := DefaultExemptTools()

	m := &Manager{
		cfg:      cfg,
		adaptive: NewAdaptiveTracker(),
		exempt:   exempt,
		logger:   logger,
	}
	if !cfg.Enabled {
		return m
	}

	if cfg.MaskingEnabled {
		m.masker = &Masker{
			OutputDir:     cfg.OutputDir,
			SessionID:     cfg.SessionID,
			ExemptTools:   exempt,
			ShellToolName: cfg.ShellToolName,
		}
	}
	if cfg.Summarize != nil {
		m.compressor = &Compressor{
			Summarize:         cfg.Summarize,
			OutputDir:         cfg.OutputDir,
			CompressionPrompt: DefaultCompressionPrompt,
		}
	}
	return m
}

// Enabled reports whether the manager applies defenses. A nil or pass-through
// manager returns false.
func (m *Manager) Enabled() bool {
	return m != nil && m.cfg.Enabled
}

// ContextLimit returns the configured context window in tokens, or 0 when the
// manager is nil/disabled. The TUI footer uses it to show a usage percentage.
func (m *Manager) ContextLimit() int {
	if m == nil || !m.cfg.Enabled {
		return 0
	}
	return m.cfg.ContextLimit
}

// BudgetLimit returns ContextLimit scaled by EstimateSafetyFactor. Callers that
// decide whether history fits (compression, masking, ejection, pre-flight
// truncation) must use this, not ContextLimit, so a chars/4 underestimate
// cannot walk the request past the provider's real window. The footer gauge
// still reads ContextLimit.
func (m *Manager) BudgetLimit() int {
	if m == nil || !m.cfg.Enabled {
		return 0
	}
	limit := m.cfg.ContextLimit
	if limit <= 0 {
		return 0
	}
	factor := m.cfg.EstimateSafetyFactor
	if factor <= 0 || factor > 1 {
		factor = defaultEstimateSafetyFactor
	}
	return int(float64(limit) * factor)
}

// MessageBudget calculates the available token budget for messages after
// subtracting system/tool overhead and reserved response tokens from BudgetLimit.
// It is clamped to at least 1 so pre-flight truncation always receives a valid target.
func (m *Manager) MessageBudget(overheadTokens int) int {
	if m == nil || !m.cfg.Enabled {
		return 0
	}
	limit := m.BudgetLimit()
	if limit <= 0 {
		return 0
	}
	reserved := m.cfg.ReservedResponseTokens
	if reserved < 0 {
		reserved = 0
	}
	budget := limit - overheadTokens - reserved
	if budget < 1 {
		return 1
	}
	return budget
}

// ResetCompressionFailure clears the summarizer-failure latch so the next
// PrepareTurn may attempt summarization again (manual /compress or a model switch).
func (m *Manager) ResetCompressionFailure() {
	if m == nil {
		return
	}
	m.latchMu.Lock()
	m.hasFailedCompression = false
	m.latchMu.Unlock()
}

func (m *Manager) getFailedCompression() bool {
	if m == nil {
		return false
	}
	m.latchMu.Lock()
	defer m.latchMu.Unlock()
	return m.hasFailedCompression
}

func (m *Manager) setFailedCompression(val bool) {
	if m == nil {
		return
	}
	m.latchMu.Lock()
	m.hasFailedCompression = val
	m.latchMu.Unlock()
}

// CompressionAvailable reports whether manual compression can run: the manager
// must be enabled and have a configured summarizer. It is false for nil or
// disabled managers (gemini-native and openai-responses paths).
func (m *Manager) CompressionAvailable() bool {
	return m != nil && m.cfg.Enabled && m.compressor != nil
}

// ForceCompress summarizes history immediately, bypassing the budget and
// threshold checks, and returns the (possibly unchanged) history plus the
// compression info. It is a no-op returning the original history and a
// CompressionNoOp status when compression is unavailable or history is empty.
func (m *Manager) ForceCompress(ctx context.Context, history []Message) ([]Message, CompressionInfo, error) {
	if !m.CompressionAvailable() || len(history) == 0 {
		return history, CompressionInfo{Status: CompressionNoOp}, nil
	}
	m.ResetCompressionFailure()
	preserveFraction := m.cfg.PreserveFraction
	if preserveFraction <= 0 {
		preserveFraction = DefaultLocalPreserveFraction
	}
	original := EstimateTokens(flattenParts(history))
	opts := CompressOptions{
		History:            history,
		Force:              true,
		OriginalTokenCount: original,
		EffectiveLimit:     m.BudgetLimit(),
		PreserveFraction:   preserveFraction,
	}
	if WillCompress(opts) && m.cfg.OnWillCompress != nil {
		m.cfg.OnWillCompress(ctx, "manual")
	}
	res, err := m.compressor.Compress(ctx, opts)
	if err != nil {
		return history, res.Info, fmt.Errorf("manual compression failed: %w", err)
	}
	if res.NewHistory != nil {
		return res.NewHistory, res.Info, nil
	}
	return history, res.Info, nil
}

// PrepareTurn applies ejection, masking, and (when over budget) compression to
// history before a generate request. It returns the transformed history and a
// best-effort error; callers should proceed with the returned history even when
// err is non-nil (the defenses degrade gracefully). It is a no-op when disabled.
func (m *Manager) PrepareTurn(ctx context.Context, history []Message, turnIndex int) ([]Message, error) {
	if m == nil || !m.cfg.Enabled || len(history) == 0 {
		return history, nil
	}

	history = m.applyEjection(history)
	history = m.applyMasking(history)
	history, err := m.applyBudgetCompression(ctx, history, turnIndex)
	return history, err
}

// ejectionTriggerFraction is the share of the context limit that must be in use
// before stale write_file content is ejected. Ejecting earlier wastes the
// model's access to its own recent writes for no token benefit, so ejection only
// fires once the conversation is genuinely approaching the budget (it still runs
// before threshold compression, which triggers higher, at ProactiveCompressAt).
const ejectionTriggerFraction = 0.6

func (m *Manager) applyEjection(history []Message) []Message {
	if !m.cfg.EjectionEnabled || m.cfg.WriteFileToolName == "" {
		return history
	}
	// Only eject under real budget pressure. With headroom, retaining write_file
	// content keeps the model's recent writes visible and avoids needlessly
	// mutating its prior tool calls.
	if m.cfg.ContextLimit > 0 {
		historyTokens := EstimateTokens(flattenParts(history))
		if float64(historyTokens) < ejectionTriggerFraction*float64(m.BudgetLimit()) {
			return history
		}
	}
	res := EjectStaleWriteFileContent(history, WriteFileEjectionOptions{
		WriteFileToolName: m.cfg.WriteFileToolName,
		ExemptTools:       m.exempt,
		ProtectLatestTurn: true,
		MinAgeTurns:       m.cfg.EjectionMinAgeTurns,
		MinTokensPerCall:  m.cfg.EjectionMinTokensPerCall,
	})
	if res.EjectedCount > 0 {
		m.logger.Debug("context: ejected stale write_file content",
			"ejected", res.EjectedCount, "tokensSaved", res.TokensSaved)
	}
	return res.NewHistory
}

func (m *Manager) applyMasking(history []Message) []Message {
	if m.masker == nil {
		return history
	}
	protectionFraction := m.cfg.MaskingProtectionFraction
	if protectionFraction <= 0 {
		protectionFraction = DefaultLocalMaskingProtectionFraction
	}
	prunableFraction := m.cfg.MaskingPrunableFraction
	if prunableFraction <= 0 {
		prunableFraction = DefaultLocalMaskingPrunableFraction
	}
	cfg := GetLocalMaskingDefaults(LocalMaskingSettings{
		ContextLimit:       m.BudgetLimit(),
		Enabled:            true,
		ProtectionFraction: protectionFraction,
		PrunableFraction:   prunableFraction,
		ProtectLatestTurn:  m.cfg.MaskingProtectLatestTurn,
	})
	res, err := m.masker.Mask(history, cfg)
	if err != nil {
		m.logger.Warn("context: tool-output masking failed", "error", err)
		return history
	}
	if res.MaskedCount > 0 {
		m.logger.Debug("context: masked tool outputs",
			"masked", res.MaskedCount, "tokensSaved", res.TokensSaved)
	}
	return res.NewHistory
}

const minCompressionReductionFraction = 0.1 // OpenCode's loop guard, issue #27924

func unproductive(info CompressionInfo) bool {
	switch info.Status {
	case CompressionFailedEmptySummary, CompressionFailedInflatedTokenCount:
		return true
	case Compressed:
		saved := info.OriginalTokenCount - info.NewTokenCount
		return float64(saved) < minCompressionReductionFraction*float64(info.OriginalTokenCount)
	}
	return false
}

func (m *Manager) applyBudgetCompression(ctx context.Context, history []Message, turnIndex int) ([]Message, error) {
	if m.compressor == nil {
		return history, nil
	}

	historyTokens := EstimateTokens(flattenParts(history))

	// The pre-turn budget layer only forces compression early; the normal
	// threshold check (inside Compress) still runs when the budget does not
	// trigger, so threshold-based compression is never skipped.
	budgetTriggered := false
	if m.cfg.BudgetEnabled {
		assessment := AssessTurnBudget(PreTurnBudgetInput{
			CurrentHistoryTokens:   historyTokens,
			ContextLimit:           m.BudgetLimit(),
			ReservedResponseTokens: m.cfg.ReservedResponseTokens,
			ProactiveCompressAt:    m.cfg.ProactiveCompressAt,
		})
		budgetTriggered = assessment.ShouldCompressFirst
	}

	preserveFraction := m.cfg.PreserveFraction
	if preserveFraction <= 0 {
		preserveFraction = DefaultLocalPreserveFraction
	}
	threshold := m.effectiveThreshold(turnIndex)
	opts := CompressOptions{
		History:            history,
		BudgetTriggered:    budgetTriggered,
		OriginalTokenCount: historyTokens,
		Threshold:          threshold,
		EffectiveLimit:     m.BudgetLimit(),
		PreserveFraction:   preserveFraction,
		HasFailedAttempt:   m.getFailedCompression(),
	}
	if WillCompress(opts) && m.cfg.OnWillCompress != nil {
		m.cfg.OnWillCompress(ctx, "auto")
	}
	res, err := m.compressor.Compress(ctx, opts)
	if err != nil {
		m.setFailedCompression(true)
		m.logger.Warn("context: compression failed", "error", err)
		return history, err
	}

	// Latch the failure flag on any unproductive outcome when not manual/forced.
	if unproductive(res.Info) && !opts.Force {
		m.setFailedCompression(true)
	}

	if m.cfg.AdaptiveEnabled {
		m.adaptive.RecordCompressionResult(m.cfg.SessionID,
			res.Info.OriginalTokenCount, res.Info.NewTokenCount, turnIndex)
	}
	if res.NewHistory != nil {
		m.logger.Debug("context: compressed history",
			"before", res.Info.OriginalTokenCount, "after", res.Info.NewTokenCount)
		return res.NewHistory, nil
	}
	return history, nil
}

func (m *Manager) effectiveThreshold(turnIndex int) float64 {
	base := m.cfg.CompressionThreshold
	if base <= 0 {
		base = DefaultLocalCompressionThreshold
	}
	if !m.cfg.AdaptiveEnabled {
		return base
	}
	return m.adaptive.EffectiveCompressionThreshold(base, EffectiveThresholdOptions{
		SessionID:           m.cfg.SessionID,
		CurrentTurnIndex:    turnIndex,
		UserOverridePresent: m.cfg.CompressionThresholdUserSet,
		CooldownTurns:       m.cfg.AdaptiveCooldownTurns,
		Floor:               m.cfg.AdaptiveFloor,
	})
}
