// Package hooks implements a lifecycle hooks system for Sagittarius.
//
// Hooks run shell commands at specific lifecycle events (SessionStart,
// SessionEnd, FirstTurn, BeforeAgent, AfterAgent, PreCompress, BeforeTool,
// AfterTool). The system is wire-compatible with gemini-cli's hook protocol:
// JSON on stdin, JSON-only on stdout, stderr for logging, exit code 0 for
// success, exit code 2 for system blocks, and non-JSON stdout falling back to
// plain-text responses.
package hooks
