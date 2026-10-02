package trajectory

import (
	"fmt"
)

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

// Validate checks an ATIF trajectory against the Harbor ATIF v1.7 specification rules.
func Validate(traj *Trajectory) []ValidationError {
	var errs []ValidationError

	if traj == nil {
		return []ValidationError{{Field: "root", Message: "trajectory is nil"}}
	}

	if traj.SchemaVersion == "" {
		errs = append(errs, ValidationError{Field: "schema_version", Message: "schema_version is required"})
	}

	if traj.SessionID == "" {
		errs = append(errs, ValidationError{Field: "session_id", Message: "session_id is required"})
	}

	if traj.Agent.Name == "" {
		errs = append(errs, ValidationError{Field: "agent.name", Message: "agent.name is required"})
	}

	seenTrajIDs := make(map[string]bool)
	seenTrajIDs[traj.SessionID] = true

	// Check steps sequentially from 1
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
		case StepSourceUser:
			if len(step.ToolCalls) > 0 {
				errs = append(errs, ValidationError{StepID: step.StepID, Field: "tool_calls", Message: "user steps cannot have tool_calls"})
			}
			if step.Observation != nil {
				errs = append(errs, ValidationError{StepID: step.StepID, Field: "observation", Message: "user steps cannot have observation"})
			}
			if step.Metrics != nil {
				errs = append(errs, ValidationError{StepID: step.StepID, Field: "metrics", Message: "user steps cannot have metrics"})
			}
			if step.LLMCallCount != nil {
				errs = append(errs, ValidationError{StepID: step.StepID, Field: "llm_call_count", Message: "user steps cannot have llm_call_count"})
			}

		case StepSourceSystem:
			if len(step.ToolCalls) > 0 {
				errs = append(errs, ValidationError{StepID: step.StepID, Field: "tool_calls", Message: "system steps cannot have tool_calls"})
			}
			if step.Observation != nil {
				errs = append(errs, ValidationError{StepID: step.StepID, Field: "observation", Message: "system steps cannot have observation"})
			}
			if step.Metrics != nil {
				errs = append(errs, ValidationError{StepID: step.StepID, Field: "metrics", Message: "system steps cannot have metrics"})
			}
			if step.LLMCallCount != nil {
				errs = append(errs, ValidationError{StepID: step.StepID, Field: "llm_call_count", Message: "system steps cannot have llm_call_count"})
			}

		case StepSourceAgent:
			// If LLMCallCount == 0, metrics and reasoning should not be present
			if step.LLMCallCount != nil && *step.LLMCallCount == 0 {
				if step.Metrics != nil {
					errs = append(errs, ValidationError{StepID: step.StepID, Field: "metrics", Message: "metrics cannot be present when llm_call_count is 0"})
				}
				if step.ReasoningContent != "" {
					errs = append(errs, ValidationError{StepID: step.StepID, Field: "reasoning_content", Message: "reasoning_content cannot be present when llm_call_count is 0"})
				}
			}

			// Validate tool call IDs vs observation results
			callIDs := make(map[string]bool)
			for _, tc := range step.ToolCalls {
				if tc.CallID == "" {
					errs = append(errs, ValidationError{StepID: step.StepID, Field: "tool_calls.call_id", Message: "tool call missing call_id"})
				} else {
					callIDs[tc.CallID] = true
				}
			}

			if step.Observation != nil {
				for callID, obsRes := range step.Observation.Results {
					if obsRes.SourceCallID != "" && obsRes.SourceCallID != callID {
						errs = append(errs, ValidationError{
							StepID:  step.StepID,
							Field:   "observation.results.source_call_id",
							Message: fmt.Sprintf("observation result source_call_id %q does not match map key %q", obsRes.SourceCallID, callID),
						})
					}
					if !callIDs[callID] {
						errs = append(errs, ValidationError{
							StepID:  step.StepID,
							Field:   "observation.results",
							Message: fmt.Sprintf("source_call_id %q in observation does not resolve to any tool call in this step", callID),
						})
					}
				}
			}
		default:
			errs = append(errs, ValidationError{StepID: step.StepID, Field: "source", Message: fmt.Sprintf("invalid step source %q", step.Source)})
		}
	}

	// Validate embedded subagents
	for _, sub := range traj.Subagents {
		if sub.SessionID == "" {
			errs = append(errs, ValidationError{Field: "subagent_trajectories.session_id", Message: "subagent trajectory missing session_id"})
		} else if seenTrajIDs[sub.SessionID] {
			errs = append(errs, ValidationError{Field: "subagent_trajectories.session_id", Message: fmt.Sprintf("duplicate trajectory_id %q across subagents", sub.SessionID)})
		} else {
			seenTrajIDs[sub.SessionID] = true
		}

		subErrs := Validate(&sub)
		for _, se := range subErrs {
			errs = append(errs, ValidationError{
				StepID:  se.StepID,
				Field:   fmt.Sprintf("subagent(%s).%s", sub.SessionID, se.Field),
				Message: se.Message,
			})
		}
	}

	return errs
}
