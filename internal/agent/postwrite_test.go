package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/diagnostics"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

func TestClassifyFindings(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name              string
		report            diagnostics.Report
		wantModelMessages int
		wantUserMessages  int
		wantStyleInUser   bool
	}{
		{
			name:              "empty report yields no feedback",
			report:            diagnostics.Report{},
			wantModelMessages: 0,
			wantUserMessages:  0,
		},
		{
			name: "error findings reach both model and user",
			report: diagnostics.Report{Findings: []diagnostics.Finding{
				{Tool: "vet", Severity: diagnostics.SeverityError, Message: "bad code"},
			}},
			wantModelMessages: 1,
			wantUserMessages:  1,
		},
		{
			name: "warning findings reach both model and user",
			report: diagnostics.Report{Findings: []diagnostics.Finding{
				{Tool: "eslint", Severity: diagnostics.SeverityWarning, Message: "unused var"},
			}},
			wantModelMessages: 1,
			wantUserMessages:  1,
		},
		{
			name: "style findings reach the user only, never the model",
			report: diagnostics.Report{Findings: []diagnostics.Finding{
				{Tool: "gofmt", Severity: diagnostics.SeverityStyle, Message: "needs formatting"},
			}},
			wantModelMessages: 0,
			wantUserMessages:  1,
			wantStyleInUser:   true,
		},
		{
			name: "missing tools are user-visible only",
			report: diagnostics.Report{MissingTools: []diagnostics.MissingTool{
				{Name: "ruff", InstallHint: "pip install ruff"},
			}},
			wantModelMessages: 0,
			wantUserMessages:  1,
		},
		{
			name: "a mixed report only sends error/warning to the model",
			report: diagnostics.Report{Findings: []diagnostics.Finding{
				{Tool: "vet", Severity: diagnostics.SeverityError, Message: "bad code"},
				{Tool: "gofmt", Severity: diagnostics.SeverityStyle, Message: "needs formatting"},
			}},
			wantModelMessages: 1,
			wantUserMessages:  2,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			modelFeedback, userFeedback := classifyFindings(tc.report)
			if len(modelFeedback) != tc.wantModelMessages {
				t.Fatalf("modelFeedback = %v, want %d messages", modelFeedback, tc.wantModelMessages)
			}
			if len(userFeedback) != tc.wantUserMessages {
				t.Fatalf("userFeedback = %v, want %d messages", userFeedback, tc.wantUserMessages)
			}
			if tc.wantStyleInUser {
				found := false
				for _, msg := range userFeedback {
					if strings.Contains(msg, "needs formatting") {
						found = true
					}
				}
				if !found {
					t.Fatalf("userFeedback = %v, want the style finding present", userFeedback)
				}
			}
		})
	}
}

func newPostWriteTestRunner(t *testing.T) *Runner {
	t.Helper()
	root := t.TempDir()
	runner, err := NewRunner(RunnerConfig{
		Model:        "test-model",
		WorkDir:      root,
		ApprovalMode: ApprovalYolo,
		Interactive:  false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	return runner
}

// TestEditLoopNudgeFiresOnceAtThreshold covers the AD-080 repeated-edit loop
// detector: three failing checks against the same path must yield exactly
// one nudge (on the call that crosses the threshold), and a fourth failing
// call for the same path must stay silent.
func TestEditLoopNudgeFiresOnceAtThreshold(t *testing.T) {
	t.Parallel()
	r := newPostWriteTestRunner(t)

	const threshold = 3
	path := "flaky.go"

	var nudges []string
	for i := 0; i < 4; i++ {
		nudge := r.editLoopNudge([]string{path}, threshold)
		nudges = append(nudges, nudge)
	}

	for i, nudge := range nudges[:2] {
		if nudge != "" {
			t.Fatalf("call %d: nudge = %q, want empty (below threshold)", i+1, nudge)
		}
	}
	if nudges[2] == "" {
		t.Fatal("call 3: nudge = \"\", want a nudge (threshold crossed)")
	}
	if !strings.Contains(nudges[2], path) {
		t.Fatalf("call 3 nudge = %q, want it to mention %q", nudges[2], path)
	}
	if nudges[3] != "" {
		t.Fatalf("call 4: nudge = %q, want empty (already nudged once)", nudges[3])
	}
}

// TestEditLoopNudgeDisabledAtZeroThreshold covers threshold<=0 disabling the
// detector entirely.
func TestEditLoopNudgeDisabledAtZeroThreshold(t *testing.T) {
	t.Parallel()
	r := newPostWriteTestRunner(t)

	for i := 0; i < 10; i++ {
		if nudge := r.editLoopNudge([]string{"x.go"}, 0); nudge != "" {
			t.Fatalf("call %d: nudge = %q, want empty (threshold<=0 disables detection)", i+1, nudge)
		}
	}
}

// TestEditLoopNudgeIsPerPath asserts the counters and nudge state are
// tracked independently per path.
func TestEditLoopNudgeIsPerPath(t *testing.T) {
	t.Parallel()
	r := newPostWriteTestRunner(t)

	const threshold = 2
	for i := 0; i < 1; i++ {
		if nudge := r.editLoopNudge([]string{"a.go"}, threshold); nudge != "" {
			t.Fatalf("a.go call %d: nudge = %q, want empty", i+1, nudge)
		}
	}
	// b.go has never failed before, so it should not be affected by a.go's count.
	if nudge := r.editLoopNudge([]string{"b.go"}, threshold); nudge != "" {
		t.Fatalf("b.go call 1: nudge = %q, want empty (independent from a.go)", nudge)
	}
}

// TestResetEditLoopStatsClearsState asserts resetEditLoopStats (called at the
// top of every RunTurn) drops both the failure counts and the nudged-path
// markers so a later turn can re-nudge the same path.
func TestResetEditLoopStatsClearsState(t *testing.T) {
	t.Parallel()
	r := newPostWriteTestRunner(t)

	const threshold = 1
	if nudge := r.editLoopNudge([]string{"a.go"}, threshold); nudge == "" {
		t.Fatal("expected a nudge on the first failing call at threshold 1")
	}
	if nudge := r.editLoopNudge([]string{"a.go"}, threshold); nudge != "" {
		t.Fatalf("nudge = %q, want empty (already nudged this turn)", nudge)
	}

	r.resetEditLoopStats()

	if nudge := r.editLoopNudge([]string{"a.go"}, threshold); nudge == "" {
		t.Fatal("expected a nudge again after resetEditLoopStats, want the per-turn state cleared")
	}
}

// TestRunPostWriteChecksNoFindingsReturnsFalse asserts that a clean check
// (here, simply because the written path's extension has no registry entry)
// injects no model feedback and reports false, so the caller does not spend
// an extra agent-loop round.
func TestRunPostWriteChecksNoFindingsReturnsFalse(t *testing.T) {
	t.Parallel()
	r := newPostWriteTestRunner(t)

	var events []ui.StreamEvent
	emit := func(ev ui.StreamEvent) { events = append(events, ev) }

	historyLenBefore := len(r.History())

	got := r.runPostWriteChecks(context.Background(), emit, []string{"notes.unknownext"})
	if got {
		t.Fatal("runPostWriteChecks = true, want false when no findings are produced")
	}
	if len(r.History()) != historyLenBefore {
		t.Fatalf("history length = %d, want unchanged at %d (no feedback should be appended)", len(r.History()), historyLenBefore)
	}

	sawPassed := false
	for _, ev := range events {
		if strings.Contains(ev.Text, "Checks passed") {
			sawPassed = true
		}
	}
	if !sawPassed {
		t.Fatalf("events = %v, want a \"Checks passed\" info event", events)
	}
}
