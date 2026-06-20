package ui

// StreamEventType identifies a streaming UI update from the agent loop.
type StreamEventType int

const (
	// StreamTextDelta appends incremental model output text.
	StreamTextDelta StreamEventType = iota
	// StreamToolStart announces a tool invocation (stub for Phase 08).
	StreamToolStart
	// StreamError carries a non-fatal stream error for display.
	StreamError
	// StreamDone marks the end of a single assistant turn stream.
	StreamDone
)

// StreamEvent is a single streaming update rendered in the scrollback viewport.
type StreamEvent struct {
	Type     StreamEventType
	Text     string
	ToolName string
	Err      error
}

// StatusBar holds footer metadata shown below the input area.
type StatusBar struct {
	Left   string
	Right  string
	Detail string
}
