package trajectory

// ATIF v1.7 specification structs according to Harbor RFC 0001.

type Trajectory struct {
	SchemaVersion string         `json:"schema_version"`
	SessionID     string         `json:"session_id"`
	Agent         Agent          `json:"agent"`
	Steps         []Step         `json:"steps"`
	FinalMetrics  *FinalMetrics  `json:"final_metrics,omitempty"`
	Subagents     []Trajectory   `json:"subagent_trajectories,omitempty"`
	Notes         string         `json:"notes,omitempty"`
	Extra         map[string]any `json:"extra,omitempty"`
}

type Agent struct {
	Name      string         `json:"name"`
	Version   string         `json:"version,omitempty"`
	ModelName string         `json:"model_name,omitempty"`
	Extra     map[string]any `json:"extra,omitempty"`
}

type StepSource string

const (
	StepSourceUser   StepSource = "user"
	StepSourceAgent  StepSource = "agent"
	StepSourceSystem StepSource = "system"
)

type Step struct {
	StepID           int            `json:"step_id"`
	Timestamp        string         `json:"timestamp,omitempty"`
	Source           StepSource     `json:"source"`
	Message          string         `json:"message,omitempty"`
	ReasoningContent string         `json:"reasoning_content,omitempty"`
	ModelName        string         `json:"model_name,omitempty"`
	ToolCalls        []ToolCall     `json:"tool_calls,omitempty"`
	Observation      *Observation   `json:"observation,omitempty"`
	Metrics          *Metrics       `json:"metrics,omitempty"`
	LLMCallCount     *int           `json:"llm_call_count,omitempty"`
	Extra            map[string]any `json:"extra,omitempty"`
}

type ToolCall struct {
	CallID    string         `json:"call_id"`
	ToolName  string         `json:"tool_name"`
	Arguments map[string]any `json:"arguments,omitempty"`
	Extra     map[string]any `json:"extra,omitempty"`
}

type Observation struct {
	Results map[string]ObservationResult `json:"results,omitempty"`
}

type ObservationResult struct {
	SourceCallID          string                 `json:"source_call_id"`
	Content               string                 `json:"content"`
	SubagentTrajectoryRef *SubagentTrajectoryRef `json:"subagent_trajectory_ref,omitempty"`
	Extra                 map[string]any         `json:"extra,omitempty"`
}

type SubagentTrajectoryRef struct {
	TrajectoryID string         `json:"trajectory_id"`
	SessionID    string         `json:"session_id,omitempty"`
	Extra        map[string]any `json:"extra,omitempty"`
}

type Metrics struct {
	PromptTokens     *int     `json:"prompt_tokens,omitempty"`
	CompletionTokens *int     `json:"completion_tokens,omitempty"`
	CachedTokens     *int     `json:"cached_tokens,omitempty"`
	ReasoningTokens  *int     `json:"reasoning_tokens,omitempty"`
	TotalTokens      *int     `json:"total_tokens,omitempty"`
	CostUSD          *float64 `json:"cost_usd,omitempty"`
}

type FinalMetrics struct {
	TotalPromptTokens     *int           `json:"total_prompt_tokens,omitempty"`
	TotalCompletionTokens *int           `json:"total_completion_tokens,omitempty"`
	TotalCachedTokens     *int           `json:"total_cached_tokens,omitempty"`
	TotalReasoningTokens  *int           `json:"total_reasoning_tokens,omitempty"`
	TotalTokens           *int           `json:"total_tokens,omitempty"`
	TotalCostUSD          *float64       `json:"total_cost_usd,omitempty"`
	Extra                 map[string]any `json:"extra,omitempty"`
}
