package agents

import (
	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/modes"
)

// ResolveSubagentModel returns the model id for a subagent using the Sagittarius
// fallback chain (subagent.<name> → subagent.default → provider-scoped default →
// provider default → legacy global default). The active provider id scopes the
// sagittarius.defaultModels lookup.
func ResolveSubagentModel(name string, settings *config.Settings, providerDefault string) string {
	var cfg *config.SagittariusSettings
	var providerID string
	if settings != nil {
		cfg = settings.Sagittarius
		providerID = settings.ActiveProvider()
	}
	return modes.ResolveSubagentModel(name, cfg, providerID, providerDefault)
}
