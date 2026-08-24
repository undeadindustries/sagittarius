package agent

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/undeadindustries/sagittarius/internal/goal"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

// evaluateGoalTurn evaluates the active goal, if any, and returns true if the
// loop should continue to the next iteration. It emits appropriate stream events.
func (r *Runner) evaluateGoalTurn(ctx context.Context, out chan<- ui.StreamEvent, lastAssistantText string) bool {
	r.goalMu.RLock()
	g := r.activeGoal
	r.goalMu.RUnlock()

	if g == nil || g.Status != goal.StatusActive {
		return false
	}

	r.goalMu.Lock()
	g.TurnCount++
	// Note: g is a pointer, so we are mutating it directly.
	r.goalMu.Unlock()

	// Notify status change to UI (footer update)
	r.syncContextGauge()

	settings := r.settingsSnapshot()
	timeoutSecs := goal.DefaultEvaluatorTimeoutSeconds
	if settings != nil && settings.Sagittarius != nil && settings.Sagittarius.Goal != nil && settings.Sagittarius.Goal.EvaluatorTimeout != nil {
		timeoutSecs = *settings.Sagittarius.Goal.EvaluatorTimeout
	}

	evalCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSecs)*time.Second)
	defer cancel()

	if !r.hasDedicatedEvaluator() {
		r.evaluatorSelfJudgeWarned.Do(func() {
			out <- ui.StreamEvent{Type: ui.StreamInfo, Text: "Evaluator: " + r.GoalEvaluatorLabel()}
		})
	}

	detContext, early, stop, err := goal.PrepareEvaluation(evalCtx, g, r.workDir)
	if err != nil {
		slog.Error("goal evaluation failed", "err", err)
		out <- ui.StreamEvent{Type: ui.StreamError, Err: fmt.Errorf("goal: evaluate: %w", err)}
		return false
	}
	if stop {
		r.recordGoalDecision(early)
		r.syncContextGauge()
		out <- ui.StreamEvent{Type: ui.StreamInfo, Text: fmt.Sprintf("Goal paused/blocked: %s", early.Reason)}
		return false
	}

	transcript := r.GoalTranscript()
	dec, warn, err := r.runEvaluatorAgent(evalCtx, g, transcript, detContext)
	if err != nil {
		slog.Error("goal evaluation failed", "err", err)
		out <- ui.StreamEvent{Type: ui.StreamError, Err: fmt.Errorf("goal: evaluate: %w", err)}
		return false
	}
	if warn != "" {
		out <- ui.StreamEvent{Type: ui.StreamInfo, Text: warn}
	}

	r.recordGoalDecision(dec)
	r.syncContextGauge()

	if dec.Done {
		out <- ui.StreamEvent{Type: ui.StreamInfo, Text: fmt.Sprintf("Goal achieved: %s", dec.Reason)}
		return false
	}

	r.goalMu.RLock()
	status := g.Status
	r.goalMu.RUnlock()
	if status != goal.StatusActive {
		out <- ui.StreamEvent{Type: ui.StreamInfo, Text: fmt.Sprintf("Goal paused/blocked: %s", dec.Reason)}
		return false
	}

	contPrompt := fmt.Sprintf("[Goal continuation] The objective is not yet satisfied.\n\nObjective: %s\nEvaluator: %s\n\nContinue working toward the objective. Do not ask the user for input.", g.Objective, dec.Reason)
	r.appendUserMessage(contPrompt, false)
	return true
}

func (r *Runner) recordGoalDecision(dec goal.Decision) {
	r.goalMu.Lock()
	defer r.goalMu.Unlock()
	if r.activeGoal == nil {
		return
	}
	r.activeGoal.LastReason = dec.Reason
	if dec.Done {
		r.activeGoal.Status = goal.StatusComplete
	}
}

func (r *Runner) appendUserMessage(text string, isRealUser bool) {
	r.historyMu.Lock()
	defer r.historyMu.Unlock()
	msg := provider.Message{
		Role:  provider.RoleUser,
		Parts: []provider.Part{{Text: text}},
	}
	r.history = append(r.history, msg)
}

// GoalTranscript returns a summary of the transcript for the evaluator.
func (r *Runner) GoalTranscript() string {
	r.historyMu.RLock()
	defer r.historyMu.RUnlock()

	var out string
	// We might only want the last N turns.
	start := 0
	if len(r.history) > 20 {
		start = len(r.history) - 20
	}
	for i := start; i < len(r.history); i++ {
		msg := r.history[i]
		if msg.Role == provider.RoleUser {
			out += "User:\n"
		} else {
			out += "Assistant:\n"
		}
		for _, p := range msg.Parts {
			if p.Text != "" {
				out += p.Text + "\n"
			}
			if p.FunctionCall != nil {
				out += fmt.Sprintf("Tool Call: %s\n", p.FunctionCall.Name)
			}
			if p.FunctionResponse != nil {
				out += fmt.Sprintf("Tool Result: %s\n", p.FunctionResponse.Name)
			}
		}
		out += "\n"
	}
	return out
}

func (r *Runner) Goal() *goal.Goal {
	r.goalMu.RLock()
	defer r.goalMu.RUnlock()
	return r.activeGoal
}

func (r *Runner) SetGoal(g *goal.Goal) {
	r.goalMu.Lock()
	defer r.goalMu.Unlock()
	r.activeGoal = g
	// Note: in a real implementation we would write to session JSONL here.
	if r.sessionRecorder != nil {
		if g == nil {
			_ = r.sessionRecorder.SetGoal(nil)
		} else {
			_ = r.sessionRecorder.SetGoal(g.ToSnapshot())
		}
	}
}

// TotalSessionTokens returns the cumulative token usage for the session.
func (r *Runner) TotalSessionTokens() int {
	r.metrics.mu.Lock()
	defer r.metrics.mu.Unlock()
	return r.metrics.inputTokens + r.metrics.outputTokens
}
