package agents

import (
	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/modes"
)

// ResolveSubagentModel returns the model id for a subagent: a subagent-specific
// override (subagent.<name>.model → subagent.default.model) when configured,
// otherwise the live mode-resolved model the main loop is using. liveModel is
// the runner's current model, which already encodes the full mode/provider
// resolution chain.
func ResolveSubagentModel(name string, settings *config.Settings, liveModel string) string {
	var cfg *config.SagittariusSettings
	if settings != nil {
		cfg = settings.Sagittarius
	}
	return modes.ResolveSubagentModel(name, cfg, liveModel)
}
