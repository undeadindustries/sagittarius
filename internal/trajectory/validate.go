package trajectory

import "fmt"

// ValidationError represents an error during ATIF trajectory validation.
type ValidationError struct {
	StepID  int    `json:"step_id,omitempty"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e ValidationError) Error() string {
	if e.StepID > 0 {
		return fmt.Sprintf("step %d [%s]: %s", e.StepID, e.Field, e.Message)
	}
	return fmt.Sprintf("[%s]: %s", e.Field, e.Message)
}

// Validate checks an ATIF trajectory against the rules Harbor's Pydantic
// models enforce: schema version, sequential step ids, agent-only fields,
// tool-call references, and unique embedded trajectory ids.
func Validate(traj *Trajectory) []ValidationError {
	var errs []ValidationError

	if traj == nil {
		return []ValidationError{{Field: "root", Message: "trajectory is nil"}}
	}

	if _, ok := allowedSchemaVersions[traj.SchemaVersion]; !ok {
		errs = append(errs, ValidationError{
			Field:   "schema_version",
			Message: fmt.Sprintf("schema_version %q is not a Harbor ATIF version", traj.SchemaVersion),
		})
	}

	if traj.Agent.Name == "" {
		errs = append(errs, ValidationError{Field: "agent.name", Message: "agent.name is required"})
	}
	if traj.Agent.Version == "" {
		errs = append(errs, ValidationError{Field: "agent.version", Message: "agent.version is required"})
	}
	if len(traj.Steps) == 0 {
		errs = append(errs, ValidationError{Field: "steps", Message: "steps must be non-empty"})
	}

	for idx, step := range traj.Steps {
		expectedStepID := idx + 1
		if step.StepID != expectedStepID {
			errs = append(errs, ValidationError{
				StepID:  step.StepID,
				Field:   "step_id",
				Message: fmt.Sprintf("step_id must be strictly sequential starting from 1; expected %d, got %d", expectedStepID, step.StepID),
			})
		}

		switch step.Source {
		case StepSourceUser, StepSourceSystem:
			errs = append(errs, agentOnlyFieldErrors(step)...)
		case StepSourceAgent:
			if step.LLMCallCount != nil && *step.LLMCallCount == 0 {
				if step.Metrics != nil {
					errs = append(errs, ValidationError{StepID: step.StepID, Field: "metrics", Message: "metrics cannot be present when llm_call_count is 0"})
				}
				if step.ReasoningContent != "" {
					errs = append(errs, ValidationError{StepID: step.StepID, Field: "reasoning_content", Message: "reasoning_content cannot be present when llm_call_count is 0"})
				}
			}
			errs = append(errs, toolCallErrors(step)...)
		default:
			errs = append(errs, ValidationError{StepID: step.StepID, Field: "source", Message: fmt.Sprintf("invalid step source %q", step.Source)})
		}
	}

	seenTrajIDs := map[string]struct{}{}
	if traj.TrajectoryID != "" {
		seenTrajIDs[traj.TrajectoryID] = struct{}{}
	}
	for i, sub := range traj.SubagentTrajectories {
		if sub.TrajectoryID == "" {
			errs = append(errs, ValidationError{
				Field:   "subagent_trajectories.trajectory_id",
				Message: fmt.Sprintf("subagent_trajectories[%d].trajectory_id is required", i),
			})
		} else if _, dup := seenTrajIDs[sub.TrajectoryID]; dup {
			errs = append(errs, ValidationError{
				Field:   "subagent_trajectories.trajectory_id",
				Message: fmt.Sprintf("duplicate trajectory_id %q", sub.TrajectoryID),
			})
		} else {
			seenTrajIDs[sub.TrajectoryID] = struct{}{}
		}
		for _, se := range Validate(&sub) {
			errs = append(errs, ValidationError{
				StepID:  se.StepID,
				Field:   fmt.Sprintf("subagent(%s).%s", sub.TrajectoryID, se.Field),
				Message: se.Message,
			})
		}
	}

	return errs
}

func agentOnlyFieldErrors(step Step) []ValidationError {
	var errs []ValidationError
	if step.ModelName != "" {
		errs = append(errs, ValidationError{StepID: step.StepID, Field: "model_name", Message: "model_name is only valid when source is agent"})
	}
	if step.ReasoningContent != "" {
		errs = append(errs, ValidationError{StepID: step.StepID, Field: "reasoning_content", Message: "reasoning_content is only valid when source is agent"})
	}
	if len(step.ToolCalls) > 0 {
		errs = append(errs, ValidationError{StepID: step.StepID, Field: "tool_calls", Message: fmt.Sprintf("%s steps cannot have tool_calls", step.Source)})
	}
	if step.Observation != nil {
		errs = append(errs, ValidationError{StepID: step.StepID, Field: "observation", Message: fmt.Sprintf("%s steps cannot have observation", step.Source)})
	}
	if step.Metrics != nil {
		errs = append(errs, ValidationError{StepID: step.StepID, Field: "metrics", Message: fmt.Sprintf("%s steps cannot have metrics", step.Source)})
	}
	if step.LLMCallCount != nil {
		errs = append(errs, ValidationError{StepID: step.StepID, Field: "llm_call_count", Message: fmt.Sprintf("%s steps cannot have llm_call_count", step.Source)})
	}
	return errs
}

func toolCallErrors(step Step) []ValidationError {
	var errs []ValidationError
	callIDs := make(map[string]struct{}, len(step.ToolCalls))
	for _, tc := range step.ToolCalls {
		if tc.ToolCallID == "" {
			errs = append(errs, ValidationError{StepID: step.StepID, Field: "tool_calls.tool_call_id", Message: "tool call missing tool_call_id"})
			continue
		}
		if tc.FunctionName == "" {
			errs = append(errs, ValidationError{StepID: step.StepID, Field: "tool_calls.function_name", Message: "tool call missing function_name"})
		}
		if tc.Arguments == nil {
			errs = append(errs, ValidationError{StepID: step.StepID, Field: "tool_calls.arguments", Message: "tool call arguments are required"})
		}
		callIDs[tc.ToolCallID] = struct{}{}
	}
	if step.Observation == nil {
		return errs
	}
	for _, obsRes := range step.Observation.Results {
		if obsRes.SourceCallID == "" {
			continue
		}
		if _, ok := callIDs[obsRes.SourceCallID]; !ok {
			errs = append(errs, ValidationError{
				StepID:  step.StepID,
				Field:   "observation.results.source_call_id",
				Message: fmt.Sprintf("source_call_id %q does not resolve to a tool call in this step", obsRes.SourceCallID),
			})
		}
	}
	return errs
}
