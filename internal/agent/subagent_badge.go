package agent

import (
	"github.com/undeadindustries/sagittarius/internal/config"
)

// subagentPairLabel formats a child (provider, model) pair for display, or
// returns "" when it matches the parent's live pair. Used for the model-facing
// via line, where a label that duplicates the schema's provider/model fields
// is pure token cost. The UI badge (subagentBadge) is unconditional instead.
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
// routing pins resolve to. It is unconditional — the card exists to make
// routing visible, and "no badge" read as "no information" in practice. The
// label is UI-only, so showing the live pair costs nothing.
func subagentBadge(r *Runner, class config.SubagentClass) string {
	liveProvider := r.activeProviderID()
	liveModel := r.Model()
	target := config.ResolveSubagentTarget(class, r.sagittariusSettings(), liveProvider, liveModel)
	provider := target.Provider
	if provider == "" {
		provider = liveProvider
	}
	if target.Model == "" {
		return ""
	}
	return config.ProviderDisplayID(provider) + "/" + target.Model
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
