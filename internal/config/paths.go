package config

import (
	"os"
	"path/filepath"
)

const (
	// SagittariusDir is the directory name under the user's home (or
	// SAGITTARIUS_HOME) where global Sagittarius settings live.
	SagittariusDir = ".sagittarius"

	settingsFileName = "settings.json"
)

// ResolveHome returns the effective home directory.
// SAGITTARIUS_HOME overrides os.UserHomeDir, matching the fork homedir() seam.
func ResolveHome() (string, error) {
	if envHome := os.Getenv("SAGITTARIUS_HOME"); envHome != "" {
		return envHome, nil
	}
	return os.UserHomeDir()
}

// ResolveSagittariusDir returns the global ~/.sagittarius directory
// (or $SAGITTARIUS_HOME/.sagittarius).
func ResolveSagittariusDir() (string, error) {
	home, err := ResolveHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, SagittariusDir), nil
}

// ResolveSettingsPath returns the path to the user settings.json file.
func ResolveSettingsPath() (string, error) {
	dir, err := ResolveSagittariusDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, settingsFileName), nil
}

// ResolveGlobalAgentsPath returns ~/.sagittarius/AGENTS.md, the global memory file.
func ResolveGlobalAgentsPath() (string, error) {
	dir, err := ResolveSagittariusDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "AGENTS.md"), nil
}

// ProjectSagittariusDir returns <workDir>/.sagittarius.
func ProjectSagittariusDir(workDir string) string {
	return filepath.Join(workDir, SagittariusDir)
}

// ResolveProjectSettingsPath returns <workDir>/.sagittarius/settings.json, the
// per-project settings file merged over the global one for trusted workspaces.
func ResolveProjectSettingsPath(workDir string) string {
	return filepath.Join(ProjectSagittariusDir(workDir), settingsFileName)
}

// ResolveSystemSettingsPath returns the system-wide settings path.
// Override via SAGITTARIUS_SYSTEM_SETTINGS_PATH.
func ResolveSystemSettingsPath() string {
	if p := os.Getenv("SAGITTARIUS_SYSTEM_SETTINGS_PATH"); p != "" {
		return p
	}
	return "/etc/sagittarius/settings.json"
}

// ResolveSystemDefaultsPath returns the system defaults path.
// Override via SAGITTARIUS_SYSTEM_DEFAULTS_PATH.
func ResolveSystemDefaultsPath() string {
	if p := os.Getenv("SAGITTARIUS_SYSTEM_DEFAULTS_PATH"); p != "" {
		return p
	}
	return filepath.Join(filepath.Dir(ResolveSystemSettingsPath()), "system-defaults.json")
}
