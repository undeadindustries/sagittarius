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

// RoundTelemetry carries per-model-generation operational telemetry.
type RoundTelemetry struct {
	Provider         string  `json:"provider,omitempty"`
	Model            string  `json:"model,omitempty"`
	Mode             string  `json:"mode,omitempty"`
	AgentKind        string  `json:"agentKind,omitempty"` // "main", "subagent", "evaluator"
	InputTokens      int     `json:"inputTokens,omitempty"`
	OutputTokens     int     `json:"outputTokens,omitempty"`
	CachedTokens     int     `json:"cachedTokens,omitempty"`
	ReasoningTokens  int     `json:"reasoningTokens,omitempty"`
	CostUSD          float64 `json:"costUSD,omitempty"`
	CostKnown        bool    `json:"costKnown,omitempty"`
	UsageEstimated   bool    `json:"usageEstimated,omitempty"`
	LatencyMs        int64   `json:"latencyMs,omitempty"`
	LLMCalls         int     `json:"llmCalls,omitempty"` // count of inferences for this round (retries/cuts)
	HadReasoning     bool    `json:"hadReasoning,omitempty"`
	Reasoning        string  `json:"reasoning,omitempty"`    // populated only when opted in
	FinishReason     string  `json:"finishReason,omitempty"` // provider's abnormal stop reason, empty for a normal finish
	SystemPromptHash string  `json:"systemPromptHash,omitempty"`
}

// ToolResultTelemetry carries per-tool execution telemetry.
type ToolResultTelemetry struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	DurationMs int64  `json:"durationMs,omitempty"`
	Status     string `json:"status,omitempty"` // "ok", "error", "denied"
	Code       string `json:"code,omitempty"`   // ErrorCode string (e.g. INVALID_ARGS, MODE_RESTRICTION)
	ExitCode   *int   `json:"exitCode,omitempty"`
}

// EventRecord captures asynchronous agent lifecycle events (compression, budget cut, etc.).
type EventRecord struct {
	Type      string                 `json:"type"`
	Timestamp string                 `json:"timestamp"`
	Data      map[string]interface{} `json:"data,omitempty"`
}

// MessageRecord is one line of JSONL (a single turn).
type MessageRecord struct {
	ID          string                `json:"id"`
	Timestamp   string                `json:"timestamp"`
	Type        MessageType           `json:"type"`
	Origin      string                `json:"origin,omitempty"`      // "user" (default) or "harness"
	Round       *RoundTelemetry       `json:"round,omitempty"`       // populated on model turns
	ToolResults []ToolResultTelemetry `json:"toolResults,omitempty"` // populated on functionResponse turns
	// Content is []Part (serialised as JSON array).
	Content   []Part           `json:"content"`
	ToolCalls []ToolCallRecord `json:"toolCalls,omitempty"`
}

// End-of-run outcome values stored on MetadataRecord.Outcome. A later $set
// replaces an earlier one, so a turn records exactly one of these.
const (
	// OutcomeDone is a turn that finished without hitting the tool-round cap.
	OutcomeDone = "done"
	// OutcomeMaxRounds is a turn stopped because it exhausted max tool rounds.
	OutcomeMaxRounds = "max_rounds"
)

// MetadataRecord is the first line of each JSONL file and any $set update.
type MetadataRecord struct {
	SessionID       string `json:"sessionId"`
	ProjectHash     string `json:"projectHash"`
	StartTime       string `json:"startTime"`
	LastUpdated     string `json:"lastUpdated"`
	Summary         string `json:"summary,omitempty"`
	Branch          string `json:"branch,omitempty"` // display-only; never validated on read
	Kind            string `json:"kind,omitempty"`   // "main" | "subagent" | "evaluator"
	ParentSessionID string `json:"parentSessionId,omitempty"`
	ParentCallID    string `json:"parentCallId,omitempty"`
	SubagentClass   string `json:"subagentClass,omitempty"`
	AgentVersion    string `json:"agentVersion,omitempty"`
	PersonaPreset   string `json:"personaPreset,omitempty"`
	Outcome         string `json:"outcome,omitempty"` // OutcomeDone | OutcomeMaxRounds | "canceled" | "error"
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
	// Scratchpad follows the same pointer pattern: nil = this $set line did not
	// touch the scratchpad, non-nil pointer to "" = the model or user cleared
	// it. A plain string cannot distinguish those, so a clear would be lost on
	// the next merge (see applyMetaUpdate).
	Scratchpad *string `json:"scratchpad,omitempty"`
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
	SessionID       string
	ProjectHash     string
	StartTime       string
	LastUpdated     string
	Summary         string
	Branch          string
	Kind            string
	ParentSessionID string
	ParentCallID    string
	SubagentClass   string
	AgentVersion    string
	PersonaPreset   string
	Outcome         string
	CleanExit       bool
	SessionGrants   []string
	Goal            *goal.Snapshot
	Grill           *grill.Snapshot
	// Constraints holds standing session constraints (see internal/agent's
	// Runner.Constraints), or nil when none were ever set. Unlike
	// MetadataRecord.Constraints this is a plain slice: the pointer indirection
	// exists only to make the $set merge correct, not for external consumers.
	Constraints []string
	// ReadOnly holds the durable read-only posture setting. Sessions written
	// before AD-107 may also carry a readOnlyConversational key; it is ignored
	// on load, so an inferred lock never survives a resume.
	ReadOnly *bool
	// Scratchpad holds the model's working-memory note (see internal/agent's
	// Runner.Scratchpad), or "" when none was ever set. As with Constraints the
	// pointer indirection exists only for the $set merge, not for consumers.
	Scratchpad string
	Events     []EventRecord
	Messages   []MessageRecord
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
