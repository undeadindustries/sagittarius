package agent

import (
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
)

func attemptsRunner(t *testing.T, maxAttempts *int) *Runner {
	t.Helper()
	settings := openAISettingsWithModelPins(nil)
	if maxAttempts != nil {
		if settings.Sagittarius.Subagents == nil {
			settings.Sagittarius.Subagents = &config.SagittariusSubagents{}
		}
		settings.Sagittarius.Subagents.MaxAttempts = maxAttempts
	}
	h := newSubagentHarness(t, settings)
	return h.parent
}

func TestClaimSubagentAttemptCountsUpThenDenies(t *testing.T) {
	t.Parallel()

	r := attemptsRunner(t, nil)
	for want := 1; want <= config.DefaultSubagentMaxAttempts; want++ {
		got, max, err := r.claimSubagentAttempt(config.SubagentCoding, "Add retry", []string{"internal/http/**"})
		if err != nil {
			t.Fatalf("attempt %d: %v", want, err)
		}
		if got != want || max != config.DefaultSubagentMaxAttempts {
			t.Fatalf("attempt = %d/%d, want %d/%d", got, max, want, config.DefaultSubagentMaxAttempts)
		}
	}
	if _, _, err := r.claimSubagentAttempt(config.SubagentCoding, "Add retry", []string{"internal/http/**"}); err == nil {
		t.Fatal("expected denial past the cap, got nil")
	} else if !strings.Contains(err.Error(), "finish it yourself") {
		t.Errorf("denial does not tell the parent to take over: %v", err)
	}
}

func TestClaimSubagentAttemptBucketsDiffer(t *testing.T) {
	t.Parallel()

	r := attemptsRunner(t, nil)
	if _, _, err := r.claimSubagentAttempt(config.SubagentCoding, "Add retry", []string{"a/**"}); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	// A different lease is a different task.
	if _, _, err := r.claimSubagentAttempt(config.SubagentCoding, "Add retry", []string{"b/**"}); err != nil {
		t.Fatalf("different lease: %v", err)
	}
	// A different class is a different task.
	if _, _, err := r.claimSubagentAttempt(config.SubagentResearch, "Add retry", nil); err != nil {
		t.Fatalf("different class: %v", err)
	}
	// Case and whitespace do not open a fresh bucket.
	if _, _, err := r.claimSubagentAttempt(config.SubagentCoding, "  ADD retry ", []string{"a/**"}); err != nil {
		t.Fatalf("reworded claim: %v", err)
	}
	if _, _, err := r.claimSubagentAttempt(config.SubagentCoding, "Add retry", []string{"a/**"}); err == nil {
		t.Fatal("expected denial on the third identical claim")
	}
}

func TestClaimSubagentAttemptUnlimited(t *testing.T) {
	t.Parallel()

	zero := 0
	r := attemptsRunner(t, &zero)
	for i := 0; i < 5; i++ {
		if _, max, err := r.claimSubagentAttempt(config.SubagentCoding, "x", nil); err != nil || max != 0 {
			t.Fatalf("unlimited claim %d: attempt/max/err", i)
		}
	}
}

func TestClaimSubagentAttemptResetsOnClear(t *testing.T) {
	t.Parallel()

	one := 1
	r := attemptsRunner(t, &one)
	if _, _, err := r.claimSubagentAttempt(config.SubagentCoding, "x", nil); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if _, _, err := r.claimSubagentAttempt(config.SubagentCoding, "x", nil); err == nil {
		t.Fatal("expected denial at cap 1")
	}
	r.ClearHistory()
	if _, _, err := r.claimSubagentAttempt(config.SubagentCoding, "x", nil); err != nil {
		t.Fatalf("claim after /clear: %v", err)
	}
	r.RotateSession()
	// Rotation also resets; with cap 1 the pre-rotation claim must not linger.
	r.ClearHistory()
	if _, _, err := r.claimSubagentAttempt(config.SubagentCoding, "x", nil); err != nil {
		t.Fatalf("claim after rotation: %v", err)
	}
}

func TestSubagentMaxAttemptsResolution(t *testing.T) {
	t.Parallel()

	three := 3
	s := &config.Settings{Sagittarius: &config.SagittariusSettings{
		Subagents: &config.SagittariusSubagents{MaxAttempts: &three},
	}}
	if got := config.ResolveSubagentMaxAttempts(s.Sagittarius, config.DefaultSubagentMaxAttempts); got != 3 {
		t.Fatalf("maxAttempts = %d, want 3", got)
	}
	if got := config.ResolveSubagentMaxAttempts(nil, config.DefaultSubagentMaxAttempts); got != config.DefaultSubagentMaxAttempts {
		t.Fatalf("nil = %d, want default", got)
	}
	neg := -1
	s.Sagittarius.Subagents.MaxAttempts = &neg
	if got := config.ResolveSubagentMaxAttempts(s.Sagittarius, config.DefaultSubagentMaxAttempts); got != config.DefaultSubagentMaxAttempts {
		t.Fatalf("negative = %d, want default", got)
	}
}
