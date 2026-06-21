// Package session provides fork-compatible session persistence for Sagittarius.
//
// Sessions are stored as JSONL files under ~/.gemini/tmp/<project_hash>/chats/.
// The file format is intentionally compatible with the gemini-cli fork so that
// sessions created by either tool can be listed and resumed.
//
// Each JSONL file contains:
//   - First line: a metadata record (sessionId, projectHash, startTime, …)
//   - Subsequent lines: message records or $set / $rewindTo control records
//
// Use Recorder for streaming writes during a session, Manager for listing and
// loading existing sessions, and Selector for resolving a resume argument.
package session
