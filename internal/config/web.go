package config

// WebSearchEnabled resolves sagittarius.web.searchEnabled with project-over-global
// precedence. If unset in both, it defaults to true when a Gemini key is resolvable,
// false otherwise. But we don't have key resolution here. We just return true if
// it's explicitly true. Wait, the plan says:
// "if unset, treat as on when Gemini key present for search, fetch on"
// To keep config pure, we can just return the pointer from settings, or resolve it
// in the caller where credentials are known.
// Actually, let's just return a bool and assume true by default if unset?
// The plan says: "defaults: document current resolver behavior; if unset, treat as on when Gemini key present for search, fetch on - match docs intent"
func WebSearchEnabled(global, project *Settings, hasGeminiKey bool) bool {
	if project != nil && project.Sagittarius != nil && project.Sagittarius.Web != nil && project.Sagittarius.Web.SearchEnabled != nil {
		return *project.Sagittarius.Web.SearchEnabled
	}
	if global != nil && global.Sagittarius != nil && global.Sagittarius.Web != nil && global.Sagittarius.Web.SearchEnabled != nil {
		return *global.Sagittarius.Web.SearchEnabled
	}
	return hasGeminiKey
}

// WebFetchEnabled resolves sagittarius.web.fetchEnabled with project-over-global
// precedence. If unset in both, it defaults to true.
func WebFetchEnabled(global, project *Settings) bool {
	if project != nil && project.Sagittarius != nil && project.Sagittarius.Web != nil && project.Sagittarius.Web.FetchEnabled != nil {
		return *project.Sagittarius.Web.FetchEnabled
	}
	if global != nil && global.Sagittarius != nil && global.Sagittarius.Web != nil && global.Sagittarius.Web.FetchEnabled != nil {
		return *global.Sagittarius.Web.FetchEnabled
	}
	return true
}
