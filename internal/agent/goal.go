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
	timeoutSecs := 30
	if settings != nil && settings.Sagittarius != nil && settings.Sagittarius.Goal != nil && settings.Sagittarius.Goal.EvaluatorTimeout != nil {
		timeoutSecs = *settings.Sagittarius.Goal.EvaluatorTimeout
	}

	gen, err := r.generator()
	if err != nil {
		slog.Error("goal evaluation: generator error", "err", err)
		out <- ui.StreamEvent{Type: ui.StreamError, Err: fmt.Errorf("goal: generator: %w", err)}
		return false
	}
	// Use evaluator model if configured
	if settings != nil && settings.Sagittarius != nil && settings.Sagittarius.Goal != nil {
		gCfg := settings.Sagittarius.Goal
		if gCfg.EvaluatorProvider != "" || gCfg.EvaluatorModel != "" {
			// For v1, we use the primary generator for simplicity unless a new generator
			// needs to be instantiated. If we need to instantiate one:
			// TODO: support different generator for evaluator
			slog.Warn("evaluator model override not fully supported yet, using active generator")
		}
	}

	// We need the transcript
	transcript := r.GoalTranscript()

	dec, err := goal.Evaluate(ctx, g, gen, transcript, r.workDir, time.Duration(timeoutSecs)*time.Second)
	if err != nil {
		// Fail closed
		slog.Error("goal evaluation failed", "err", err)
		out <- ui.StreamEvent{Type: ui.StreamError, Err: fmt.Errorf("goal: evaluate: %w", err)}
		return false
	}

	r.goalMu.Lock()
	g.LastReason = dec.Reason
	if dec.Done {
		g.Status = goal.StatusComplete
	}
	r.goalMu.Unlock()

	r.syncContextGauge()

	if dec.Done {
		msg := fmt.Sprintf("Goal achieved: %s", dec.Reason)
		out <- ui.StreamEvent{Type: ui.StreamInfo, Text: msg}
		return false
	}

	if g.Status != goal.StatusActive {
		msg := fmt.Sprintf("Goal paused/blocked: %s", dec.Reason)
		out <- ui.StreamEvent{Type: ui.StreamInfo, Text: msg}
		return false
	}

	// Goal continues. Inject continuation prompt.
	contPrompt := fmt.Sprintf("[Goal continuation] The objective is not yet satisfied.\n\nObjective: %s\nEvaluator: %s\n\nContinue working toward the objective. Do not ask the user for input.", g.Objective, dec.Reason)
	r.appendModelMessage("", nil) // to ensure we have assistant before user if needed? Actually appendUserMessage does it
	// wait, appendModelMessage is already called before evaluateGoalTurn!
	// So we just append a user message.
	r.appendUserMessage(contPrompt, false) // We don't need to expand @mentions for synthetic
	return true
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
		r.sessionRecorder.SetGoal(g.ToSnapshot())
	}
}

// TotalSessionTokens returns the cumulative token usage for the session.
func (r *Runner) TotalSessionTokens() int {
	r.metrics.mu.Lock()
	defer r.metrics.mu.Unlock()
	return r.metrics.inputTokens + r.metrics.outputTokens
}
