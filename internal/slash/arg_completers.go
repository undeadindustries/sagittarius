package slash

import (
	"sort"

	"github.com/undeadindustries/sagittarius/internal/config"
)

// completeProviderIDs returns built-in and custom provider ids, sorted. It backs
// argument completion for `/provider use`.
func completeProviderIDs(deps Deps, _ string) []string {
	ids := make([]string, 0, len(config.BuiltInProviders)+4)
	for id := range config.BuiltInProviders {
		ids = append(ids, config.ProviderDisplayID(string(id)))
	}
	if deps.Settings != nil && deps.Settings.Providers != nil {
		for id := range deps.Settings.Providers.Custom {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

// completeCustomProviderIDs returns only user-defined provider ids, sorted. It
// backs argument completion for `/provider remove` (built-ins cannot be removed).
func completeCustomProviderIDs(deps Deps, _ string) []string {
	if deps.Settings == nil || deps.Settings.Providers == nil {
		return nil
	}
	ids := make([]string, 0, len(deps.Settings.Providers.Custom))
	for id := range deps.Settings.Providers.Custom {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
