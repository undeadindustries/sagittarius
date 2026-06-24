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
	// StreamClearScrollback clears the scrollback before a restored conversation
	// is repainted, so /chat resume replaces the visible history instead of
	// appending to it.
	StreamClearScrollback
	// StreamScrollback repaints a restored conversation block (role + text) into
	// the scrollback, used by /chat resume.
	StreamScrollback
	// StreamCopyToClipboard asks the UI to copy Text to the system clipboard
	// (used by /copy).
	StreamCopyToClipboard
	// StreamSetTheme asks the TUI to switch its active theme to Text live
	// ("default" or "greyscale"), used by /theme.
	StreamSetTheme
)

// ScrollbackRole classifies a StreamScrollback block so the TUI applies the
// matching user / assistant / info styling.
type ScrollbackRole int

const (
	// ScrollbackUser is a user turn.
	ScrollbackUser ScrollbackRole = iota
	// ScrollbackAssistant is a model turn.
	ScrollbackAssistant
	// ScrollbackInfo is system / informational text.
	ScrollbackInfo
)

// ScrollbackEntry is one restored conversation block used to seed the TUI
// scrollback on startup (e.g. when --resume loads a prior session).
type ScrollbackEntry struct {
	Role ScrollbackRole
	Text string
}

// DialogKind identifies an interactive TUI dialog requested by the agent layer.
type DialogKind string

const (
	// DialogProviders opens the providers management wizard.
	DialogProviders DialogKind = "providers"
	// DialogModels opens the per-model settings editor.
	DialogModels DialogKind = "models"
	// DialogModelPick opens the global {Provider}/{Model} current-model picker.
	DialogModelPick DialogKind = "model-pick"
	// DialogModes opens the mode-override editor.
	DialogModes DialogKind = "modes"
	// DialogSystemPrompt opens the project system-prompt preset picker.
	DialogSystemPrompt DialogKind = "system-prompt"
	// DialogMCP opens the MCP server management wizard.
	DialogMCP DialogKind = "mcp"
	// DialogTools opens the tool inventory.
	DialogTools DialogKind = "tools"
)

// StreamEvent is a single streaming update rendered in the scrollback viewport.
type StreamEvent struct {
	Type     StreamEventType
	Text     string
	ToolName string
	Err      error
	// Diff is set for StreamToolConfirm on write_file: a git-style unified diff
	// previewing the pending change. The TUI colorizes it in the confirm band.
	Diff string
	// ConfirmReply is set for StreamToolConfirm; the TUI sends the user's
	// decision (deny / once / session) when the user responds.
	ConfirmReply chan ConfirmDecision
	// Dialog is set for StreamOpenDialog and names the overlay to open.
	Dialog DialogKind
	// ScrollbackRole is set for StreamScrollback and selects the block styling.
	ScrollbackRole ScrollbackRole
}

// ConfirmDecision is the user's answer to a tool confirmation prompt.
type ConfirmDecision int

const (
	// ConfirmDeny rejects the tool invocation.
	ConfirmDeny ConfirmDecision = iota
	// ConfirmOnce approves this single invocation.
	ConfirmOnce
	// ConfirmSession approves this and all future invocations of the same tool
	// for the rest of the session.
	ConfirmSession
)

// StatusBar holds footer metadata shown below the input area.
type StatusBar struct {
	Left   string // transient UI state (e.g. "confirm tool", "mode"); empty when idle
	Right  string // "{provider} - {model}" plus usage stats (footer line 1, right)
	Detail string // system-prompt preset label + session totals (footer line 2)
	Mode   string // interaction mode id for the input prompt (agent, plan, ask, debug)
}
