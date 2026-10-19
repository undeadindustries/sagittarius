package config

// SkillsEnabled reports whether agent skills are enabled (the activate_skill
// tool, the Skills prompt section, and @skill: mentions). Project settings win
// over global; the default is true.
func SkillsEnabled(global, project *Settings) bool {
	if v, ok := skillsBoolValue(project); ok {
		return v
	}
	if v, ok := skillsBoolValue(global); ok {
		return v
	}
	return true
}

func skillsBoolValue(s *Settings) (bool, bool) {
	if s == nil || s.Sagittarius == nil || s.Sagittarius.SkillsEnabled == nil {
		return false, false
	}
	return *s.Sagittarius.SkillsEnabled, true
}
