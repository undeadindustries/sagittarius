package hooks

import (
	"time"
)

// HookEventName defines standard lifecycle event names.
type HookEventName string

const (
	EventBeforeTool          HookEventName = "BeforeTool"
	EventAfterTool           HookEventName = "AfterTool"
	EventBeforeAgent         HookEventName = "BeforeAgent"
	EventAfterAgent          HookEventName = "AfterAgent"
	EventBeforeModel         HookEventName = "BeforeModel"
	EventAfterModel          HookEventName = "AfterModel"
	EventBeforeToolSelection HookEventName = "BeforeToolSelection"
	EventSessionStart        HookEventName = "SessionStart"
	EventSessionEnd          HookEventName = "SessionEnd"
	EventPreCompress         HookEventName = "PreCompress"
	EventNotification        HookEventName = "Notification"

	// EventFirstTurn is a Sagittarius extension firing once after the first turn.
	EventFirstTurn HookEventName = "FirstTurn"
)

// ReservedConfigFields lists non-event fields in the "hooks" settings object.
var ReservedConfigFields = map[string]bool{
	"enabled":       true,
	"disabled":      true,
	"notifications": true,
}

// ConfigSource describes where a hook configuration originated.
type ConfigSource string

const (
	SourceRuntime    ConfigSource = "runtime"
	SourceProject    ConfigSource = "project"
	SourceUser       ConfigSource = "user"
	SourceSystem     ConfigSource = "system"
	SourceExtensions ConfigSource = "extensions"
)

// HookType defines the execution engine type.
type HookType string

const (
	TypeCommand HookType = "command"
	TypeRuntime HookType = "runtime"
)

// Exit code semantics.
const (
	ExitCodeSuccess          = 0
	ExitCodeNonBlockingError = 1
	ExitCodeSystemBlock      = 2
)

// HookConfig holds configuration for a single hook command.
type HookConfig struct {
	Type        HookType          `json:"type"`
	Command     string            `json:"command,omitempty"`
	Name        string            `json:"name,omitempty"`
	Description string            `json:"description,omitempty"`
	Timeout     int               `json:"timeout,omitempty"` // seconds, matching gemini-cli and Claude Code
	Source      ConfigSource      `json:"source,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
}

// Key returns a unique identifier string for this hook config.
func (c HookConfig) Key() string {
	if c.Name != "" {
		return c.Name
	}
	return c.Command
}

// HookDefinition groups hook configurations under a matcher.
type HookDefinition struct {
	Matcher    string       `json:"matcher,omitempty"`
	Sequential bool         `json:"sequential,omitempty"`
	Hooks      []HookConfig `json:"hooks"`
}

// HookDecision defines approval/rejection decisions.
type HookDecision string

const (
	DecisionAllow   HookDecision = "allow"
	DecisionApprove HookDecision = "approve"
	DecisionDeny    HookDecision = "deny"
	DecisionBlock   HookDecision = "block"
	DecisionAsk     HookDecision = "ask"
)

// HookInput is the base payload written to stdin as JSON.
type HookInput struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	CWD            string `json:"cwd"`
	HookEventName  string `json:"hook_event_name"`
	Timestamp      string `json:"timestamp"`
	TurnIndex      int    `json:"turn_index"`

	// Event-specific fields (flattened in JSON via omitempty pointers or inline)
	Prompt              string         `json:"prompt,omitempty"`
	PromptResponse      string         `json:"prompt_response,omitempty"`
	StopHookActive      bool           `json:"stop_hook_active,omitempty"`
	ToolName            string         `json:"tool_name,omitempty"`
	ToolInput           map[string]any `json:"tool_input,omitempty"`
	ToolResponse        map[string]any `json:"tool_response,omitempty"`
	OriginalRequestName string         `json:"original_request_name,omitempty"`
	McpContext          map[string]any `json:"mcp_context,omitempty"`
	SessionStartSource  string         `json:"source,omitempty"`
	SessionEndReason    string         `json:"reason,omitempty"`
	PreCompressTrigger  string         `json:"trigger,omitempty"`
	NotificationType    string         `json:"notification_type,omitempty"`
	NotificationMessage string         `json:"message,omitempty"`
	NotificationDetails map[string]any `json:"details,omitempty"`
}

// NewHookInput constructs a base HookInput populated with timestamp.
func NewHookInput(sessionID, transcriptPath, cwd string, event HookEventName, turnIndex int) HookInput {
	return HookInput{
		SessionID:      sessionID,
		TranscriptPath: transcriptPath,
		CWD:            cwd,
		HookEventName:  string(event),
		Timestamp:      time.Now().UTC().Format(time.RFC3339),
		TurnIndex:      turnIndex,
	}
}

// HookOutput is parsed from stdout JSON.
type HookOutput struct {
	Continue           *bool          `json:"continue,omitempty"`
	StopReason         string         `json:"stopReason,omitempty"`
	SuppressOutput     bool           `json:"suppressOutput,omitempty"`
	SystemMessage      string         `json:"systemMessage,omitempty"`
	Decision           HookDecision   `json:"decision,omitempty"`
	Reason             string         `json:"reason,omitempty"`
	HookSpecificOutput map[string]any `json:"hookSpecificOutput,omitempty"`
}

// IsBlocking returns true if the hook decision is "deny" or "block".
func (o *HookOutput) IsBlocking() bool {
	if o == nil {
		return false
	}
	return o.Decision == DecisionDeny || o.Decision == DecisionBlock
}

// ShouldStop returns true if continue is explicitly false.
func (o *HookOutput) ShouldStop() bool {
	if o == nil || o.Continue == nil {
		return false
	}
	return !*o.Continue
}

// EffectiveReason returns stopReason or reason or fallback.
func (o *HookOutput) EffectiveReason(fallback string) string {
	if o == nil {
		return fallback
	}
	if o.StopReason != "" {
		return o.StopReason
	}
	if o.Reason != "" {
		return o.Reason
	}
	return fallback
}

// AdditionalContext returns string context from hookSpecificOutput if present.
func (o *HookOutput) AdditionalContext() string {
	if o == nil || o.HookSpecificOutput == nil {
		return ""
	}
	v, ok := o.HookSpecificOutput["additionalContext"]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// ModifiedToolInput returns modified tool input from hookSpecificOutput if present.
func (o *HookOutput) ModifiedToolInput() map[string]any {
	if o == nil || o.HookSpecificOutput == nil {
		return nil
	}
	v, ok := o.HookSpecificOutput["tool_input"]
	if !ok {
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	return m
}

// ShouldClearContext returns true if hookSpecificOutput requests clearing context.
func (o *HookOutput) ShouldClearContext() bool {
	if o == nil || o.HookSpecificOutput == nil {
		return false
	}
	v, ok := o.HookSpecificOutput["clearContext"]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}

// TailToolCallRequest returns tail tool call request if present in hookSpecificOutput.
type TailToolCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

// TailToolCall returns tail tool call request if present.
func (o *HookOutput) TailToolCall() *TailToolCall {
	if o == nil || o.HookSpecificOutput == nil {
		return nil
	}
	v, ok := o.HookSpecificOutput["tailToolCallRequest"]
	if !ok {
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	name, _ := m["name"].(string)
	if name == "" {
		return nil
	}
	args, _ := m["args"].(map[string]any)
	return &TailToolCall{Name: name, Args: args}
}

// ExecutionResult holds the output and metrics from running a hook.
type ExecutionResult struct {
	HookConfig   HookConfig    `json:"hookConfig"`
	EventName    HookEventName `json:"eventName"`
	Success      bool          `json:"success"`
	Output       *HookOutput   `json:"output,omitempty"`
	OutputFormat string        `json:"outputFormat,omitempty"` // "json" or "text"
	Stdout       string        `json:"stdout,omitempty"`
	Stderr       string        `json:"stderr,omitempty"`
	ExitCode     int           `json:"exitCode"`
	Duration     time.Duration `json:"duration"`
	Error        error         `json:"error,omitempty"`
}
