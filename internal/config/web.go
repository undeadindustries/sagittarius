package config

// Resolvers for sagittarius.web.*, all with project-over-global precedence.
//
// Key resolution lives in the caller (internal/credentials owns that), so
// WebSearchEnabled takes hasGeminiKey as a parameter rather than reaching for
// credentials from here — this package stays free of credential lookups.

// WebSearchEnabled resolves sagittarius.web.searchEnabled. When unset in both
// scopes it follows key availability: google_web_search requires Gemini native
// grounding, so registering it without a resolvable key would only ever produce
// errors.
func WebSearchEnabled(global, project *Settings, hasGeminiKey bool) bool {
	if project != nil && project.Sagittarius != nil && project.Sagittarius.Web != nil && project.Sagittarius.Web.SearchEnabled != nil {
		return *project.Sagittarius.Web.SearchEnabled
	}
	if global != nil && global.Sagittarius != nil && global.Sagittarius.Web != nil && global.Sagittarius.Web.SearchEnabled != nil {
		return *global.Sagittarius.Web.SearchEnabled
	}
	return hasGeminiKey
}

// WebFetchEnabled resolves sagittarius.web.fetchEnabled, defaulting to true:
// web_fetch has a key-free Go HTTP fallback, so it stays useful without Gemini.
func WebFetchEnabled(global, project *Settings) bool {
	if project != nil && project.Sagittarius != nil && project.Sagittarius.Web != nil && project.Sagittarius.Web.FetchEnabled != nil {
		return *project.Sagittarius.Web.FetchEnabled
	}
	if global != nil && global.Sagittarius != nil && global.Sagittarius.Web != nil && global.Sagittarius.Web.FetchEnabled != nil {
		return *global.Sagittarius.Web.FetchEnabled
	}
	return true
}

// WebDirectFetch resolves sagittarius.web.directWebFetch, defaulting to false
// (prompt-plus-summarize mode). When true, web_fetch takes a single url and
// returns raw converted text with no LLM summarization.
func WebDirectFetch(global, project *Settings) bool {
	if project != nil && project.Sagittarius != nil && project.Sagittarius.Web != nil && project.Sagittarius.Web.DirectWebFetch != nil {
		return *project.Sagittarius.Web.DirectWebFetch
	}
	if global != nil && global.Sagittarius != nil && global.Sagittarius.Web != nil && global.Sagittarius.Web.DirectWebFetch != nil {
		return *global.Sagittarius.Web.DirectWebFetch
	}
	return false
}

// WebMaxFetchBytes resolves sagittarius.web.maxFetchBytes, the per-request
// download cap for the Go HTTP fetch path. An unset or non-positive value
// yields DefaultMaxFetchBytes (or DefaultMaxExperimentalFetchBytes in
// directWebFetch mode, where the raw text goes to the caller rather than into
// one turn's context).
func WebMaxFetchBytes(global, project *Settings, directWebFetch bool) int {
	fallback := DefaultMaxFetchBytes
	if directWebFetch {
		fallback = DefaultMaxExperimentalFetchBytes
	}
	if project != nil && project.Sagittarius != nil && project.Sagittarius.Web != nil && project.Sagittarius.Web.MaxFetchBytes != nil {
		if n := *project.Sagittarius.Web.MaxFetchBytes; n > 0 {
			return n
		}
	}
	if global != nil && global.Sagittarius != nil && global.Sagittarius.Web != nil && global.Sagittarius.Web.MaxFetchBytes != nil {
		if n := *global.Sagittarius.Web.MaxFetchBytes; n > 0 {
			return n
		}
	}
	return fallback
}

// WebUtilityModel resolves sagittarius.web.utilityModel, the Gemini model the
// web tools' dedicated utility client uses. An empty result means "let the
// provider pick its default".
func WebUtilityModel(global, project *Settings) string {
	if project != nil && project.Sagittarius != nil && project.Sagittarius.Web != nil && project.Sagittarius.Web.UtilityModel != "" {
		return project.Sagittarius.Web.UtilityModel
	}
	if global != nil && global.Sagittarius != nil && global.Sagittarius.Web != nil {
		return global.Sagittarius.Web.UtilityModel
	}
	return ""
}
