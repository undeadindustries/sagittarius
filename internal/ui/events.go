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
	// StreamInfo carries non-model system text (slash command output).
	StreamInfo
	// StreamQuit signals the TUI should exit the session.
	StreamQuit
	// StreamOpenDialog asks the TUI to open an interactive dialog overlay.
	StreamOpenDialog
	// StreamDone marks the end of a single assistant turn stream.
	StreamDone
)

// DialogKind identifies an interactive TUI dialog requested by the agent layer.
type DialogKind string

const (
	// DialogProviders opens the providers management wizard.
	DialogProviders DialogKind = "providers"
	// DialogModels opens the active-provider model picker.
	DialogModels DialogKind = "models"
)

// StreamEvent is a single streaming update rendered in the scrollback viewport.
type StreamEvent struct {
	Type     StreamEventType
	Text     string
	ToolName string
	Err      error
	// ConfirmReply is set for StreamToolConfirm; the TUI sends true/false when the user responds.
	ConfirmReply chan bool
	// Dialog is set for StreamOpenDialog and names the overlay to open.
	Dialog DialogKind
}

// StatusBar holds footer metadata shown below the input area.
type StatusBar struct {
	Left   string // provider display name (footer line 1, left)
	Right  string // model id and usage stats (footer line 1, right)
	Detail string // system-prompt preset label (footer line 2)
	Mode   string // interaction mode id for the input prompt (agent, plan, ask, debug)
}
