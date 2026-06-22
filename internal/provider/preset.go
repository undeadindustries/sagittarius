package provider

import (
	"fmt"

	"github.com/undeadindustries/sagittarius/internal/config"
)

// PresetApplyResult describes the effect of applying a system-prompt preset so
// the wizard can show the user what changed. Temperature and compression are not
// persisted by the preset (they resolve dynamically from personality/variant and
// model family); the result reports the resolved defaults for the info message
// and whether the user has pinned an explicit override that takes precedence.
type PresetApplyResult struct {
	PresetID    string
	Label       string
	Personality string
	Variant     string
	// DefaultTemperature is the generic personality default (nil = let the
	// model/server decide). Model-family rules may still override per model.
	DefaultTemperature *float64
	// TemperaturePinned is true when the provider has an explicit temperature
	// override, which wins over the preset default.
	TemperaturePinned bool
	// CompressionThreshold is the variant default applied when unpinned.
	CompressionThreshold float64
	// CompressionPinned is true when the provider has an explicit
	// compressionThreshold override, which wins over the preset default.
	CompressionPinned bool
}

// ApplySystemPromptPreset writes a preset's personality and variant onto the
// provider instance. It does not overwrite the temperature or compression knobs:
// those resolve dynamically (personality/variant default unless the user pinned a
// value), so the result reports the effective defaults for an info message.
func ApplySystemPromptPreset(settings *config.Settings, providerID, presetID string) (PresetApplyResult, error) {
	if settings == nil {
		return PresetApplyResult{}, fmt.Errorf("apply system prompt preset: settings are required")
	}
	preset, ok := config.LookupPreset(presetID)
	if !ok {
		return PresetApplyResult{}, fmt.Errorf("apply system prompt preset: unknown preset %q", presetID)
	}
	canonical := config.NormalizeProviderID(providerID)
	cfg, err := ensureProviderInstance(settings, canonical)
	if err != nil {
		return PresetApplyResult{}, err
	}
	cfg.Personality = preset.Personality
	cfg.PromptMode = config.PromptMode(preset.Variant)
	if err := setProviderInstance(settings, canonical, cfg); err != nil {
		return PresetApplyResult{}, err
	}
	return PresetApplyResult{
		PresetID:             preset.ID,
		Label:                preset.Label,
		Personality:          preset.Personality,
		Variant:              preset.Variant,
		DefaultTemperature:   config.PersonalityDefaultTemperature(preset.Personality),
		TemperaturePinned:    cfg.Temperature != nil,
		CompressionThreshold: config.VariantCompressionThreshold(preset.Variant),
		CompressionPinned:    cfg.CompressionThreshold != nil,
	}, nil
}

// MaybeSetContextLimit applies a discovered context limit to a provider instance
// unless the user has pinned contextLimit explicitly. It returns whether the
// value changed. A non-positive limit is a no-op.
func MaybeSetContextLimit(settings *config.Settings, providerID string, limit int) (bool, error) {
	if settings == nil {
		return false, fmt.Errorf("set context limit: settings are required")
	}
	if limit <= 0 {
		return false, nil
	}
	canonical := config.NormalizeProviderID(providerID)
	if inst := providerInstance(settings, canonical); inst != nil {
		if inst.ContextLimitUserSet != nil && *inst.ContextLimitUserSet {
			return false, nil
		}
		if inst.ContextLimit != nil && *inst.ContextLimit == limit {
			return false, nil
		}
	}
	cfg, err := ensureProviderInstance(settings, canonical)
	if err != nil {
		return false, err
	}
	cfg.ContextLimit = &limit
	cfg.ContextLimitUserSet = nil
	return true, setProviderInstance(settings, canonical, cfg)
}

// CurrentSystemPromptPreset returns the preset id that matches the provider's
// stored personality + promptMode, falling back to the resolved global defaults.
func CurrentSystemPromptPreset(settings *config.Settings, providerID string) string {
	personality := config.ResolvePersonality(settings, providerID, "")
	variant := config.ResolveVariant(settings, providerID, "")
	if preset, ok := config.PresetForPersonalityVariant(personality, variant); ok {
		return preset.ID
	}
	return ""
}

// ResetProviderInstanceOverrides clears the behavioral instance overrides for a
// provider (temperature, compression, personality, masking, ...), preserving the
// structural fields (model, baseUrl, wireFormat), the curated activeModels set,
// and any unknown round-tripped keys. It does not touch the API key or a custom
// provider's definition.
func ResetProviderInstanceOverrides(settings *config.Settings, providerID string) error {
	if settings == nil {
		return fmt.Errorf("reset provider settings: settings are required")
	}
	canonical := config.NormalizeProviderID(providerID)
	inst := providerInstance(settings, canonical)
	if inst == nil {
		return nil
	}
	reset := &config.ProviderInstanceConfig{
		Model:        inst.Model,
		BaseURL:      inst.BaseURL,
		WireFormat:   inst.WireFormat,
		ActiveModels: inst.ActiveModels,
		Extra:        inst.Extra,
	}
	return setProviderInstance(settings, canonical, reset)
}

// ClearProviderSetting removes a single instance override so the field falls back
// to its resolved default.
func ClearProviderSetting(settings *config.Settings, providerID, key string) error {
	if settings == nil {
		return fmt.Errorf("clear provider setting: settings are required")
	}
	canonical := config.NormalizeProviderID(providerID)
	cfg, err := ensureProviderInstance(settings, canonical)
	if err != nil {
		return err
	}
	switch key {
	case "model":
		cfg.Model = ""
	case "baseUrl":
		cfg.BaseURL = ""
	case "promptMode":
		cfg.PromptMode = ""
	case "personality":
		cfg.Personality = ""
	case "toolCallParsing":
		cfg.ToolCallParsing = ""
	case "systemPromptOverride":
		cfg.SystemPromptOverride = ""
	case "reasoningEffort":
		cfg.ReasoningEffort = ""
	case "contextLimit":
		cfg.ContextLimit = nil
		cfg.ContextLimitUserSet = nil
	case "timeout":
		cfg.Timeout = nil
	case "compressionThreshold":
		cfg.CompressionThreshold = nil
	case "preserveFraction":
		cfg.PreserveFraction = nil
	case "temperature":
		cfg.Temperature = nil
	case "enableTools":
		cfg.EnableTools = nil
	case "useResponseChaining":
		cfg.UseResponseChaining = nil
	case "toolOutputMaskingEnabled":
		cfg.ToolOutputMaskingEnabled = nil
	case "toolOutputMaskingProtectionFraction":
		cfg.ToolOutputMaskingProtectionFraction = nil
	case "toolOutputMaskingPrunableFraction":
		cfg.ToolOutputMaskingPrunableFraction = nil
	case "toolOutputMaskingProtectLatestTurn":
		cfg.ToolOutputMaskingProtectLatestTurn = nil
	default:
		return fmt.Errorf("clear provider setting: unsupported key %q", key)
	}
	return setProviderInstance(settings, canonical, cfg)
}
