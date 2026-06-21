// Package config loads and persists Sagittarius settings from settings.json
// under ~/.sagittarius/.
//
// # Config paths
//
// Global settings live at $HOME/.sagittarius/settings.json. Path resolution
// matches the fork Storage.getGlobalSettingsPath() and homedir() seams:
//
//   - SAGITTARIUS_HOME — if set, used instead of os.UserHomeDir(); settings path
//     becomes $SAGITTARIUS_HOME/.sagittarius/settings.json.
//
// System paths (read-only in fork; documented for later phases):
//
//   - SAGITTARIUS_SYSTEM_SETTINGS_PATH — overrides /etc/sagittarius/settings.json
//   - SAGITTARIUS_SYSTEM_DEFAULTS_PATH — overrides system-defaults.json sibling
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
