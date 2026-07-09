package slash_test

import (
	"context"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/grill"
	"github.com/undeadindustries/sagittarius/internal/modes"
	"github.com/undeadindustries/sagittarius/internal/slash"
)

func TestGrillCommandTree(t *testing.T) {
	t.Parallel()
	r := slash.NewRegistry()
	if cmd := r.Lookup([]string{"grill"}); cmd == nil {
		t.Fatal("expected grill command")
	}
	for _, sub := range []string{"start", "status", "pause", "resume", "done", "finish", "clear", "stop"} {
		if cmd := r.Lookup([]string{"grill", sub}); cmd == nil {
			t.Fatalf("expected grill %s command", sub)
		}
	}
}

func TestGrillStartRequiresTopic(t *testing.T) {
	t.Parallel()
	deps, _, _ := testDeps(t, nil)
	p := slash.NewProcessor()

	result := p.Process(context.Background(), "/grill start", deps)
	if result.Err == nil {
		t.Fatal("expected error when starting a grill session without a topic")
	}
}

func TestGrillStartBlockedInPlanMode(t *testing.T) {
	t.Parallel()
	deps, _, hooks := testDeps(t, nil)
	hooks.interactionMode = modes.ModePlan
	p := slash.NewProcessor()

	result := p.Process(context.Background(), "/grill widget pricing", deps)
	if result.Err == nil {
		t.Fatal("expected error starting a grill session in plan mode")
	}
	if hooks.grillSession != nil {
		t.Fatal("grill session should not have been seeded")
	}
}

func TestGrillStartSubmitsOpeningPrompt(t *testing.T) {
	t.Parallel()
	deps, _, hooks := testDeps(t, nil)
	p := slash.NewProcessor()

	result := p.Process(context.Background(), "/grill widget pricing", deps)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if !result.Handled {
		t.Fatal("expected handled")
	}
	if result.SubmitPrompt == "" {
		t.Fatal("expected an opening SubmitPrompt to kick off interrogation")
	}
	if !strings.Contains(result.SubmitPrompt, "widget pricing") {
		t.Errorf("SubmitPrompt = %q, want it to mention the topic", result.SubmitPrompt)
	}
	if hooks.grillSession == nil || hooks.grillSession.Topic != "widget pricing" {
		t.Fatalf("grillSession = %+v, want topic 'widget pricing'", hooks.grillSession)
	}
}

func TestGrillDoubleStartRejected(t *testing.T) {
	t.Parallel()
	deps, _, _ := testDeps(t, nil)
	p := slash.NewProcessor()

	if r := p.Process(context.Background(), "/grill widget pricing", deps); r.Err != nil {
		t.Fatalf("first start: %v", r.Err)
	}
	result := p.Process(context.Background(), "/grill another topic", deps)
	if result.Err == nil {
		t.Fatal("expected error starting a second grill session while one is active")
	}
}

func TestGrillStatusReportsNoActiveSession(t *testing.T) {
	t.Parallel()
	deps, _, _ := testDeps(t, nil)
	p := slash.NewProcessor()

	result := p.Process(context.Background(), "/grill status", deps)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if !strings.Contains(strings.Join(result.Messages, "\n"), "No active grill session") {
		t.Fatalf("messages = %v, want 'No active grill session'", result.Messages)
	}
}

func TestGrillPauseResumeCycle(t *testing.T) {
	t.Parallel()
	deps, _, hooks := testDeps(t, nil)
	p := slash.NewProcessor()

	if r := p.Process(context.Background(), "/grill widget pricing", deps); r.Err != nil {
		t.Fatalf("start: %v", r.Err)
	}
	if r := p.Process(context.Background(), "/grill pause taking a break", deps); r.Err != nil {
		t.Fatalf("pause: %v", r.Err)
	}
	if hooks.grillSession.Status != grill.StatusPaused {
		t.Fatalf("status after pause = %q, want paused", hooks.grillSession.Status)
	}
	if r := p.Process(context.Background(), "/grill resume", deps); r.Err != nil {
		t.Fatalf("resume: %v", r.Err)
	}
	if hooks.grillSession.Status != grill.StatusActive {
		t.Fatalf("status after resume = %q, want active", hooks.grillSession.Status)
	}
}

func TestGrillPauseWithoutActiveSessionErrors(t *testing.T) {
	t.Parallel()
	deps, _, _ := testDeps(t, nil)
	p := slash.NewProcessor()

	result := p.Process(context.Background(), "/grill pause", deps)
	if result.Err == nil {
		t.Fatal("expected error pausing with no active grill session")
	}
}

func TestGrillDoneGeneratesSpecAndCompletesAfter(t *testing.T) {
	t.Parallel()
	deps, _, hooks := testDeps(t, nil)
	p := slash.NewProcessor()

	if r := p.Process(context.Background(), "/grill widget pricing", deps); r.Err != nil {
		t.Fatalf("start: %v", r.Err)
	}
	result := p.Process(context.Background(), "/grill done", deps)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if result.SubmitPrompt == "" {
		t.Fatal("expected a spec-generation SubmitPrompt")
	}
	if !result.CompleteGrillAfter {
		t.Fatal("expected CompleteGrillAfter to be set so the session completes once the spec turn finishes")
	}
	if hooks.grillSession.Status != grill.StatusSummarizing {
		t.Fatalf("status after done = %q, want summarizing", hooks.grillSession.Status)
	}
}

func TestGrillFinishIsAliasForDone(t *testing.T) {
	t.Parallel()
	deps, _, _ := testDeps(t, nil)
	p := slash.NewProcessor()

	if r := p.Process(context.Background(), "/grill widget pricing", deps); r.Err != nil {
		t.Fatalf("start: %v", r.Err)
	}
	result := p.Process(context.Background(), "/grill finish", deps)
	if result.Err != nil || !result.CompleteGrillAfter {
		t.Fatalf("result = %+v, want CompleteGrillAfter", result)
	}
}

func TestGrillClearDropsSession(t *testing.T) {
	t.Parallel()
	deps, _, hooks := testDeps(t, nil)
	p := slash.NewProcessor()

	if r := p.Process(context.Background(), "/grill widget pricing", deps); r.Err != nil {
		t.Fatalf("start: %v", r.Err)
	}
	if r := p.Process(context.Background(), "/grill clear", deps); r.Err != nil {
		t.Fatalf("clear: %v", r.Err)
	}
	if hooks.grillSession != nil {
		t.Fatal("expected grill session to be cleared")
	}
}

func TestGrillStopIsAliasForClear(t *testing.T) {
	t.Parallel()
	deps, _, hooks := testDeps(t, nil)
	p := slash.NewProcessor()

	if r := p.Process(context.Background(), "/grill widget pricing", deps); r.Err != nil {
		t.Fatalf("start: %v", r.Err)
	}
	if r := p.Process(context.Background(), "/grill stop", deps); r.Err != nil {
		t.Fatalf("stop: %v", r.Err)
	}
	if hooks.grillSession != nil {
		t.Fatal("expected grill session to be cleared by /grill stop")
	}
}

func TestGrillBareCommandRoutesToStatusOrStart(t *testing.T) {
	t.Parallel()
	deps, _, hooks := testDeps(t, nil)
	p := slash.NewProcessor()

	// No args -> status (no active session).
	status := p.Process(context.Background(), "/grill", deps)
	if status.Err != nil {
		t.Fatalf("unexpected error: %v", status.Err)
	}
	if !strings.Contains(strings.Join(status.Messages, "\n"), "No active grill session") {
		t.Fatalf("messages = %v, want status output", status.Messages)
	}

	// Args -> start.
	start := p.Process(context.Background(), "/grill onboarding flow", deps)
	if start.Err != nil {
		t.Fatalf("unexpected error: %v", start.Err)
	}
	if hooks.grillSession == nil || hooks.grillSession.Topic != "onboarding flow" {
		t.Fatalf("grillSession = %+v, want topic 'onboarding flow'", hooks.grillSession)
	}
}

func TestGrillHelpListsCommand(t *testing.T) {
	t.Parallel()
	p := slash.NewProcessor()
	help := p.Registry().RenderHelp()
	if !strings.Contains(help, "/grill") {
		t.Errorf("help missing /grill\n%s", help)
	}
}
