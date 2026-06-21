package contextmgmt

import (
	"context"
	"log/slog"
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

	// Summarize performs compression summarization. Nil disables compression
	// (masking and ejection still run).
	Summarize Summarizer
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
	// hasFailedCompression records that a non-forced compression previously
	// inflated the token count. Once set, later non-forced compressions skip
	// summarization (truncation only) to avoid repeated failures and cost,
	// mirroring the fork's hasFailedCompressionAttempt. PrepareTurn runs one
	// turn at a time on the runner goroutine, so no lock is required.
	hasFailedCompression bool
}

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
	return m.applyBudgetCompression(ctx, history, turnIndex)
}

func (m *Manager) applyEjection(history []Message) []Message {
	if !m.cfg.EjectionEnabled || m.cfg.WriteFileToolName == "" {
		return history
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
		ContextLimit:       m.cfg.ContextLimit,
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
			ContextLimit:           m.cfg.ContextLimit,
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
	res, err := m.compressor.Compress(ctx, CompressOptions{
		History:            history,
		Force:              budgetTriggered,
		OriginalTokenCount: historyTokens,
		Threshold:          threshold,
		EffectiveLimit:     m.cfg.ContextLimit,
		PreserveFraction:   preserveFraction,
		HasFailedAttempt:   m.hasFailedCompression,
	})
	if err != nil {
		m.logger.Warn("context: compression failed", "error", err)
		return history, err
	}

	// Latch the failure flag when a non-forced summarization inflated the token
	// count, matching the fork: hasFailed = hasFailed || !force.
	if res.Info.Status == CompressionFailedInflatedTokenCount && !budgetTriggered {
		m.hasFailedCompression = true
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
