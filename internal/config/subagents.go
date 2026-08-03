package config

// SubagentsEnabled reports whether read-only research subagents are enabled.
// Project settings win over global; the default is false.
func SubagentsEnabled(global, project *Settings) bool {
	if v, ok := subagentsBoolValue(project, func(c *SagittariusSubagents) *bool { return c.Enabled }); ok {
		return v
	}
	if v, ok := subagentsBoolValue(global, func(c *SagittariusSubagents) *bool { return c.Enabled }); ok {
		return v
	}
	return false
}

func subagentsBoolValue(s *Settings, pick func(*SagittariusSubagents) *bool) (bool, bool) {
	if s == nil || s.Sagittarius == nil || s.Sagittarius.Subagents == nil {
		return false, false
	}
	if v := pick(s.Sagittarius.Subagents); v != nil {
		return *v, true
	}
	return false, false
}
