package provider

import (
	"strings"

	"github.com/undeadindustries/sagittarius/internal/config"
)

// DefaultLocalContextLimit is the assumed context window (in tokens) for an
// openai-chat provider that does not declare one. It mirrors the fork's
// getLocalContextLimit fallback.
const DefaultLocalContextLimit = 32_768

const (
	// anthropicDenseTokenizerScale is the cold-start multiplier applied to
	// chars/4 estimates for Anthropic models using the Opus 4.7+/5.x tokenizer
	// (~2.5 chars/token vs 4 chars/token) before live usage calibrates it.
	anthropicDenseTokenizerScale = 1.5
	// maxReservedResponseDivisor caps ReservedResponseTokens at 1/4 of
	// ContextLimit so a small manual window is never starved by max_tokens.
	maxReservedResponseDivisor = 4
)

// ContextManagementConfig holds the resolved context-management knobs
// for the active provider. Zero-valued fraction/threshold fields mean "use the
// contextmgmt package default" — callers must not treat 0 as a literal value.
type ContextManagementConfig struct {
	// Enabled is true when the active provider uses a stateless wire format
	// (openai-chat, gemini, or anthropic). Every defense is gated on this.
	// openai-responses is disabled because it chains turns server-side via
	// previous_response_id, so mutating history client-side desynchronizes the chain.
	Enabled bool

	// Adaptive is true when adaptive threshold tightening is enabled. This is
	// tuned for small local windows and is disabled on large-context endpoints.
	Adaptive bool

	// ContextLimit is the model context window in tokens.
	ContextLimit int
	// ReservedResponseTokens is the output-token reservation subtracted from
	// the message budget (0 means use the caller's default). Anthropic sets
	// this from the model's max_tokens because Anthropic counts input +
	// max_tokens against the context window.
	ReservedResponseTokens int
	// EstimateScale is the cold-start multiplier applied to chars/4 token
	// estimates before provider-reported Usage.InputTokens calibrates the
	// ratio (1.0 for standard ~4 chars/token models, 1.5 for Opus 4.7+/5.x).
	EstimateScale float64

	// CompressionThreshold is the user-configured base trigger fraction (0 if unset).
	CompressionThreshold float64
	// CompressionThresholdUserSet is true when the user pinned the threshold,
	// which disables adaptive tightening.
	CompressionThresholdUserSet bool
	// PreserveFraction is the fraction of recent history kept raw (0 if unset).
	PreserveFraction float64

	// MaskingEnabled toggles tool-output masking (default true).
	MaskingEnabled bool
	// MaskingProtectionFraction reserves a fraction of the window (0 if unset).
	MaskingProtectionFraction float64
	// MaskingPrunableFraction buffers a fraction before masking (0 if unset).
	MaskingPrunableFraction float64
	// MaskingProtectLatestTurn skips the most recent turn (default true).
	MaskingProtectLatestTurn bool
}

// ResolveContextManagement resolves the context-management knobs for the active
// provider, merging built-in defaults, custom-provider defaults, and per-instance
// overrides. liveModel is the mode-resolved model id at call time; pass an empty
// string to fall back to the endpoint's persisted default model. It never errors:
// on any resolution failure it returns a disabled, default-populated config so
// callers degrade to a pure pass-through.
func ResolveContextManagement(settings *config.Settings, liveModel string) ContextManagementConfig {
	cm := ContextManagementConfig{
		ContextLimit:             DefaultLocalContextLimit,
		EstimateScale:            1.0,
		MaskingEnabled:           true,
		MaskingProtectLatestTurn: true,
	}
	if settings == nil {
		return cm
	}
	endpoint, err := ResolveEndpointConfig(settings)
	if err != nil {
		return cm
	}
	cm.Enabled = endpoint.WireFormat == config.WireFormatOpenAIChat ||
		endpoint.WireFormat == config.WireFormatGemini ||
		endpoint.WireFormat == config.WireFormatAnthropic
	cm.Adaptive = endpoint.WireFormat == config.WireFormatOpenAIChat

	providerID := endpoint.ProviderID
	// Use the caller-supplied live model for per-model lookup; fall back to the
	// endpoint's persisted default when the caller passes "".
	modelForLookup := liveModel
	if modelForLookup == "" {
		modelForLookup = endpoint.Model
	}
	fallbackLimit := config.ProviderDefaultContextLimit(endpoint.ProviderID)
	if fallbackLimit == 0 {
		fallbackLimit = DefaultLocalContextLimit
	}
	cm.ContextLimit = resolveContextLimit(settings, providerID, modelForLookup, fallbackLimit)
	if endpoint.WireFormat == config.WireFormatAnthropic {
		cm.ReservedResponseTokens = resolveAnthropicReservedResponseTokens(settings, providerID, modelForLookup, cm.ContextLimit)
		if isAnthropicDenseTokenizerModel(modelForLookup) {
			cm.EstimateScale = anthropicDenseTokenizerScale
		}
	}
	applyInstanceContextKnobs(&cm, providerInstance(settings, providerID))
	if !cm.CompressionThresholdUserSet {
		if endpoint.WireFormat == config.WireFormatGemini {
			cm.CompressionThreshold = 0.5
		} else {
			// Seed an unpinned threshold from the system-prompt variant so lite
			// presets compress earlier. UserSet stays false so adaptive tightening
			// still applies on top of this base.
			variant := config.ResolveVariant(settings, providerID, modelForLookup)
			cm.CompressionThreshold = config.VariantCompressionThreshold(variant)
		}
	}
	return cm
}

func resolveContextLimit(settings *config.Settings, providerID, model string, fallback int) int {
	inst := providerInstance(settings, providerID)
	var modelLimit, instLimit int
	if inst != nil {
		// Always consider a per-model pin when present. A stale provider-wide
		// pin from a larger sibling (the OpenRouter 1M / Kimi case) must not
		// win: take the smaller of the two so an over-large limit cannot
		// produce hard 400s.
		if model != "" {
			if mc, ok := config.LookupModelConfig(inst, model); ok && mc.ContextLimit != nil && *mc.ContextLimit > 0 {
				modelLimit = *mc.ContextLimit
			}
		}
		if inst.ContextLimit != nil && *inst.ContextLimit > 0 {
			instLimit = *inst.ContextLimit
		}
	}
	switch {
	case modelLimit > 0 && instLimit > 0:
		return min(modelLimit, instLimit)
	case modelLimit > 0:
		return modelLimit
	case instLimit > 0:
		return instLimit
	}
	if settings.Providers != nil {
		if custom, ok := settings.Providers.Custom[providerID]; ok {
			if custom.DefaultContextLimit != nil && *custom.DefaultContextLimit > 0 {
				return *custom.DefaultContextLimit
			}
		}
	}
	return fallback
}

func applyInstanceContextKnobs(cm *ContextManagementConfig, inst *config.ProviderInstanceConfig) {
	if inst == nil {
		return
	}
	if inst.CompressionThreshold != nil {
		cm.CompressionThreshold = *inst.CompressionThreshold
		cm.CompressionThresholdUserSet = true
	}
	if inst.PreserveFraction != nil {
		cm.PreserveFraction = *inst.PreserveFraction
	}
	if inst.ToolOutputMaskingEnabled != nil {
		cm.MaskingEnabled = *inst.ToolOutputMaskingEnabled
	}
	if inst.ToolOutputMaskingProtectionFraction != nil {
		cm.MaskingProtectionFraction = *inst.ToolOutputMaskingProtectionFraction
	}
	if inst.ToolOutputMaskingPrunableFraction != nil {
		cm.MaskingPrunableFraction = *inst.ToolOutputMaskingPrunableFraction
	}
	if inst.ToolOutputMaskingProtectLatestTurn != nil {
		cm.MaskingProtectLatestTurn = *inst.ToolOutputMaskingProtectLatestTurn
	}
}

func resolveAnthropicReservedResponseTokens(settings *config.Settings, providerID, model string, contextLimit int) int {
	maxOut := anthropicDefaultMaxTokens
	if inst := providerInstance(settings, providerID); inst != nil && model != "" {
		if mc, ok := config.LookupModelConfig(inst, model); ok && mc.MaxOutputTokens != nil && *mc.MaxOutputTokens > 0 {
			maxOut = *mc.MaxOutputTokens
		}
	}
	if contextLimit > 0 {
		if capTokens := contextLimit / maxReservedResponseDivisor; capTokens > 0 && maxOut > capTokens {
			maxOut = capTokens
		}
	}
	return maxOut
}

func isAnthropicDenseTokenizerModel(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	if i := strings.LastIndex(m, "/"); i >= 0 {
		m = m[i+1:]
	}
	for _, prefix := range []string{
		"claude-opus-5",
		"claude-sonnet-5",
		"claude-haiku-5",
		"claude-fable-5",
		"claude-mythos-5",
		"claude-opus-4-7",
		"claude-opus-4-8",
	} {
		if strings.HasPrefix(m, prefix) {
			return true
		}
	}
	return false
}
