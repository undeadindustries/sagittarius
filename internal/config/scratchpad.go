package config

// ScratchpadEnabled reports whether the update_scratchpad working-memory tool
// is registered. Project settings win over global; the default is true.
func ScratchpadEnabled(global, project *Settings) bool {
	if v, ok := scratchpadBoolValue(project); ok {
		return v
	}
	if v, ok := scratchpadBoolValue(global); ok {
		return v
	}
	return true
}

func scratchpadBoolValue(s *Settings) (bool, bool) {
	if s == nil || s.Sagittarius == nil || s.Sagittarius.ScratchpadEnabled == nil {
		return false, false
	}
	return *s.Sagittarius.ScratchpadEnabled, true
}
