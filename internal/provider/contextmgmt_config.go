package provider

import "github.com/undeadindustries/sagittarius/internal/config"

// DefaultLocalContextLimit is the assumed context window (in tokens) for an
// openai-chat provider that does not declare one. It mirrors the fork's
// getLocalContextLimit fallback.
const DefaultLocalContextLimit = 32_768

// ContextManagementConfig holds the resolved Phase 11 context-management knobs
// for the active provider. Zero-valued fraction/threshold fields mean "use the
// contextmgmt package default" — callers must not treat 0 as a literal value.
type ContextManagementConfig struct {
	// Enabled is true only when the active provider uses openai-chat wire
	// format (fork isLocalMode). Every defense is gated on this.
	Enabled bool

	// ContextLimit is the model context window in tokens.
	ContextLimit int

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
// overrides. It never errors: on any resolution failure it returns a disabled,
// default-populated config so callers degrade to a pure pass-through.
func ResolveContextManagement(settings *config.Settings) ContextManagementConfig {
	cm := ContextManagementConfig{
		ContextLimit:             DefaultLocalContextLimit,
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
	cm.Enabled = endpoint.WireFormat == config.WireFormatOpenAIChat

	providerID := endpoint.ProviderID
	cm.ContextLimit = resolveContextLimit(settings, providerID, cm.ContextLimit)
	applyInstanceContextKnobs(&cm, providerInstance(settings, providerID))
	return cm
}

func resolveContextLimit(settings *config.Settings, providerID string, fallback int) int {
	inst := providerInstance(settings, providerID)
	if inst != nil && inst.ContextLimit != nil && *inst.ContextLimit > 0 {
		return *inst.ContextLimit
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
