package config

// ScriptToolEnabled reports whether the run_script batch tool is registered.
// Project settings win over global; the default is false.
func ScriptToolEnabled(global, project *Settings) bool {
	if v, ok := scriptToolBoolValue(project); ok {
		return v
	}
	if v, ok := scriptToolBoolValue(global); ok {
		return v
	}
	return false
}

func scriptToolBoolValue(s *Settings) (bool, bool) {
	if s == nil || s.Sagittarius == nil || s.Sagittarius.ScriptToolEnabled == nil {
		return false, false
	}
	return *s.Sagittarius.ScriptToolEnabled, true
}
