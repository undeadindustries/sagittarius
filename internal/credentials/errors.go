package credentials

import (
	"errors"
	"fmt"

	"github.com/undeadindustries/sagittarius/internal/config"
)

// ErrAPIKeyMissing indicates no API key was found via env or secure storage.
var ErrAPIKeyMissing = errors.New("api key missing")

func missingAPIKeyError(providerID string) error {
	envName := primaryEnvVarName(providerID)
	if providerID == string(config.BuiltInGeminiAPIKey) {
		return fmt.Errorf("%w: when using Gemini API, set %s or %s, or store a key with /provider set %s key",
			ErrAPIKeyMissing, envGeminiAPIKey, envGoogleAPIKey, providerID)
	}
	if def, ok := config.LookupBuiltInProvider(providerID); ok {
		return fmt.Errorf("%w: set %s or store a key with /provider set %s key (%s)",
			ErrAPIKeyMissing, def.APIKeyEnvVar, providerID, def.DisplayName)
	}
	return fmt.Errorf("%w: set %s or store a key with /provider set %s key",
		ErrAPIKeyMissing, envName, providerID)
}

func saveAPIKeyError(providerID string, reason error) error {
	return fmt.Errorf("could not save API key for provider %q: %w. "+
		"On Linux, ensure libsecret and a Secret Service (e.g. gnome-keyring) are available; "+
		"or set GEMINI_FORCE_FILE_STORAGE=true to use encrypted file storage",
		providerID, reason)
}
