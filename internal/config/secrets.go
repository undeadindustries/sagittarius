package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrSecretsInSettings indicates API keys or other secrets were found in
// settings.json. Secrets are stripped during load; callers may treat this as
// a warning via errors.Is.
var ErrSecretsInSettings = errors.New("settings.json contains forbidden secret fields")

// StripSecretsFromDocument removes forbidden secret fields from raw settings
// JSON before parsing. Returns the cleaned bytes and dotted paths stripped.
func StripSecretsFromDocument(raw []byte) ([]byte, []string, error) {
	if len(raw) == 0 {
		return raw, nil, nil
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, nil, fmt.Errorf("parse settings for secret scan: %w", err)
	}
	var stripped []string
	cleaned := stripSecretsValue(doc, "", &stripped)
	out, err := json.MarshalIndent(cleaned, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("re-encode settings after secret strip: %w", err)
	}
	out = append(out, '\n')
	return out, stripped, nil
}

func stripSecretsValue(v any, path string, stripped *[]string) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for key, val := range t {
			childPath := joinPath(path, key)
			if isSecretField(key, path) {
				*stripped = append(*stripped, childPath)
				continue
			}
			out[key] = stripSecretsValue(val, childPath, stripped)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			out[i] = stripSecretsValue(item, fmt.Sprintf("%s[%d]", path, i), stripped)
		}
		return out
	default:
		return v
	}
}

func isSecretField(key, parentPath string) bool {
	if strings.EqualFold(key, "apiKeyEnvVar") {
		return false
	}
	if !isForbiddenSecretKey(key) {
		return false
	}
	if parentPath == "" {
		return true
	}
	return strings.HasPrefix(parentPath, "providers")
}

func isForbiddenSecretKey(key string) bool {
	switch key {
	case "apiKey", "api_key", "key", "secret", "accessToken", "access_token", "token":
		return true
	default:
		return false
	}
}

func joinPath(base, key string) string {
	if base == "" {
		return key
	}
	return base + "." + key
}

// FindSecretFields returns dotted paths of forbidden secret keys in raw JSON.
func FindSecretFields(raw []byte) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse settings for secret scan: %w", err)
	}
	var found []string
	scanSecrets(doc, "", &found)
	return found, nil
}

func scanSecrets(v any, path string, found *[]string) {
	switch t := v.(type) {
	case map[string]any:
		for key, val := range t {
			childPath := joinPath(path, key)
			if isSecretField(key, path) {
				*found = append(*found, childPath)
				continue
			}
			scanSecrets(val, childPath, found)
		}
	case []any:
		for i, item := range t {
			scanSecrets(item, fmt.Sprintf("%s[%d]", path, i), found)
		}
	}
}
