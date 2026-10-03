package trajectory

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/redact"
	"github.com/undeadindustries/sagittarius/internal/session"
	"github.com/undeadindustries/sagittarius/internal/version"
)

// Options controls FromSession. RedactSecrets strips credentials from text
// and tool arguments. AgentName and AgentVersion override the trajectory
// agent block; an empty version falls back to the session, then the binary.
type Options struct {
	RedactSecrets bool
	AgentName     string
	AgentVersion  string
}

type timedStep struct {
	ts   string
	ord  int
	step Step
}

// FromSession converts a session conversation record (and optional child
// sessions) into an ATIF document Harbor's Trajectory model accepts.
func FromSession(rec *session.ConversationRecord, children map[string]*session.ConversationRecord, opts Options) (*Trajectory, error) {
	if rec == nil {
		return nil, fmt.Errorf("trajectory: nil conversation record")
	}

	agentName := opts.AgentName
	if agentName == "" {
		agentName = "sagittarius"
	}

	modelName := ""
	for _, m := range rec.Messages {
		if m.Round != nil && m.Round.Model != "" {
			modelName = m.Round.Model
			break
		}
	}

	traj := &Trajectory{
		SchemaVersion: SchemaATIFV17,
		SessionID:     rec.SessionID,
		TrajectoryID:  rec.SessionID,
		Agent: Agent{
			Name:      agentName,
			Version:   resolveAgentVersion(opts.AgentVersion, rec.AgentVersion),
			ModelName: modelName,
			Extra:     agentExtra(rec),
		},
	}

	var totalPrompt, totalCompletion, totalCached, totalReasoning, totalTok int
	var totalCost float64
	var hasCost bool
	var legacyMissingTelemetry bool

	var timed []timedStep
	ord := 0

	i := 0
	for i < len(rec.Messages) {
		msg := rec.Messages[i]

		switch msg.Type {
		case session.MessageTypeUser:
			// An unpaired function-response line has no user text. The paired
			// case is consumed by the model-step lookahead above.
			if isFunctionResponseMessage(msg) && extractMessageText(msg, false) == "" {
				i++
				continue
			}
			source := StepSourceUser
			var extra map[string]any
			if msg.Origin == "harness" {
				source = StepSourceSystem
				extra = map[string]any{"origin": "harness"}
			}
			step := Step{
				Timestamp: msg.Timestamp,
				Source:    source,
				Message:   extractMessageText(msg, opts.RedactSecrets),
				Extra:     extra,
			}
			timed = append(timed, timedStep{ts: msg.Timestamp, ord: ord, step: step})
			ord++
			i++

		case session.MessageTypeModel:
			modelText := extractMessageText(msg, opts.RedactSecrets)
			round := msg.Round
			if round == nil {
				legacyMissingTelemetry = true
			}

			step := Step{
				Timestamp: msg.Timestamp,
				Source:    StepSourceAgent,
				Message:   modelText,
			}

			if round != nil {
				step.ModelName = round.Model
				step.Extra = map[string]any{
					"provider":        round.Provider,
					"mode":            round.Mode,
					"agent_kind":      round.AgentKind,
					"latency_ms":      round.LatencyMs,
					"usage_estimated": round.UsageEstimated,
				}
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
				totalPrompt += inT
				totalCompletion += outT
				totalTok += tot

				if round.CachedTokens > 0 {
					c := round.CachedTokens
					m.CachedTokens = &c
					totalCached += c
				}
				if round.CostKnown {
					c := round.CostUSD
					m.CostUSD = &c
					totalCost += c
					hasCost = true
				}
				if round.ReasoningTokens > 0 {
					totalReasoning += round.ReasoningTokens
				}
				m.Extra = metricsExtra(round.ReasoningTokens, tot)
				step.Metrics = m
			} else {
				calls := 1
				step.LLMCallCount = &calls
			}

			var calls []ToolCall
			for _, p := range msg.Content {
				if p.FunctionCall == nil {
					continue
				}
				args := p.FunctionCall.Args
				if args == nil {
					args = map[string]any{}
				} else if opts.RedactSecrets {
					args = redactMap(args)
				}
				calls = append(calls, ToolCall{
					ToolCallID:   p.FunctionCall.ID,
					FunctionName: p.FunctionCall.Name,
					Arguments:    args,
				})
			}
			if len(calls) > 0 {
				step.ToolCalls = calls
			}

			if i+1 < len(rec.Messages) && isFunctionResponseMessage(rec.Messages[i+1]) {
				step.Observation = foldObservation(rec.Messages[i+1], children, opts.RedactSecrets)
				i++
			}

			timed = append(timed, timedStep{ts: msg.Timestamp, ord: ord, step: step})
			ord++
			i++

		default:
			i++
		}
	}

	for _, ev := range rec.Events {
		eventData := ev.Data
		if eventData == nil {
			eventData = map[string]any{}
		}
		dataBytes, err := json.Marshal(eventData)
		if err != nil {
			dataBytes = []byte("{}")
		}
		step := Step{
			Timestamp: ev.Timestamp,
			Source:    StepSourceSystem,
			Message:   fmt.Sprintf("$event: %s %s", ev.Type, string(dataBytes)),
			Extra: map[string]any{
				"event_type": ev.Type,
				"event_data": eventData,
			},
		}
		timed = append(timed, timedStep{ts: ev.Timestamp, ord: ord, step: step})
		ord++
	}

	sort.SliceStable(timed, func(a, b int) bool {
		if timed[a].ts != timed[b].ts {
			return timed[a].ts < timed[b].ts
		}
		return timed[a].ord < timed[b].ord
	})

	traj.Steps = make([]Step, len(timed))
	for n, item := range timed {
		item.step.StepID = n + 1
		traj.Steps[n] = item.step
	}

	if len(children) > 0 {
		ids := make([]string, 0, len(children))
		for id := range children {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(a, b int) bool {
			ca, cb := children[ids[a]], children[ids[b]]
			if ca.StartTime != cb.StartTime {
				return ca.StartTime < cb.StartTime
			}
			return ids[a] < ids[b]
		})
		traj.SubagentTrajectories = make([]Trajectory, 0, len(ids))
		for _, id := range ids {
			childTraj, err := FromSession(children[id], nil, opts)
			if err != nil || childTraj == nil {
				continue
			}
			childTraj.TrajectoryID = id
			traj.SubagentTrajectories = append(traj.SubagentTrajectories, *childTraj)
		}
	}

	stepCount := len(traj.Steps)
	fmExtra := map[string]any{}
	if rec.Outcome != "" {
		fmExtra["outcome"] = rec.Outcome
	}
	if totalReasoning > 0 {
		fmExtra["reasoning_tokens"] = totalReasoning
	}
	if totalTok > 0 {
		fmExtra["total_tokens"] = totalTok
	}
	fm := &FinalMetrics{
		TotalPromptTokens:     &totalPrompt,
		TotalCompletionTokens: &totalCompletion,
		TotalSteps:            &stepCount,
	}
	if len(fmExtra) > 0 {
		fm.Extra = fmExtra
	}
	if totalCached > 0 {
		fm.TotalCachedTokens = &totalCached
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

func resolveAgentVersion(opt, recorded string) string {
	if opt != "" {
		return opt
	}
	if recorded != "" {
		return recorded
	}
	if version.Version != "" {
		return version.Version
	}
	return "unknown"
}

func agentExtra(rec *session.ConversationRecord) map[string]any {
	extra := map[string]any{}
	if rec.Kind != "" {
		extra["kind"] = rec.Kind
	}
	if rec.PersonaPreset != "" {
		extra["persona_preset"] = rec.PersonaPreset
	}
	if len(extra) == 0 {
		return nil
	}
	return extra
}

func metricsExtra(reasoningTokens, totalTokens int) map[string]any {
	extra := map[string]any{}
	if reasoningTokens > 0 {
		extra["reasoning_tokens"] = reasoningTokens
	}
	if totalTokens > 0 {
		extra["total_tokens"] = totalTokens
	}
	if len(extra) == 0 {
		return nil
	}
	return extra
}

func isFunctionResponseMessage(msg session.MessageRecord) bool {
	for _, p := range msg.Content {
		if p.FunctionResponse != nil {
			return true
		}
	}
	return false
}

func foldObservation(msg session.MessageRecord, children map[string]*session.ConversationRecord, redactSec bool) *Observation {
	resultTelemByID := make(map[string]session.ToolResultTelemetry)
	for _, tr := range msg.ToolResults {
		resultTelemByID[tr.ID] = tr
	}

	obs := &Observation{}
	for _, p := range msg.Content {
		fr := p.FunctionResponse
		if fr == nil {
			continue
		}
		contentStr := ""
		if fr.Response != nil {
			b, err := json.Marshal(fr.Response)
			if err == nil {
				contentStr = string(b)
			}
		}
		if redactSec {
			contentStr = redact.Secrets(contentStr)
		}
		obsRes := ObservationResult{
			SourceCallID: fr.ID,
			Content:      contentStr,
		}
		if tr, ok := resultTelemByID[fr.ID]; ok {
			extra := map[string]any{
				"duration_ms": tr.DurationMs,
				"status":      tr.Status,
			}
			if tr.Code != "" {
				extra["code"] = tr.Code
			}
			if tr.ExitCode != nil {
				extra["exit_code"] = *tr.ExitCode
			}
			obsRes.Extra = extra
		}
		for childSessID, childRec := range children {
			if childRec != nil && childRec.ParentCallID == fr.ID {
				obsRes.SubagentTrajectoryRef = []SubagentTrajectoryRef{{
					TrajectoryID: childSessID,
					SessionID:    childSessID,
					Extra: map[string]any{
						"subagent_class": childRec.SubagentClass,
					},
				}}
				break
			}
		}
		obs.Results = append(obs.Results, obsRes)
	}
	if len(obs.Results) == 0 {
		return nil
	}
	return obs
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
