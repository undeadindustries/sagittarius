package session

import "github.com/undeadindustries/sagittarius/internal/provider"

const (
	// SessionFilePrefix is the filename prefix for session JSONL files.
	SessionFilePrefix = "session-"

	// ResumeLatest is the sentinel value passed to --resume when no argument is given.
	ResumeLatest = "latest"
)

// MessageType classifies a message record in the JSONL file.
type MessageType string

const (
	MessageTypeUser    MessageType = "user"
	MessageTypeModel   MessageType = "gemini" // fork uses "gemini" for model messages
	MessageTypeInfo    MessageType = "info"
	MessageTypeError   MessageType = "error"
	MessageTypeWarning MessageType = "warning"
)

// Part is a single content element within a message. Maps to provider.Part for
// serialisation: only Text, FunctionCall, and FunctionResponse are written.
type Part struct {
	Text             string            `json:"text,omitempty"`
	FunctionCall     *FunctionCallPart `json:"functionCall,omitempty"`
	FunctionResponse *FuncResponsePart `json:"functionResponse,omitempty"`
}

// FunctionCallPart carries a tool invocation.
type FunctionCallPart struct {
	ID   string                 `json:"id,omitempty"`
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args,omitempty"`
}

// FuncResponsePart carries a tool result.
type FuncResponsePart struct {
	ID       string      `json:"id,omitempty"`
	Name     string      `json:"name"`
	Response interface{} `json:"response,omitempty"`
}

// ToolCallRecord records a single tool execution inside a model message.
type ToolCallRecord struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// MessageRecord is one line of JSONL (a single turn).
type MessageRecord struct {
	ID        string      `json:"id"`
	Timestamp string      `json:"timestamp"`
	Type      MessageType `json:"type"`
	// Content is []Part (serialised as JSON array).
	Content   []Part           `json:"content"`
	ToolCalls []ToolCallRecord `json:"toolCalls,omitempty"`
}

// MetadataRecord is the first line of each JSONL file and any $set update.
type MetadataRecord struct {
	SessionID   string `json:"sessionId"`
	ProjectHash string `json:"projectHash"`
	StartTime   string `json:"startTime"`
	LastUpdated string `json:"lastUpdated"`
	Summary     string `json:"summary,omitempty"`
	Kind        string `json:"kind,omitempty"` // "main" | "subagent"
}

// SetRecord carries a $set metadata update appended mid-session.
type SetRecord struct {
	Set *MetadataRecord `json:"$set"`
}

// RewindRecord marks a rewind-to-message operation.
type RewindRecord struct {
	RewindTo string `json:"$rewindTo"`
}

// ConversationRecord is the fully loaded in-memory view of a session.
type ConversationRecord struct {
	SessionID   string
	ProjectHash string
	StartTime   string
	LastUpdated string
	Summary     string
	Kind        string
	Messages    []MessageRecord
}

// SessionInfo is the display/selection view of a session (used for listing).
type SessionInfo struct {
	// ID is the full session UUID.
	ID string
	// File is the basename without extension.
	File string
	// FileName is the full filename including extension.
	FileName string
	// StartTime is an ISO 8601 timestamp.
	StartTime string
	// LastUpdated is an ISO 8601 timestamp.
	LastUpdated string
	// MessageCount is the total number of messages.
	MessageCount int
	// DisplayName is the first user message (truncated) or summary.
	DisplayName string
	// FirstUserMessage is the raw first user message.
	FirstUserMessage string
	// IsCurrentSession is true when this is the session currently being written.
	IsCurrentSession bool
	// Index is the 1-based position in the sorted list.
	Index int
}

// SelectionResult is returned by Selector.ResolveSession.
type SelectionResult struct {
	SessionPath string
	Record      *ConversationRecord
	DisplayInfo string
}

// HistoryEntry maps a loaded ConversationRecord back to provider.Messages for
// the agent runner.
type HistoryEntry = provider.Message
