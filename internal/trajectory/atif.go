package trajectory

// ATIF types matching Harbor's Pydantic models (schema ATIF-v1.7).
// Harbor sets extra="forbid" on every model, so JSON field names and the
// set of fields are the schema. Fields Harbor does not know belong in extra.

// SchemaATIFV17 is the schema_version Harbor accepts for this exporter.
const SchemaATIFV17 = "ATIF-v1.7"

// allowedSchemaVersions are the literals Harbor's Trajectory model accepts.
var allowedSchemaVersions = map[string]struct{}{
	"ATIF-v1.0": {},
	"ATIF-v1.1": {},
	"ATIF-v1.2": {},
	"ATIF-v1.3": {},
	"ATIF-v1.4": {},
	"ATIF-v1.5": {},
	"ATIF-v1.6": {},
	"ATIF-v1.7": {},
	"ATIF-v1.8": {},
}

// Trajectory is one ATIF document. SubagentTrajectories are embedded children;
// each must carry a unique TrajectoryID.
type Trajectory struct {
	SchemaVersion        string         `json:"schema_version"`
	SessionID            string         `json:"session_id,omitempty"`
	TrajectoryID         string         `json:"trajectory_id,omitempty"`
	Agent                Agent          `json:"agent"`
	Steps                []Step         `json:"steps"`
	Notes                string         `json:"notes,omitempty"`
	FinalMetrics         *FinalMetrics  `json:"final_metrics,omitempty"`
	SubagentTrajectories []Trajectory   `json:"subagent_trajectories,omitempty"`
	Extra                map[string]any `json:"extra,omitempty"`
}

// Agent is the harness that produced the trajectory. Version is required.
type Agent struct {
	Name      string         `json:"name"`
	Version   string         `json:"version"`
	ModelName string         `json:"model_name,omitempty"`
	Extra     map[string]any `json:"extra,omitempty"`
}

type StepSource string

const (
	StepSourceUser   StepSource = "user"
	StepSourceAgent  StepSource = "agent"
	StepSourceSystem StepSource = "system"
)

// Step is one turn. Message is required by Harbor even when it is empty,
// so the JSON tag must not use omitempty.
type Step struct {
	StepID           int            `json:"step_id"`
	Timestamp        string         `json:"timestamp,omitempty"`
	Source           StepSource     `json:"source"`
	ModelName        string         `json:"model_name,omitempty"`
	Message          string         `json:"message"`
	ReasoningContent string         `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall     `json:"tool_calls,omitempty"`
	Observation      *Observation   `json:"observation,omitempty"`
	Metrics          *Metrics       `json:"metrics,omitempty"`
	LLMCallCount     *int           `json:"llm_call_count,omitempty"`
	Extra            map[string]any `json:"extra,omitempty"`
}

// ToolCall is one function invocation. Arguments is required and may be {}.
type ToolCall struct {
	ToolCallID   string         `json:"tool_call_id"`
	FunctionName string         `json:"function_name"`
	Arguments    map[string]any `json:"arguments"`
	Extra        map[string]any `json:"extra,omitempty"`
}

// Observation is the environment feedback for a step's tool calls.
// Results is a list; Harbor rejects a map.
type Observation struct {
	Results []ObservationResult `json:"results"`
}

// ObservationResult is one tool result. SubagentTrajectoryRef is a list.
type ObservationResult struct {
	SourceCallID          string                  `json:"source_call_id,omitempty"`
	Content               string                  `json:"content,omitempty"`
	SubagentTrajectoryRef []SubagentTrajectoryRef `json:"subagent_trajectory_ref,omitempty"`
	Extra                 map[string]any          `json:"extra,omitempty"`
}

// SubagentTrajectoryRef points at an embedded child by TrajectoryID.
type SubagentTrajectoryRef struct {
	TrajectoryID string         `json:"trajectory_id,omitempty"`
	SessionID    string         `json:"session_id,omitempty"`
	Extra        map[string]any `json:"extra,omitempty"`
}

// Metrics is per-step token usage. Harbor has no reasoning_tokens or
// total_tokens fields; those go in Extra.
type Metrics struct {
	PromptTokens     *int           `json:"prompt_tokens,omitempty"`
	CompletionTokens *int           `json:"completion_tokens,omitempty"`
	CachedTokens     *int           `json:"cached_tokens,omitempty"`
	CostUSD          *float64       `json:"cost_usd,omitempty"`
	Extra            map[string]any `json:"extra,omitempty"`
}

// FinalMetrics is the trajectory total. Harbor has total_steps but not
// total_tokens or total_reasoning_tokens; those go in Extra.
type FinalMetrics struct {
	TotalPromptTokens     *int           `json:"total_prompt_tokens,omitempty"`
	TotalCompletionTokens *int           `json:"total_completion_tokens,omitempty"`
	TotalCachedTokens     *int           `json:"total_cached_tokens,omitempty"`
	TotalCostUSD          *float64       `json:"total_cost_usd,omitempty"`
	TotalSteps            *int           `json:"total_steps,omitempty"`
	Extra                 map[string]any `json:"extra,omitempty"`
}
