package config

import (
	"os"
	"path/filepath"
)

const (
	// GeminiDir is the directory name under the user's home (or GEMINI_CLI_HOME)
	// where global Gemini CLI settings live.
	GeminiDir = ".gemini"

	settingsFileName = "settings.json"
)

// ResolveHome returns the effective home directory.
// GEMINI_CLI_HOME overrides os.UserHomeDir, matching fork homedir() in paths.ts.
func ResolveHome() (string, error) {
	if envHome := os.Getenv("GEMINI_CLI_HOME"); envHome != "" {
		return envHome, nil
	}
	return os.UserHomeDir()
}

// ResolveGeminiDir returns the global ~/.gemini directory (or $GEMINI_CLI_HOME/.gemini).
func ResolveGeminiDir() (string, error) {
	home, err := ResolveHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, GeminiDir), nil
}

// ResolveSettingsPath returns the path to the user settings.json file.
func ResolveSettingsPath() (string, error) {
	dir, err := ResolveGeminiDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, settingsFileName), nil
}

// ResolveSystemSettingsPath returns the system-wide settings path.
// Override via GEMINI_CLI_SYSTEM_SETTINGS_PATH (fork getSystemSettingsPath).
func ResolveSystemSettingsPath() string {
	if p := os.Getenv("GEMINI_CLI_SYSTEM_SETTINGS_PATH"); p != "" {
		return p
	}
	return "/etc/gemini-cli/settings.json"
}

// ResolveSystemDefaultsPath returns the system defaults path.
// Override via GEMINI_CLI_SYSTEM_DEFAULTS_PATH (fork getSystemDefaultsPath).
func ResolveSystemDefaultsPath() string {
	if p := os.Getenv("GEMINI_CLI_SYSTEM_DEFAULTS_PATH"); p != "" {
		return p
	}
	return filepath.Join(filepath.Dir(ResolveSystemSettingsPath()), "system-defaults.json")
}
