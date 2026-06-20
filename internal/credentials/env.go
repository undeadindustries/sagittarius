package credentials

import (
	"os"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/config"
)

const (
	envGeminiAPIKey = "GEMINI_API_KEY"
	envGoogleAPIKey = "GOOGLE_API_KEY"
)

func apiKeyFromEnv(providerID string) (string, bool) {
	for _, name := range envVarNames(providerID) {
		raw, ok := os.LookupEnv(name)
		if !ok {
			continue
		}
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		return trimmed, true
	}
	return "", false
}

func envVarNames(providerID string) []string {
	if def, ok := config.LookupBuiltInProvider(providerID); ok {
		if providerID == string(config.BuiltInGeminiAPIKey) {
			return []string{envGeminiAPIKey, envGoogleAPIKey}
		}
		if def.APIKeyEnvVar != "" {
			return []string{def.APIKeyEnvVar}
		}
	}
	if name := customProviderEnvVar(providerID); name != "" {
		return []string{name}
	}
	return nil
}

func customProviderEnvVar(providerID string) string {
	loader, err := config.NewLoader()
	if err != nil {
		return ""
	}
	settings, err := loader.Load()
	if err != nil || settings == nil || settings.Providers == nil {
		return ""
	}
	if settings.Providers.Custom == nil {
		return ""
	}
	if def, ok := settings.Providers.Custom[providerID]; ok {
		return def.APIKeyEnvVar
	}
	return ""
}

func primaryEnvVarName(providerID string) string {
	names := envVarNames(providerID)
	if len(names) == 0 {
		return envGeminiAPIKey
	}
	return names[0]
}
