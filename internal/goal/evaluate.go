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

// DefaultEvaluatorTimeoutSeconds caps a tool-using evaluator turn. 30s was
// enough for the old single-shot JSON call; a multi-round read-only loop
// needs more headroom.
const DefaultEvaluatorTimeoutSeconds = 120

// EvaluatorMaxToolRounds is the hard cap on tool rounds for the judge so it
// cannot grind the working tree looking for faults.
const EvaluatorMaxToolRounds = 6

// EvaluatorPrompt is the system instruction for the /goal judge. It is a
// different stance from the worker persona: verify claims against the
// working tree, prefer mechanical checks, and do not do the work.
const EvaluatorPrompt = `You are the Goal Evaluator. You did not do this work. Decide whether the stated
objective has actually been met, not whether the worker believes it has.

You have read-only tools. Use them. The transcript is the worker's account of
what it did: a claim, not evidence. Verify the claims that matter against the
current state of the files.

Prefer mechanical verification over judgement. If a claim can be checked by
running the project's checks, reading the file, or searching for the symbol,
check it rather than reasoning about whether it sounds plausible. Read the
working tree as it is now; do not rely on git history or commits, which may be
stale or absent.

When the objective is not mechanically verifiable (wording, documentation,
naming, design intent), say so in your reason and judge on the evidence you can
gather. Do not withhold completion solely because no test covers it.

Do not do the work. Do not propose a better implementation. Decide only whether
the objective is met, and if not, name the specific gap.

Respond with strict JSON only: {"done": bool, "reason": "short explanation"}`

// DecisionJSONNudge is the one-shot retry prompt when the judge's reply is
// not valid JSON.
const DecisionJSONNudge = `Your previous reply was not valid JSON. Respond with only the JSON object {"done": bool, "reason": "short explanation"}.`

// Decision represents the outcome of an evaluator check.
type Decision struct {
	Done   bool   `json:"done"`
	Reason string `json:"reason"`
}

// Evaluate runs the hybrid completion check for the goal.
// It applies deterministic checks and then consults the configured evaluator model.
// Production /goal evaluation uses a tool-using sub-runner in internal/agent;
// this single-shot path remains for tests and callers that already have a generator.
func Evaluate(ctx context.Context, g *Goal, gen provider.ContentGenerator, transcript string, workDir string, timeout time.Duration) (Decision, error) {
	slog.Info("goal: evaluating", "objective", g.Objective, "turn", g.TurnCount)

	if timeout <= 0 {
		timeout = DefaultEvaluatorTimeoutSeconds * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	detContext, early, stop, err := PrepareEvaluation(ctx, g, workDir)
	if err != nil {
		return Decision{}, err
	}
	if stop {
		return early, nil
	}

	dec, err := runModelEvaluator(ctx, g.Objective, gen, transcript, detContext)
	if err != nil {
		slog.Error("goal: model evaluator failed", "err", err, "objective", g.Objective)
		return Decision{}, fmt.Errorf("goal: model evaluator: %w", err)
	}
	return dec, nil
}

// PrepareEvaluation checks turn caps and runs deterministic backtick checks.
// stop is true when the model should not be consulted (a cap was hit); early
// then holds the decision to record. The caller owns the timeout on ctx.
func PrepareEvaluation(ctx context.Context, g *Goal, workDir string) (detContext string, early Decision, stop bool, err error) {
	ok, status := withinCaps(g)
	if !ok {
		return "", Decision{Done: false, Reason: fmt.Sprintf("Hit limit: %s", status)}, true, nil
	}

	detContext, err = runDeterministicChecks(ctx, g.Objective, workDir)
	if err != nil {
		slog.Error("goal: deterministic checks failed", "err", err)
		return "", Decision{}, false, fmt.Errorf("goal: deterministic checks: %w", err)
	}
	return detContext, Decision{}, false, nil
}

func withinCaps(g *Goal) (bool, Status) {
	if g.TurnCount >= g.MaxTurns {
		return false, StatusBudgetLimited
	}
	// token budget handled by runner metrics, but turn caps checked here as defense in depth
	return true, StatusActive
}

func runModelEvaluator(ctx context.Context, objective string, gen provider.ContentGenerator, transcript, detContext string) (Decision, error) {
	req := &provider.GenerateRequest{
		SystemInstruction: EvaluatorPrompt,
		Messages: []provider.Message{
			{Role: provider.RoleUser, Parts: []provider.Part{{Text: EvaluatorUserPrompt(objective, transcript, detContext)}}},
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

	return ParseDecision(fullText)
}

// EvaluatorUserPrompt builds the user message the judge sees: the objective,
// the worker's transcript (a claim, not evidence), and any backtick-check output.
func EvaluatorUserPrompt(objective, transcript, detContext string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Objective: %s\n\nRecent Transcript:\n%s\n", objective, transcript)
	if detContext != "" {
		fmt.Fprintf(&b, "\nDeterministic checks ground truth:\n%s\n", detContext)
	}
	return b.String()
}

// ParseDecision extracts a Decision from model output. It accepts a raw JSON
// object or one wrapped in markdown fences / surrounding prose.
func ParseDecision(raw string) (Decision, error) {
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
