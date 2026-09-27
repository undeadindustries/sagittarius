package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/snapshot"
)

// TestReviewerStartFailureIsNonFatal: the coding child writes, then the
// reviewer cannot be built (its pinned provider's generator fails). The
// hand-off must still succeed, keep files_changed, and carry a review with
// verdict "error" — not abort the whole code_task.
func TestReviewerStartFailureIsNonFatal(t *testing.T) {
	t.Setenv("SAGITTARIUS_HOME", t.TempDir())
	root := t.TempDir()
	seedFile(t, root, "alpha/a.txt", "before")

	snapMgr, err := snapshot.NewManager(root, "review-fail", snapshot.Options{})
	if err != nil {
		t.Fatalf("snapshot.NewManager: %v", err)
	}
	gen := newRoutedGenerator(map[string][][]provider.StreamResponse{
		"TASK-ALPHA": {
			{
				{ToolCalls: []provider.ToolCall{writeCall("a1", "alpha/a.txt", "after")}},
				{Done: true},
			},
			{{TextDelta: "alpha finished", Done: true}},
		},
	})
	on := true
	settings := &config.Settings{
		Sagittarius: &config.SagittariusSettings{
			Subagents: &config.SagittariusSubagents{
				Coding:   &config.SagittariusSubagentClass{Enabled: &on},
				Reviewer: &config.SagittariusSubagentClass{Enabled: &on, Provider: "openrouter", Model: "broken"},
			},
		},
	}
	parent, err := NewRunner(RunnerConfig{
		Generator:    gen,
		Model:        "test-model",
		WorkDir:      root,
		ApprovalMode: ApprovalYolo,
		Settings:     settings,
		Snapshotter:  snapMgr,
		SubagentGenerator: func(_ context.Context, s *config.Settings) (provider.ContentGenerator, error) {
			if s.ActiveProvider() == "openrouter" {
				return nil, errors.New("no credential for pinned provider")
			}
			return gen, nil
		},
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { _ = parent.Close() })

	result, err := newCodeTaskTool(parent).Execute(context.Background(), codeTaskArgs("alpha work", "TASK-ALPHA", "alpha/**"))
	if err != nil {
		t.Fatalf("Execute must not fail when only the reviewer cannot start: %v", err)
	}
	files, _ := result["files_changed"].([]string)
	if len(files) != 1 {
		t.Errorf("files_changed = %v, want the child's write", result["files_changed"])
	}
	review, ok := result["review"].(map[string]any)
	if !ok {
		t.Fatalf("hand-off has no review block: %v", result)
	}
	if review["verdict"] != reviewVerdictError {
		t.Errorf("verdict = %v, want error", review["verdict"])
	}
	if f, _ := review["findings"].(string); !strings.Contains(f, "could not start") {
		t.Errorf("findings = %q, want the start failure", f)
	}
}

// TestAppendRereadNoticeKeepsSummary: the stale-read notice must extend the
// hand-off summary, not replace it — on a failed run the summary is the only
// line explaining what went wrong.
func TestAppendRereadNoticeKeepsSummary(t *testing.T) {
	t.Parallel()

	result := map[string]any{"summary": "subagent failed: boom", "result": "subagent failed: boom"}
	appendRereadNotice(result, "[NOTE: re-read a.go]")
	for _, key := range []string{"summary", "result"} {
		v, _ := result[key].(string)
		if !strings.Contains(v, "subagent failed: boom") || !strings.Contains(v, "[NOTE: re-read a.go]") {
			t.Errorf("%s = %q, want failure message plus notice", key, v)
		}
	}
	if result["stale_warnings"] != "[NOTE: re-read a.go]" {
		t.Errorf("stale_warnings = %v", result["stale_warnings"])
	}

	empty := map[string]any{"summary": ""}
	appendRereadNotice(empty, "[NOTE]")
	if empty["summary"] != "[NOTE]" {
		t.Errorf("empty summary = %q, want just the notice (no leading blank lines)", empty["summary"])
	}

	untouched := map[string]any{"summary": "fine"}
	appendRereadNotice(untouched, "")
	if untouched["summary"] != "fine" || untouched["stale_warnings"] != nil {
		t.Errorf("empty notice must be a no-op, got %v", untouched)
	}
}

// TestFailedLaunchReleasesAttempt: with a budget of 1, a child that cannot be
// built must not consume the attempt. The second call should hit the same
// construction error, not "reached max attempts". Covers both tools.
func TestFailedLaunchReleasesAttempt(t *testing.T) {
	t.Parallel()

	one := 1
	settings := openAISettingsWithModelPins(nil)
	settings.Sagittarius.Subagents = &config.SagittariusSubagents{MaxAttempts: &one}
	h := newSubagentHarness(t, settings)
	h.parent.newSubagentGenerator = func(context.Context, *config.Settings) (provider.ContentGenerator, error) {
		return nil, errors.New("no credential for pinned provider")
	}

	code := newCodeTaskTool(h.parent)
	research := newTaskTool(h.parent)
	for i := 0; i < 2; i++ {
		_, err := code.Execute(context.Background(), codeTaskArgs("fix it", "PROMPT", "a/**"))
		if err == nil || strings.Contains(err.Error(), "max attempts") {
			t.Fatalf("code_task call %d: err = %v, want the construction error, not the budget refusal", i+1, err)
		}
		_, err = research.Execute(context.Background(), map[string]any{
			"description": "look it up",
			"prompt":      "PROMPT",
		})
		if err == nil || strings.Contains(err.Error(), "max attempts") {
			t.Fatalf("task call %d: err = %v, want the construction error, not the budget refusal", i+1, err)
		}
	}
}

func TestReleaseSubagentAttempt(t *testing.T) {
	t.Parallel()

	r := attemptsRunner(t, nil)
	if _, _, err := r.claimSubagentAttempt(config.SubagentCoding, "x", nil); err != nil {
		t.Fatalf("claim 1: %v", err)
	}
	if _, _, err := r.claimSubagentAttempt(config.SubagentCoding, "x", nil); err != nil {
		t.Fatalf("claim 2: %v", err)
	}
	r.releaseSubagentAttempt(config.SubagentCoding, "x", nil)
	if n, _, err := r.claimSubagentAttempt(config.SubagentCoding, "x", nil); err != nil || n != 2 {
		t.Fatalf("after release: attempt=%d err=%v, want attempt 2", n, err)
	}
	// Releasing an unknown key is a no-op.
	r.releaseSubagentAttempt(config.SubagentCoding, "never-claimed", nil)
}
