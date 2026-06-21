package contextmgmt

import "math"

// Tool-output masking defaults ported from the fork
// (toolOutputMaskingService.ts + localMaskingDefaults.ts).
const (
	// DefaultToolProtectionThreshold is the upstream protection window in tokens.
	DefaultToolProtectionThreshold = 50_000
	// DefaultMinPrunableTokensThreshold is the upstream prunable buffer in tokens.
	DefaultMinPrunableTokensThreshold = 30_000
	// DefaultProtectLatestTurn keeps the most recent turn unmasked by default.
	DefaultProtectLatestTurn = true

	// DefaultLocalMaskingProtectionFraction reserves this fraction of the local
	// context limit as the never-masked protection window (0.15 of 32K ≈ 4800).
	DefaultLocalMaskingProtectionFraction = 0.15
	// DefaultLocalMaskingPrunableFraction is the prunable buffer fraction that
	// must accumulate before masking fires (0.10 of 32K ≈ 3200).
	DefaultLocalMaskingPrunableFraction = 0.10

	// minProtectionTokens floors the protection window so a tiny/misconfigured
	// context limit cannot collapse it to zero (which would mask everything).
	minProtectionTokens = 2_000
	// minPrunableTokens floors the prunable buffer for the same reason.
	minPrunableTokens = 1_000

	// maskingFractionMin and maskingFractionMax clamp configured fractions.
	maskingFractionMin = 0.05
	maskingFractionMax = 0.5
)

// ToolOutputMaskingConfig holds the thresholds that drive ToolOutputMaskingService.
type ToolOutputMaskingConfig struct {
	// ProtectionThresholdTokens is the newest-N-tokens protection window.
	ProtectionThresholdTokens int
	// MinPrunableThresholdTokens is the batch trigger for masking.
	MinPrunableThresholdTokens int
	// ProtectLatestTurn skips the most recent history entry when true.
	ProtectLatestTurn bool
}

// LocalMaskingSettings is the subset of provider/local config that
// GetLocalMaskingDefaults needs. Kept as a struct so it unit-tests without a
// full Settings instance (mirrors the fork's LocalMaskingConfigLike).
type LocalMaskingSettings struct {
	// ContextLimit is the local model context window in tokens.
	ContextLimit int
	// Enabled reports whether local tool-output masking is active.
	Enabled bool
	// ProtectionFraction is the fraction of ContextLimit kept as protection.
	ProtectionFraction float64
	// PrunableFraction is the fraction of ContextLimit buffered before masking.
	PrunableFraction float64
	// ProtectLatestTurn skips the latest turn when true.
	ProtectLatestTurn bool
}

// GetLocalMaskingDefaults computes ToolOutputMaskingConfig values scaled to the
// local context window. Callers should invoke it only when masking is enabled
// in local (openai-chat) mode. When the context limit is unusable it falls back
// to the upstream cloud defaults so masking degrades safely.
func GetLocalMaskingDefaults(cfg LocalMaskingSettings) ToolOutputMaskingConfig {
	limit := cfg.ContextLimit
	if limit <= 0 {
		return ToolOutputMaskingConfig{
			ProtectionThresholdTokens:  DefaultToolProtectionThreshold,
			MinPrunableThresholdTokens: DefaultMinPrunableTokensThreshold,
			ProtectLatestTurn:          DefaultProtectLatestTurn,
		}
	}

	protectionFraction := clampFraction(cfg.ProtectionFraction, maskingFractionMin, maskingFractionMax)
	prunableFraction := clampFraction(cfg.PrunableFraction, maskingFractionMin, maskingFractionMax)

	protection := maxInt(minProtectionTokens, int(math.Floor(float64(limit)*protectionFraction)))
	prunable := maxInt(minPrunableTokens, int(math.Floor(float64(limit)*prunableFraction)))

	return ToolOutputMaskingConfig{
		ProtectionThresholdTokens:  protection,
		MinPrunableThresholdTokens: prunable,
		ProtectLatestTurn:          cfg.ProtectLatestTurn,
	}
}

func clampFraction(value, min, max float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return min
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
