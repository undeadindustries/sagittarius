package config

// EditEnabled reports whether the built-in edit tool should be registered.
// Project settings win over global; the default is true.
func EditEnabled(global, project *Settings) bool {
	if v, ok := editBoolValue(project, func(c *SagittariusEditConfig) *bool { return c.Enabled }); ok {
		return v
	}
	if v, ok := editBoolValue(global, func(c *SagittariusEditConfig) *bool { return c.Enabled }); ok {
		return v
	}
	return true
}

func editBoolValue(s *Settings, pick func(*SagittariusEditConfig) *bool) (bool, bool) {
	if s == nil || s.Sagittarius == nil || s.Sagittarius.Edit == nil {
		return false, false
	}
	if v := pick(s.Sagittarius.Edit); v != nil {
		return *v, true
	}
	return false, false
}
