package config

// SymbolsEnabled reports whether the built-in find_symbol tool should be
// registered. Project settings win over global; the default is true so the
// feature is on unless a user explicitly disables it (e.g. to use an external
// code-intelligence MCP instead).
func SymbolsEnabled(global, project *Settings) bool {
	if v, ok := symbolsBoolValue(project, func(c *SagittariusSymbolsConfig) *bool { return c.Enabled }); ok {
		return v
	}
	if v, ok := symbolsBoolValue(global, func(c *SagittariusSymbolsConfig) *bool { return c.Enabled }); ok {
		return v
	}
	return true
}

// SymbolsPreferGopls reports whether find_symbol should note gopls MCP tools in
// its description on Go modules. Project wins over global; the default is true.
func SymbolsPreferGopls(global, project *Settings) bool {
	if v, ok := symbolsBoolValue(project, func(c *SagittariusSymbolsConfig) *bool { return c.PreferGopls }); ok {
		return v
	}
	if v, ok := symbolsBoolValue(global, func(c *SagittariusSymbolsConfig) *bool { return c.PreferGopls }); ok {
		return v
	}
	return true
}

func symbolsBoolValue(s *Settings, pick func(*SagittariusSymbolsConfig) *bool) (bool, bool) {
	if s == nil || s.Sagittarius == nil || s.Sagittarius.Symbols == nil {
		return false, false
	}
	if v := pick(s.Sagittarius.Symbols); v != nil {
		return *v, true
	}
	return false, false
}
