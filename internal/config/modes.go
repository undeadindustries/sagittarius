package config

import "fmt"

// modeSlot returns a pointer to the mode-config slot for modeName, or nil for an
// unknown mode. Reads (ModeConfig) and writes (SetModeOverride/ClearModeOverride)
// share this one switch, so adding a mode only requires editing here.
func modeSlot(m *SagittariusModes, modeName string) **SagittariusModeConfig {
	if m == nil {
		return nil
	}
	switch modeName {
	case "agent":
		return &m.Agent
	case "plan":
		return &m.Plan
	case "ask":
		return &m.Ask
	case "debug":
		return &m.Debug
	default:
		return nil
	}
}

// ModeConfig returns the override config for modeName, or nil when m is nil, the
// mode is unknown, or no override is set.
func ModeConfig(m *SagittariusModes, modeName string) *SagittariusModeConfig {
	slot := modeSlot(m, modeName)
	if slot == nil {
		return nil
	}
	return *slot
}

// SetModeOverride writes a (provider, model) routing override for modeName into s,
// preserving any existing SystemPromptSuffix/Extra. An empty model clears the
// override (see ClearModeOverride). It is the single mutation path shared by the
// headless /modes command and the modes dialog.
func SetModeOverride(s *Settings, modeName, providerID, model string) error {
	if s == nil {
		return fmt.Errorf("set mode override: nil settings")
	}
	if model == "" {
		ClearModeOverride(s, modeName)
		return nil
	}
	if s.Sagittarius == nil {
		s.Sagittarius = &SagittariusSettings{}
	}
	if s.Sagittarius.Modes == nil {
		s.Sagittarius.Modes = &SagittariusModes{}
	}
	slot := modeSlot(s.Sagittarius.Modes, modeName)
	if slot == nil {
		return fmt.Errorf("unknown mode %q (expected agent, plan, ask, debug)", modeName)
	}
	mc := &SagittariusModeConfig{
		Provider: NormalizeProviderID(providerID),
		Model:    model,
	}
	if *slot != nil {
		mc.SystemPromptSuffix = (*slot).SystemPromptSuffix
		mc.Extra = (*slot).Extra
	}
	*slot = mc
	return nil
}

// ClearModeOverride drops the (provider, model) routing override for modeName,
// preserving any SystemPromptSuffix/Extra (the slot is removed entirely only when
// both are empty). It is a no-op for nil settings or an unknown mode.
func ClearModeOverride(s *Settings, modeName string) {
	if s == nil || s.Sagittarius == nil || s.Sagittarius.Modes == nil {
		return
	}
	slot := modeSlot(s.Sagittarius.Modes, modeName)
	if slot == nil || *slot == nil {
		return
	}
	mc := *slot
	mc.Provider = ""
	mc.Model = ""
	if mc.SystemPromptSuffix == "" && mc.Extra == nil {
		*slot = nil
	}
}
