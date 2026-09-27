package agent

import (
	"github.com/undeadindustries/sagittarius/internal/config"
)

// subagentPairLabel formats a child (provider, model) pair for display, or
// returns "" when it matches the parent's live pair. The badge exists to make
// routing visible; a label that always duplicates the footer is noise, not
// signal. A cross-provider pair shows "provider/model"; a same-provider model
// pin shows the bare model.
func subagentPairLabel(provider, model, liveProvider, liveModel string) string {
	switch {
	case provider != "" && provider != liveProvider:
		return config.ProviderDisplayID(provider) + "/" + model
	case model != "" && model != liveModel:
		return model
	default:
		return ""
	}
}

// subagentBadge is the StartBadger label for a subagent class: the pair its
// routing pins resolve to, or "" when the child would run the parent's live
// pair. Resolution is a pure settings read, so the scheduler can call it at
// StreamToolStart time, before the child exists.
func subagentBadge(r *Runner, class config.SubagentClass) string {
	liveProvider := r.activeProviderID()
	liveModel := r.Model()
	target := config.ResolveSubagentTarget(class, r.sagittariusSettings(), liveProvider, liveModel)
	provider := target.Provider
	if provider == "" {
		provider = liveProvider
	}
	return subagentPairLabel(provider, target.Model, liveProvider, liveModel)
}

// subagentViaLabel is the result-payload counterpart of subagentBadge: it
// labels the pair the child actually ran (read back from its runner), or ""
// when it matches the parent's live pair. Reading the child's resolved pair
// rather than the pin keeps the label truthful if resolution ever drifts.
func subagentViaLabel(parent *Runner, child *subagent) string {
	if parent == nil || child == nil || child.runner == nil {
		return ""
	}
	return subagentPairLabel(child.runner.ActiveProviderID(), child.runner.Model(),
		parent.activeProviderID(), parent.Model())
}
