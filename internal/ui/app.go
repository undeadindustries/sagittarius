package ui

import "context"

// BangCallIDPrefix marks StreamEvent.ToolCallID values for a user `!` command
// so the TUI can offer Tab-focus without treating agent shell calls the same.
const BangCallIDPrefix = "bang-"

// App handles user input during an interactive session.
// Phase 07 replaces the demo echo implementation with the agent loop.
type App interface {
	// HandleInput processes one user line and streams assistant output via events.
	// The channel must be closed after StreamDone or an error event.
	HandleInput(ctx context.Context, input string) (<-chan StreamEvent, error)
}

// BangInputWriter writes keystrokes into an in-flight user `!` PTY.
// Implemented by the agent App; TUI stubs may omit it.
type BangInputWriter interface {
	WriteBangInput([]byte) error
}
