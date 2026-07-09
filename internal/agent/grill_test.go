package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/grill"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/tools"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

// TestGrillDirectiveInjectedIntoSystemInstruction asserts that an active grill
// session appends grill.Directive to the system instruction sent to the
// provider, so the model actually adopts the interrogator persona.
func TestGrillDirectiveInjectedIntoSystemInstruction(t *testing.T) {
	t.Parallel()

	gen := &fakeGenerator{batches: [][]provider.StreamResponse{{{Done: true}}}}
	runner, err := NewRunner(RunnerConfig{
		Generator:   gen,
		Model:       "test-model",
		WorkDir:     t.TempDir(),
		Interactive: false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	runner.SetGrill(&grill.Session{Topic: "widget pricing", Status: grill.StatusActive, StartedAt: time.Now()})

	events, err := runner.RunTurn(testContext(t), "hello")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	drainEvents(t, events)

	req := gen.lastRequest()
	if req == nil {
		t.Fatal("expected generate request")
	}
	if !strings.Contains(req.SystemInstruction, `Grill-me mode: interrogating "widget pricing"`) {
		t.Fatalf("system instruction missing grill directive:\n%s", req.SystemInstruction)
	}
}

// TestGrillDirectiveOmittedWhenPausedOrComplete asserts the directive is not
// injected once the session leaves the interrogating state entirely (cleared),
// avoiding stale instructions bleeding into unrelated turns.
func TestGrillDirectiveOmittedAfterClear(t *testing.T) {
	t.Parallel()

	gen := &fakeGenerator{batches: [][]provider.StreamResponse{{{Done: true}}}}
	runner, err := NewRunner(RunnerConfig{
		Generator:   gen,
		Model:       "test-model",
		WorkDir:     t.TempDir(),
		Interactive: false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	runner.SetGrill(&grill.Session{Topic: "widget pricing", Status: grill.StatusActive, StartedAt: time.Now()})
	runner.SetGrill(nil)

	events, err := runner.RunTurn(testContext(t), "hello")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	drainEvents(t, events)

	req := gen.lastRequest()
	if req == nil {
		t.Fatal("expected generate request")
	}
	if strings.Contains(req.SystemInstruction, "Grill-me mode") {
		t.Fatalf("system instruction should not mention grill mode after clear:\n%s", req.SystemInstruction)
	}
}

// TestGrillPausedOmitsDirective asserts a paused session does not inject the
// interrogation directive, so a paused grill does not keep steering the model to
// ask questions (AD-072 bugbot fix: pause must actually suspend interrogation).
func TestGrillPausedOmitsDirective(t *testing.T) {
	t.Parallel()

	gen := &fakeGenerator{batches: [][]provider.StreamResponse{{{Done: true}}}}
	runner, err := NewRunner(RunnerConfig{
		Generator:   gen,
		Model:       "test-model",
		WorkDir:     t.TempDir(),
		Interactive: false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	runner.SetGrill(&grill.Session{Topic: "widget pricing", Status: grill.StatusPaused, StartedAt: time.Now()})

	events, err := runner.RunTurn(testContext(t), "hello")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	drainEvents(t, events)

	req := gen.lastRequest()
	if req == nil {
		t.Fatal("expected generate request")
	}
	if strings.Contains(req.SystemInstruction, "Grill-me mode") {
		t.Fatalf("paused grill should not inject the directive:\n%s", req.SystemInstruction)
	}
}

// TestGrillPausedBlocksAskUser asserts ask_user refuses to run while the session
// is paused, so the model cannot keep interrogating a suspended session.
func TestGrillPausedBlocksAskUser(t *testing.T) {
	t.Parallel()

	runner := newGrillRunner(t)
	runner.SetGrill(&grill.Session{Topic: "widget pricing", Status: grill.StatusPaused, StartedAt: time.Now()})
	tool := &askUserTool{runner: runner}

	args := map[string]any{
		"question": "Anything?",
		"options":  []any{map[string]any{"label": "Yes"}, map[string]any{"label": "No"}},
	}
	if _, err := tool.ExecuteInteractive(context.Background(), args, false, nil); err == nil {
		t.Fatal("expected ask_user to be rejected while paused")
	}
	if g := runner.Grill(); g.QuestionCount != 0 {
		t.Fatalf("paused ask_user should not record a decision; QuestionCount = %d", g.QuestionCount)
	}
}

// TestGrillDirectiveReflectsConfig asserts sagittarius.grill.maxQuestions and
// recommend actually reach the injected directive (AD-072 bugbot fix: the config
// keys were round-tripping but never applied).
func TestGrillDirectiveReflectsConfig(t *testing.T) {
	t.Parallel()

	maxQ := 3
	recommend := false
	settings := &config.Settings{Sagittarius: &config.SagittariusSettings{
		Grill: &config.SagittariusGrillConfig{MaxQuestions: &maxQ, Recommend: &recommend},
	}}

	gen := &fakeGenerator{batches: [][]provider.StreamResponse{{{Done: true}}}}
	runner, err := NewRunner(RunnerConfig{
		Generator:   gen,
		Model:       "test-model",
		WorkDir:     t.TempDir(),
		Interactive: false,
		Settings:    settings,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	runner.SetGrill(&grill.Session{Topic: "widget pricing", Status: grill.StatusActive, StartedAt: time.Now()})

	events, err := runner.RunTurn(testContext(t), "hello")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	drainEvents(t, events)

	req := gen.lastRequest()
	if req == nil {
		t.Fatal("expected generate request")
	}
	if !strings.Contains(req.SystemInstruction, "within about 3 questions") {
		t.Fatalf("directive missing maxQuestions soft cap:\n%s", req.SystemInstruction)
	}
	if !strings.Contains(req.SystemInstruction, "neutrally") {
		t.Fatalf("directive should present options neutrally when recommend=false:\n%s", req.SystemInstruction)
	}
}

// TestEndGrillRejectsRepeatFinish asserts /grill done cannot re-trigger spec
// generation once a session is already summarizing or complete.
func TestEndGrillRejectsRepeatFinish(t *testing.T) {
	t.Parallel()

	runner := newGrillRunner(t)
	runner.SetGrill(&grill.Session{
		Topic:     "widget pricing",
		Status:    grill.StatusActive,
		StartedAt: time.Now(),
		Decisions: []grill.Decision{{Question: "Pricing?", Answer: "Flat-rate"}},
	})
	h := &appHooks{app: &App{runner: runner}}

	if _, err := h.EndGrill(""); err != nil {
		t.Fatalf("first EndGrill: %v", err)
	}
	if g := runner.Grill(); g.Status != grill.StatusSummarizing {
		t.Fatalf("after EndGrill Status = %q, want summarizing", g.Status)
	}
	if _, err := h.EndGrill(""); err == nil {
		t.Fatal("second EndGrill should be rejected while summarizing")
	}

	runner.SetGrill(&grill.Session{
		Topic:     "widget pricing",
		Status:    grill.StatusComplete,
		StartedAt: time.Now(),
		Decisions: []grill.Decision{{Question: "Pricing?", Answer: "Flat-rate"}},
	})
	if _, err := h.EndGrill(""); err == nil {
		t.Fatal("EndGrill should be rejected once complete")
	}
}

// TestGrillConcurrentDecisionAndPause exercises the locked-mutation path under
// the race detector: recordDecision (mid-turn) racing /grill pause (background
// goroutine) must not corrupt the session or lose either write.
func TestGrillConcurrentDecisionAndPause(t *testing.T) {
	t.Parallel()

	runner := newGrillRunner(t)
	tool := &askUserTool{runner: runner}
	h := &appHooks{app: &App{runner: runner}}

	const n = 50
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			tool.recordDecision("q", "a")
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			_ = h.PauseGrill("")
			_ = h.ResumeGrill("")
			_ = runner.Grill()
		}
	}()
	wg.Wait()

	g := runner.Grill()
	if g == nil {
		t.Fatal("session lost after concurrent mutation")
	}
	if g.QuestionCount != n {
		t.Fatalf("QuestionCount = %d, want %d (no lost decision writes)", g.QuestionCount, n)
	}
	if len(g.Decisions) != n {
		t.Fatalf("Decisions = %d, want %d", len(g.Decisions), n)
	}
}

// TestGrillActiveBlocksWriteFile asserts the runner-level read-only gate denies
// write_file while a grill session is active, regardless of approval mode.
func TestGrillActiveBlocksWriteFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	target := filepath.Join(root, "spec-draft.txt")

	gen := &fakeGenerator{
		batches: [][]provider.StreamResponse{
			{
				{ToolCalls: []provider.ToolCall{{
					Name: tools.WriteFileToolName,
					Args: map[string]any{tools.ParamFilePath: "spec-draft.txt", "content": "nope"},
				}}},
				{Done: true},
			},
			{{TextDelta: "ok"}, {Done: true}},
		},
	}
	runner, err := NewRunner(RunnerConfig{
		Generator:    gen,
		Model:        "test-model",
		WorkDir:      root,
		ApprovalMode: ApprovalYolo,
		Interactive:  false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	runner.SetGrill(&grill.Session{Topic: "widget pricing", Status: grill.StatusActive, StartedAt: time.Now()})

	events, err := runner.RunTurn(testContext(t), "write the draft")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	got := collectEvents(t, events)

	var deniedText string
	for _, ev := range got {
		if ev.Type == ui.StreamToolResult && ev.IsError {
			deniedText = ev.Text
		}
	}
	if deniedText == "" || !strings.Contains(deniedText, "grill mode") {
		t.Fatalf("events = %#v, want a grill-mode denial result", got)
	}
	if _, err := os.Stat(target); err == nil {
		t.Fatal("write_file should have been denied while grilling; file was created")
	}
}

// TestGrillSummarizingAllowsWriteFile asserts the read-only gate lifts once the
// session transitions to StatusSummarizing, so the final spec write succeeds.
func TestGrillSummarizingAllowsWriteFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	target := filepath.Join(root, "docs", "specs", "widget-pricing.md")

	gen := &fakeGenerator{
		batches: [][]provider.StreamResponse{
			{
				{ToolCalls: []provider.ToolCall{{
					Name: tools.WriteFileToolName,
					Args: map[string]any{tools.ParamFilePath: "docs/specs/widget-pricing.md", "content": "# Spec"},
				}}},
				{Done: true},
			},
			{{TextDelta: "done"}, {Done: true}},
		},
	}
	runner, err := NewRunner(RunnerConfig{
		Generator:    gen,
		Model:        "test-model",
		WorkDir:      root,
		ApprovalMode: ApprovalYolo,
		Interactive:  false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	runner.SetGrill(&grill.Session{Topic: "widget pricing", Status: grill.StatusSummarizing, StartedAt: time.Now()})

	events, err := runner.RunTurn(testContext(t), "write the spec")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	got := collectEvents(t, events)

	for _, ev := range got {
		if ev.Type == ui.StreamToolResult && ev.IsError {
			t.Fatalf("unexpected denial while summarizing: %s", ev.Text)
		}
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("expected spec file to be written while summarizing: %v", err)
	}
}

// TestGrillInitialSnapshotRestoresReadOnlyGate asserts a resumed session
// (InitialGrill) re-applies the read-only gate immediately, without a
// subsequent SetGrill call.
func TestGrillInitialSnapshotRestoresReadOnlyGate(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	gen := &fakeGenerator{
		batches: [][]provider.StreamResponse{
			{
				{ToolCalls: []provider.ToolCall{{
					Name: tools.WriteFileToolName,
					Args: map[string]any{tools.ParamFilePath: "out.txt", "content": "nope"},
				}}},
				{Done: true},
			},
			{{Done: true}},
		},
	}
	snapshot := (&grill.Session{Topic: "resumed topic", Status: grill.StatusActive, StartedAt: time.Now()}).ToSnapshot()
	runner, err := NewRunner(RunnerConfig{
		Generator:    gen,
		Model:        "test-model",
		WorkDir:      root,
		ApprovalMode: ApprovalYolo,
		Interactive:  false,
		InitialGrill: snapshot,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	if g := runner.Grill(); g == nil || g.Topic != "resumed topic" {
		t.Fatalf("Grill() = %+v, want restored session", g)
	}

	events, err := runner.RunTurn(testContext(t), "write it")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	got := collectEvents(t, events)
	var denied bool
	for _, ev := range got {
		if ev.Type == ui.StreamToolResult && ev.IsError {
			denied = true
		}
	}
	if !denied {
		t.Fatalf("events = %#v, want write_file denied by restored grill session", got)
	}
}
