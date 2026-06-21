// Package credentials resolves provider API keys from environment variables,
// the OS keychain, and an encrypted file fallback without storing secrets in
// settings.json (AD-005).
//
// # Resolution order
//
// For each provider id:
//
//  1. Environment variable — built-in providers use registry defaults; gemini-apikey
//     accepts GEMINI_API_KEY or GOOGLE_API_KEY.
//  2. Secure storage — OS keychain entry service sagittarius-provider-<id>,
//     account <id> (fork providerCredentialStorage layout).
//  3. Error — actionable message when no key is configured.
//
// # Storage backends
//
// When the OS credential manager is available, keys are stored there. Otherwise
// (or when SAGITTARIUS_FORCE_FILE_STORAGE=true) keys are stored in an AES-256-GCM
// encrypted file at ~/.sagittarius/sagittarius-credentials.json. The encryption
// format follows the fork FileKeychain layout, but the scrypt salt/password are
// Sagittarius-specific so the file is not interchangeable with gemini-cli.
//
// # Environment
//
//   - SAGITTARIUS_FORCE_FILE_STORAGE — force encrypted file storage instead of keychain
//   - SAGITTARIUS_HOME — overrides home for ~/.sagittarius paths (via internal/config)
package credentials
