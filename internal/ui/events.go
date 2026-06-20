package ui

// StreamEventType identifies a streaming UI update from the agent loop.
type StreamEventType int

const (
	// StreamTextDelta appends incremental model output text.
	StreamTextDelta StreamEventType = iota
	// StreamToolStart announces a tool invocation.
	StreamToolStart
	// StreamToolConfirm prompts the user to approve a destructive tool (interactive mode).
	StreamToolConfirm
	// StreamToolResult reports tool execution outcome text.
	StreamToolResult
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
	// ConfirmReply is set for StreamToolConfirm; the TUI sends true/false when the user responds.
	ConfirmReply chan bool
}

// StatusBar holds footer metadata shown below the input area.
type StatusBar struct {
	Left   string
	Right  string
	Detail string
}
