package goal

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/undeadindustries/sagittarius/internal/provider"
)

// Decision represents the outcome of an evaluator check.
type Decision struct {
	Done   bool   `json:"done"`
	Reason string `json:"reason"`
}

// Evaluate runs the hybrid completion check for the goal.
// It applies deterministic checks and then consults the configured evaluator model.
func Evaluate(ctx context.Context, g *Goal, gen provider.ContentGenerator, transcript string, workDir string, timeout time.Duration) (Decision, error) {
	slog.Info("goal: evaluating", "objective", g.Objective, "turn", g.TurnCount)

	ok, status := withinCaps(g)
	if !ok {
		// A limit hit is not "done", but it pauses/blocks the loop.
		// Handled above in runner, but we return a decision so it can be recorded.
		return Decision{Done: false, Reason: fmt.Sprintf("Hit limit: %s", status)}, nil
	}

	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	detContext, err := runDeterministicChecks(ctx, g.Objective, workDir)
	if err != nil {
		slog.Error("goal: deterministic checks failed", "err", err)
		return Decision{}, fmt.Errorf("goal: deterministic checks: %w", err)
	}

	dec, err := runModelEvaluator(ctx, g.Objective, gen, transcript, detContext)
	if err != nil {
		slog.Error("goal: model evaluator failed", "err", err, "objective", g.Objective)
		return Decision{}, fmt.Errorf("goal: model evaluator: %w", err)
	}
	return dec, nil
}

func withinCaps(g *Goal) (bool, Status) {
	if g.TurnCount >= g.MaxTurns {
		return false, StatusBudgetLimited
	}
	// token budget handled by runner metrics, but turn caps checked here as defense in depth
	return true, StatusActive
}

func runModelEvaluator(ctx context.Context, objective string, gen provider.ContentGenerator, transcript, detContext string) (Decision, error) {
	sysPrompt := `You are the Goal Evaluator. Your job is to check if the session objective has been met based on the transcript and optional deterministic checks. 
Respond in strict JSON with {"done": bool, "reason": "short explanation"}.`

	prompt := fmt.Sprintf("Objective: %s\n\nRecent Transcript:\n%s\n", objective, transcript)
	if detContext != "" {
		prompt += fmt.Sprintf("\nDeterministic checks ground truth:\n%s\n", detContext)
	}

	req := &provider.GenerateRequest{
		SystemInstruction: sysPrompt,
		Messages: []provider.Message{
			{Role: provider.RoleUser, Parts: []provider.Part{{Text: prompt}}},
		},
	}

	ch, err := gen.GenerateContentStream(ctx, req)
	if err != nil {
		return Decision{}, fmt.Errorf("generate stream: %w", err)
	}

	var fullText string
	for ev := range ch {
		if ev.Error != nil {
			return Decision{}, fmt.Errorf("stream event: %w", ev.Error)
		}
		if ev.TextDelta != "" {
			fullText += ev.TextDelta
		}
	}

	return parseDecision(fullText)
}

func parseDecision(raw string) (Decision, error) {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start >= 0 && end > start {
		raw = raw[start : end+1]
	}

	var d Decision
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return Decision{}, fmt.Errorf("parse JSON: %w (raw: %q)", err, raw)
	}
	return d, nil
}
