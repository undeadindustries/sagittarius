package agents

import (
	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/modes"
)

// ResolveSubagentModel returns the model id for a subagent using the Sagittarius
// fallback chain (subagent.<name> → subagent.default → global default → provider).
func ResolveSubagentModel(name string, settings *config.Settings, providerDefault string) string {
	var cfg *config.SagittariusSettings
	if settings != nil {
		cfg = settings.Sagittarius
	}
	return modes.ResolveSubagentModel(name, cfg, providerDefault)
}
