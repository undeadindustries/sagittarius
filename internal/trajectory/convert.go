package trajectory

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/redact"
	"github.com/undeadindustries/sagittarius/internal/session"
)

type Options struct {
	RedactSecrets bool
	AgentName     string
	AgentVersion  string
}

// FromSession converts a session conversation record (and optional child sessions) into ATIF v1.7.
func FromSession(rec *session.ConversationRecord, children map[string]*session.ConversationRecord, opts Options) (*Trajectory, error) {
	if rec == nil {
		return nil, fmt.Errorf("trajectory: nil conversation record")
	}

	agentName := opts.AgentName
	if agentName == "" {
		agentName = "sagittarius"
	}
	agentVersion := opts.AgentVersion
	if agentVersion == "" && rec.AgentVersion != "" {
		agentVersion = rec.AgentVersion
	}

	modelName := ""
	for _, m := range rec.Messages {
		if m.Round != nil && m.Round.Model != "" {
			modelName = m.Round.Model
			break
		}
	}

	traj := &Trajectory{
		SchemaVersion: "1.7",
		SessionID:     rec.SessionID,
		Agent: Agent{
			Name:      agentName,
			Version:   agentVersion,
			ModelName: modelName,
			Extra: map[string]any{
				"kind":           rec.Kind,
				"persona_preset": rec.PersonaPreset,
			},
		},
		Steps: make([]Step, 0, len(rec.Messages)),
	}

	var totalPrompt, totalCompletion, totalCached, totalReasoning, totalTok int
	var totalCost float64
	var hasCost bool
	var legacyMissingTelemetry bool

	stepCounter := 1

	// Track function responses by CallID for folding into the preceding agent step
	// We iterate messages in order.
	i := 0
	for i < len(rec.Messages) {
		msg := rec.Messages[i]

		switch msg.Type {
		case session.MessageTypeUser:
			source := StepSourceUser
			extra := map[string]any{}
			if msg.Origin == "harness" {
				source = StepSourceSystem
				extra["origin"] = "harness"
			}
			userText := extractMessageText(msg, opts.RedactSecrets)
			step := Step{
				StepID:    stepCounter,
				Timestamp: msg.Timestamp,
				Source:    source,
				Message:   userText,
				Extra:     extra,
			}
			stepCounter++
			traj.Steps = append(traj.Steps, step)
			i++

		case session.MessageTypeModel:
			modelText := extractMessageText(msg, opts.RedactSecrets)
			round := msg.Round
			if round == nil {
				legacyMissingTelemetry = true
			}

			step := Step{
				StepID:    stepCounter,
				Timestamp: msg.Timestamp,
				Source:    StepSourceAgent,
				Message:   modelText,
				Extra:     map[string]any{},
			}
			stepCounter++

			if round != nil {
				step.ModelName = round.Model
				step.Extra["provider"] = round.Provider
				step.Extra["mode"] = round.Mode
				step.Extra["agent_kind"] = round.AgentKind
				step.Extra["latency_ms"] = round.LatencyMs
				step.Extra["usage_estimated"] = round.UsageEstimated
				if round.SystemPromptHash != "" {
					step.Extra["system_prompt_hash"] = round.SystemPromptHash
				}

				llmCalls := round.LLMCalls
				if llmCalls <= 0 {
					llmCalls = 1
				}
				step.LLMCallCount = &llmCalls

				if round.Reasoning != "" {
					reasoning := round.Reasoning
					if opts.RedactSecrets {
						reasoning = redact.Secrets(reasoning)
					}
					step.ReasoningContent = reasoning
				}

				m := &Metrics{}
				inT := round.InputTokens
				outT := round.OutputTokens
				m.PromptTokens = &inT
				m.CompletionTokens = &outT
				tot := inT + outT
				m.TotalTokens = &tot
				totalPrompt += inT
				totalCompletion += outT
				totalTok += tot

				if round.CachedTokens > 0 {
					c := round.CachedTokens
					m.CachedTokens = &c
					totalCached += c
				}
				if round.ReasoningTokens > 0 {
					r := round.ReasoningTokens
					m.ReasoningTokens = &r
					totalReasoning += r
				}
				if round.CostKnown {
					c := round.CostUSD
					m.CostUSD = &c
					totalCost += c
					hasCost = true
				}
				step.Metrics = m
			} else {
				calls := 1
				step.LLMCallCount = &calls
			}

			// Map tool calls from Part.FunctionCall
			var calls []ToolCall
			for _, p := range msg.Content {
				if p.FunctionCall != nil {
					args := p.FunctionCall.Args
					if opts.RedactSecrets && args != nil {
						args = redactMap(args)
					}
					calls = append(calls, ToolCall{
						CallID:    p.FunctionCall.ID,
						ToolName:  p.FunctionCall.Name,
						Arguments: args,
					})
				}
			}
			if len(calls) > 0 {
				step.ToolCalls = calls
			}

			// Lookahead: is next message containing function responses?
			if i+1 < len(rec.Messages) {
				nextMsg := rec.Messages[i+1]
				var funcParts []*session.FuncResponsePart
				for _, p := range nextMsg.Content {
					if p.FunctionResponse != nil {
						funcParts = append(funcParts, p.FunctionResponse)
					}
				}

				if len(funcParts) > 0 {
					obs := &Observation{
						Results: make(map[string]ObservationResult, len(funcParts)),
					}

					// Map ToolResults telemetry if available
					resultTelemByID := make(map[string]session.ToolResultTelemetry)
					for _, tr := range nextMsg.ToolResults {
						resultTelemByID[tr.ID] = tr
					}

					for _, fr := range funcParts {
						contentStr := ""
						if fr.Response != nil {
							b, _ := json.Marshal(fr.Response)
							contentStr = string(b)
						}
						if opts.RedactSecrets {
							contentStr = redact.Secrets(contentStr)
						}

						obsRes := ObservationResult{
							SourceCallID: fr.ID,
							Content:      contentStr,
							Extra:        map[string]any{},
						}

						if tr, ok := resultTelemByID[fr.ID]; ok {
							obsRes.Extra["duration_ms"] = tr.DurationMs
							obsRes.Extra["status"] = tr.Status
							if tr.Code != "" {
								obsRes.Extra["code"] = tr.Code
							}
							if tr.ExitCode != nil {
								obsRes.Extra["exit_code"] = *tr.ExitCode
							}
						}

						// Check if this tool call corresponds to a subagent child session
						for childSessID, childRec := range children {
							if childRec.ParentCallID == fr.ID {
								obsRes.SubagentTrajectoryRef = &SubagentTrajectoryRef{
									TrajectoryID: childSessID,
									SessionID:    childSessID,
									Extra: map[string]any{
										"subagent_class": childRec.SubagentClass,
									},
								}
								break
							}
						}

						obs.Results[fr.ID] = obsRes
					}
					step.Observation = obs
					i++ // consumed function response message as well
				}
			}

			traj.Steps = append(traj.Steps, step)
			i++

		default:
			i++
		}
	}

	// Insert events as system steps if any
	for _, ev := range rec.Events {
		eventData := ev.Data
		if eventData == nil {
			eventData = map[string]any{}
		}
		dataBytes, _ := json.Marshal(eventData)
		step := Step{
			StepID:    stepCounter,
			Timestamp: ev.Timestamp,
			Source:    StepSourceSystem,
			Message:   fmt.Sprintf("$event: %s %s", ev.Type, string(dataBytes)),
			Extra: map[string]any{
				"event_type": ev.Type,
				"event_data": eventData,
			},
		}
		stepCounter++
		traj.Steps = append(traj.Steps, step)
	}

	// Subagent trajectories
	if len(children) > 0 {
		traj.Subagents = make([]Trajectory, 0, len(children))
		for _, childRec := range children {
			childTraj, err := FromSession(childRec, nil, opts)
			if err == nil && childTraj != nil {
				traj.Subagents = append(traj.Subagents, *childTraj)
			}
		}
	}

	// Final metrics
	fm := &FinalMetrics{
		TotalPromptTokens:     &totalPrompt,
		TotalCompletionTokens: &totalCompletion,
		TotalTokens:           &totalTok,
		Extra: map[string]any{
			"outcome": rec.Outcome,
		},
	}
	if totalCached > 0 {
		fm.TotalCachedTokens = &totalCached
	}
	if totalReasoning > 0 {
		fm.TotalReasoningTokens = &totalReasoning
	}
	if hasCost {
		fm.TotalCostUSD = &totalCost
	}
	traj.FinalMetrics = fm

	if legacyMissingTelemetry {
		traj.Notes = "Legacy session: partial or missing operational telemetry."
	}

	return traj, nil
}

func extractMessageText(msg session.MessageRecord, redactSec bool) string {
	var sb strings.Builder
	for _, part := range msg.Content {
		if part.Text != "" {
			sb.WriteString(part.Text)
		}
	}
	res := sb.String()
	if redactSec {
		res = redact.Secrets(res)
	}
	return res
}

func redactMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		switch val := v.(type) {
		case string:
			out[k] = redact.Secrets(val)
		case map[string]any:
			out[k] = redactMap(val)
		case []any:
			out[k] = redactSlice(val)
		default:
			out[k] = v
		}
	}
	return out
}

func redactSlice(in []any) []any {
	out := make([]any, len(in))
	for i, v := range in {
		switch val := v.(type) {
		case string:
			out[i] = redact.Secrets(val)
		case map[string]any:
			out[i] = redactMap(val)
		case []any:
			out[i] = redactSlice(val)
		default:
			out[i] = v
		}
	}
	return out
}
