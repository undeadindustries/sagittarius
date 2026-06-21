// Package contextmgmt implements the local-context defenses Sagittarius applies
// when the active provider uses the openai-chat wire format (the fork's
// isLocalMode semantics — see provider.IsOpenAIChatMode).
//
// These defenses keep a turn within a small local context window:
//
//   - Smart tool-output masking (ToolOutputMaskingService) offloads bulky tool
//     results to disk and replaces them with a compact marker.
//   - Write-file ejection replaces stale write_file content in history with a
//     cached marker, since the file on disk is the source of truth.
//   - Pre-turn budget assessment proactively triggers compression before a turn
//     would overflow the window.
//   - Adaptive compression threshold tightening reacts to repeated weak
//     compressions on small windows.
//   - Chat compression summarizes older history using the active provider model.
//
// The package name is contextmgmt rather than context to avoid colliding with
// the standard library context package, which the agent runner imports for
// cancellation. Most functions here are pure (no I/O); masking and compression
// take injected dependencies (token estimator, summarizer, file saver) so they
// unit-test deterministically.
//
// Every defense is a no-op for gemini-native and openai-responses wire formats;
// gating lives in Manager and the agent runner (see AD-014, AD-015).
package contextmgmt
