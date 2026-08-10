package session

import (
	"github.com/undeadindustries/sagittarius/internal/goal"
	"github.com/undeadindustries/sagittarius/internal/grill"
	"github.com/undeadindustries/sagittarius/internal/provider"
)

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
	Branch      string `json:"branch,omitempty"` // display-only; never validated on read
	Kind        string `json:"kind,omitempty"`   // "main" | "subagent"
	// CleanExit is set by a $set line when the session's Runner.Close() runs on
	// a normal shutdown. Its absence is the unclean-exit signal (SIGHUP from a
	// dropped connection, a crash, or kill -9 all skip deferred cleanup).
	CleanExit     bool            `json:"cleanExit,omitempty"`
	SessionGrants []string        `json:"sessionGrants,omitempty"`
	Goal          *goal.Snapshot  `json:"goal,omitempty"`
	Grill         *grill.Snapshot `json:"grill,omitempty"`
	// Constraints is a pointer to distinguish "this $set line did not touch
	// constraints" (nil pointer, omitted from JSON) from "constraints were
	// explicitly cleared" (non-nil pointer to an empty slice, marshaled as
	// "[]"). A plain []string field cannot make that distinction: an empty
	// slice and an absent key both unmarshal to nil, so a clear would be
	// silently lost on the next $set-driven merge (see applyMetaUpdate).
	Constraints *[]string `json:"constraints,omitempty"`
	// ReadOnly follows the Constraints pointer pattern: nil = no change,
	// non-nil = explicitly set to on or off.
	ReadOnly *bool `json:"readOnly,omitempty"`
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
	SessionID     string
	ProjectHash   string
	StartTime     string
	LastUpdated   string
	Summary       string
	Branch        string
	Kind          string
	CleanExit     bool
	SessionGrants []string
	Goal          *goal.Snapshot
	Grill         *grill.Snapshot
	// Constraints holds standing session constraints (see internal/agent's
	// Runner.Constraints), or nil when none were ever set. Unlike
	// MetadataRecord.Constraints this is a plain slice: the pointer indirection
	// exists only to make the $set merge correct, not for external consumers.
	Constraints []string
	// ReadOnly holds the durable read-only posture setting.
	ReadOnly *bool
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
	// Branch is the recorded git branch (display-only; empty when not a repo).
	Branch string
	// CleanExit is true when the session ended via a normal shutdown.
	CleanExit bool
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
