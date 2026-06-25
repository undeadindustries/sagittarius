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
//
// # Test serialization rule
//
// Several test hooks mutate process-global state that is not otherwise locked
// against the file-store globals: SetStoreFactoryForTesting,
// SetActiveBackendForTesting, SetCredentialsPathForTesting,
// SetCacheTTLForTesting, and ResetForTesting. Any test — in this package or any
// other (e.g. internal/provider, internal/agent) — that calls one of these MUST
// serialize on the package guard and MUST NOT call t.Parallel():
//
//	func TestSomething(t *testing.T) {
//		t.Cleanup(credentials.LockTestGlobals())
//		credentials.SetStoreFactoryForTesting(...)
//		t.Cleanup(credentials.ResetForTesting)
//		// ...
//	}
//
// Register the unlock via t.Cleanup (not defer) and before any ResetForTesting
// cleanup: t.Cleanup runs LIFO and after deferred calls, so this guarantees the
// unlock fires last — keeping the lock held across ResetForTesting. The guard is
// a single process-wide mutex, so it serializes the mutating tests within each
// test binary. Holding it across the whole test (including the cleanup that runs
// ResetForTesting) is what removes the historical -race flake where a sibling's
// cleanup restored the real backend mid-test.
package credentials

import "sync"

// credTestMu serializes tests that mutate this package's process-global
// credential state. See the package doc for the rule.
var credTestMu sync.Mutex

// LockTestGlobals locks the credential-test mutex and returns its unlock func,
// so callers can write `t.Cleanup(credentials.LockTestGlobals())`. Any test that
// mutates credential package globals must hold this for its full duration and
// must not run in parallel. It lives in non-test code because Go cannot share
// _test.go symbols across packages, matching the existing *ForTesting hooks.
func LockTestGlobals() func() {
	credTestMu.Lock()
	return credTestMu.Unlock
}
