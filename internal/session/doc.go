// Package session provides session persistence for Sagittarius.
//
// Sessions are stored as JSONL files under ~/.sagittarius/tmp/<slug>/chats/,
// where <slug> is the project registry slug for the workspace root. The JSONL
// record format follows the fork's chatRecordingService schema.
//
// Each JSONL file contains:
//   - First line: a metadata record (sessionId, projectHash, startTime, …)
//   - Subsequent lines: message records or $set / $rewindTo control records
//
// Use Recorder for streaming writes during a session, Manager for listing and
// loading existing sessions, and Selector for resolving a resume argument.
package session
