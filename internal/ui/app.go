package ui

import "context"

// App handles user input during an interactive session.
// Phase 07 replaces the demo echo implementation with the agent loop.
type App interface {
	// HandleInput processes one user line and streams assistant output via events.
	// The channel must be closed after StreamDone or an error event.
	HandleInput(ctx context.Context, input string) (<-chan StreamEvent, error)
}
