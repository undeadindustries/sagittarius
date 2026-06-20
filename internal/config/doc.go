// Package config loads and persists Sagittarius settings from the shared
// gemini-cli settings.json under ~/.gemini/.
//
// # Config paths
//
// Global settings live at $HOME/.gemini/settings.json. Path resolution matches
// fork Storage.getGlobalSettingsPath() and homedir() in paths.ts:
//
//   - GEMINI_CLI_HOME — if set, used instead of os.UserHomeDir(); settings path
//     becomes $GEMINI_CLI_HOME/.gemini/settings.json.
//
// System paths (read-only in fork; documented for later phases):
//
//   - GEMINI_CLI_SYSTEM_SETTINGS_PATH — overrides /etc/gemini-cli/settings.json
//   - GEMINI_CLI_SYSTEM_DEFAULTS_PATH — overrides system-defaults.json sibling
//
// # Environment overlays (fork parity)
//
// Provider-related env vars consumed when resolving runtime config (see
// settingsSchema.ts and core/config/config.ts):
//
//   - GEMINI_PROVIDER — overrides providers.active (wins over settings.json)
//   - GEMINI_API_KEY / GOOGLE_API_KEY — Gemini API key (Phase 03 credentials)
//   - OPENAI_API_KEY — OpenAI built-in provider key (Phase 03)
//
// Auth env whitelist for untrusted workspaces (fork settings.ts AUTH_ENV_VAR_WHITELIST):
// GEMINI_API_KEY, GOOGLE_API_KEY, GOOGLE_CLOUD_PROJECT, GOOGLE_CLOUD_LOCATION.
//
// Settings values may embed $VAR / ${VAR} placeholders; env expansion is deferred
// to the phase that wires CLI startup (fork resolveEnvVarsInObject).
//
// # Secrets
//
// API keys must never appear in settings.json (AD-005). Load strips forbidden
// secret fields; Save rejects documents that still contain them.
package config
