package agent

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/contextmgmt"
	"github.com/undeadindustries/sagittarius/internal/goal"
	"github.com/undeadindustries/sagittarius/internal/modes"
	"github.com/undeadindustries/sagittarius/internal/session"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

const (
	sessionKindSubagent  = "subagent"
	sessionKindEvaluator = "evaluator"
	selfJudgeWarningText = "worker is grading its own work"
)

// newEvaluatorRunner builds a read-only ModeAsk child that judges the parent's
// goal against the working tree. It reuses the task-subagent construction
// pattern (separate recorder and context manager, shared Runtime) but pins
// the evaluator prompt and a 6-round tool cap.
func (r *Runner) newEvaluatorRunner(ctx context.Context) (*Runner, error) {
	if r.sessionRecorder != nil {
		switch r.sessionRecorder.Kind() {
		case sessionKindSubagent, sessionKindEvaluator:
			return nil, fmt.Errorf("evaluator cannot run inside a %s session", r.sessionRecorder.Kind())
		}
	}

	gen, err := r.auxGenerator(ctx)
	if err != nil {
		return nil, fmt.Errorf("evaluator generator: %w", err)
	}

	subID := uuid.New().String()
	outputDir, _ := session.ChatsDir(r.workspace.Root())
	ctxMgr := contextmgmt.NewManager(contextmgmt.ManagerConfig{
		Enabled:   true,
		SessionID: subID,
		OutputDir: outputDir,
	})

	var rec *session.Recorder
	if r.sessionRecorder != nil {
		hash := session.ProjectHash(r.workspace.Root())
		chatsDir, cdErr := session.ChatsDir(r.workspace.Root())
		if cdErr == nil {
			rec = session.NewRecorder(chatsDir, subID, hash, sessionKindEvaluator)
		}
	}

	settings := r.settingsSnapshot()
	rounds := goal.EvaluatorMaxToolRounds
	cfg := RunnerConfig{
		Generator:             gen,
		Model:                 r.evaluatorModelName(),
		ModelPinned:           true,
		WorkDir:               r.workspace.Root(),
		ApprovalMode:          ApprovalYolo,
		Interactive:           false,
		ContextManager:        ctxMgr,
		SessionRecorder:       rec,
		Settings:              settings,
		ProjectBoundary:       r.projectBoundary,
		InitialMode:           modes.ModeAsk,
		Runtime:               r.runtime,
		SpillDir:              r.spillDir,
		ScriptToolEnabled:     config.ScriptToolEnabled(settings, nil),
		SystemPromptOverride:  goal.EvaluatorPrompt,
		MaxToolRoundsOverride: &rounds,
		OmitSessionTools:      true,
	}

	sub, err := NewRunner(cfg)
	if err != nil {
		return nil, fmt.Errorf("evaluator runner: %w", err)
	}
	return sub, nil
}

func (r *Runner) runEvaluatorAgent(ctx context.Context, g *goal.Goal, transcript, detContext string) (goal.Decision, string, error) {
	sub, err := r.newEvaluatorRunner(ctx)
	if err != nil {
		return goal.Decision{}, "", err
	}
	defer func() {
		if closeErr := sub.Close(); closeErr != nil {
			slog.Debug("evaluator: close", "err", closeErr)
		}
	}()

	prompt := goal.EvaluatorUserPrompt(g.Objective, transcript, detContext)
	if err := drainEvaluatorTurn(ctx, sub, prompt); err != nil {
		return goal.Decision{}, "", err
	}

	dec, err := goal.ParseDecision(sub.LastAssistantText())
	if err == nil {
		return dec, "", nil
	}

	if nudgeErr := drainEvaluatorTurn(ctx, sub, goal.DecisionJSONNudge); nudgeErr != nil {
		return goal.Decision{Done: false, Reason: "evaluator reply was not valid JSON"},
			fmt.Sprintf("evaluator: retry failed: %v", nudgeErr), nil
	}
	dec, err = goal.ParseDecision(sub.LastAssistantText())
	if err != nil {
		return goal.Decision{Done: false, Reason: "evaluator reply was not valid JSON"},
			fmt.Sprintf("evaluator: could not parse decision: %v", err), nil
	}
	return dec, "", nil
}

func drainEvaluatorTurn(ctx context.Context, sub *Runner, prompt string) error {
	stream, err := sub.RunTurn(ctx, prompt)
	if err != nil {
		return fmt.Errorf("evaluator turn: %w", err)
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-stream:
			if !ok {
				return nil
			}
			if ev.Type == ui.StreamError && ev.Err != nil {
				return ev.Err
			}
		}
	}
}

// GoalEvaluatorLabel names the model that judges /goal completion.
// When no dedicated evaluator is configured it states that the worker is
// grading its own work.
func (r *Runner) GoalEvaluatorLabel() string {
	pair := r.evaluatorPairLabel()
	if r.hasDedicatedEvaluator() {
		return pair
	}
	return pair + " (" + selfJudgeWarningText + ")"
}

func (r *Runner) hasDedicatedEvaluator() bool {
	p, m := auxEvaluatorTarget(r.settingsSnapshot())
	return p != "" || m != ""
}

func (r *Runner) evaluatorModelName() string {
	_, m := auxEvaluatorTarget(r.settingsSnapshot())
	if m != "" {
		return m
	}
	return r.Model()
}

func (r *Runner) evaluatorPairLabel() string {
	p, m := auxEvaluatorTarget(r.settingsSnapshot())
	if p == "" {
		p = r.activeProviderID()
	}
	if m == "" {
		m = r.Model()
	}
	if p == "" {
		return m
	}
	if m == "" {
		return p
	}
	return p + " / " + m
}
