package agent

import (
	"context"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/snapshot"
	"github.com/undeadindustries/sagittarius/internal/tools"
)

func TestParseReviewVerdict(t *testing.T) {
	t.Parallel()

	tests := []struct {
		text string
		want string
	}{
		{"looks good\nVERDICT: PASS", reviewVerdictPass},
		{"VERDICT: pass", reviewVerdictPass},
		{"  verdict: approve  ", reviewVerdictPass},
		{"one finding\nVERDICT: FAIL", reviewVerdictFail},
		{"VERDICT: Fail", reviewVerdictFail},
		{"VERDICT: request changes", reviewVerdictFail},
		{"no verdict here", reviewVerdictUnknown},
		{"", reviewVerdictUnknown},
		{"VERDICT: maybe", reviewVerdictUnknown},
		// A verdict word quoted in findings is not a verdict; only the
		// trailing verdict line counts.
		{"I considered writing VERDICT: PASS but found a bug", reviewVerdictUnknown},
		{"VERDICT: PASS\ntrailing chatter", reviewVerdictUnknown},
	}
	for _, tc := range tests {
		if got := parseReviewVerdict(tc.text); got != tc.want {
			t.Errorf("parseReviewVerdict(%q) = %q, want %q", tc.text, got, tc.want)
		}
	}
}

func reviewTestParent(t *testing.T, root string, gen *routedGenerator, snapMgr *snapshot.Manager, reviewerOn bool) *Runner {
	t.Helper()
	on := true
	review := &config.SagittariusSubagentClass{Enabled: &reviewerOn}
	settings := &config.Settings{
		Sagittarius: &config.SagittariusSettings{
			Subagents: &config.SagittariusSubagents{
				Coding:   &config.SagittariusSubagentClass{Enabled: &on},
				Research: &config.SagittariusSubagentClass{Enabled: &on},
				Reviewer: review,
			},
		},
	}
	runner, err := NewRunner(RunnerConfig{
		Generator:    gen,
		Model:        "test-model",
		WorkDir:      root,
		ApprovalMode: ApprovalYolo,
		Interactive:  false,
		Settings:     settings,
		Snapshotter:  snapMgr,
		SubagentGenerator: func(context.Context, *config.Settings) (provider.ContentGenerator, error) {
			return gen, nil
		},
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	return runner
}

func codeTaskArgs(desc, promptText string, paths ...string) map[string]any {
	claims := make([]any, len(paths))
	for i, p := range paths {
		claims[i] = p
	}
	return map[string]any{
		tools.TaskParamDescription:    desc,
		tools.TaskParamPrompt:         promptText,
		tools.CodeTaskParamWritePaths: claims,
	}
}

// TestReviewerPassAttachesVerdict runs code_task end to end with the reviewer
// on: the child writes, the reviewer reads the diff and fails it, and the
// hand-off carries review.verdict plus findings.
func TestReviewerPassAttachesVerdict(t *testing.T) {
	t.Setenv("SAGITTARIUS_HOME", t.TempDir())
	root := t.TempDir()
	seedFile(t, root, "alpha/a.txt", "before")

	snapMgr, err := snapshot.NewManager(root, "review", snapshot.Options{})
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
		"Review this finished change": {
			{{TextDelta: "alpha/a.txt:1 uses tabs, repo uses spaces.\nVERDICT: FAIL", Done: true}},
		},
	})
	parent := reviewTestParent(t, root, gen, snapMgr, true)

	tool := newCodeTaskTool(parent)
	result, err := tool.Execute(context.Background(), codeTaskArgs("alpha work", "TASK-ALPHA", "alpha/**"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	review, ok := result["review"].(map[string]any)
	if !ok {
		t.Fatalf("hand-off has no review: %v", result)
	}
	if review["verdict"] != reviewVerdictFail {
		t.Errorf("verdict = %v, want fail", review["verdict"])
	}
	findings, _ := review["findings"].(string)
	if findings == "" {
		t.Error("review carries no findings")
	}
	if gen.turns("Review this finished change") != 1 {
		t.Error("reviewer child never ran")
	}
}

// TestReviewerSkippedWhenDisabledOrClean covers both off-ramps: the switch off,
// and a child that changed nothing.
func TestReviewerSkippedWhenDisabledOrClean(t *testing.T) {
	t.Setenv("SAGITTARIUS_HOME", t.TempDir())
	root := t.TempDir()
	seedFile(t, root, "alpha/a.txt", "before")

	newSnap := func() *snapshot.Manager {
		mgr, err := snapshot.NewManager(root, t.Name(), snapshot.Options{})
		if err != nil {
			t.Fatalf("snapshot.NewManager: %v", err)
		}
		return mgr
	}

	// Disabled: a writing child gets no review key.
	gen := newRoutedGenerator(map[string][][]provider.StreamResponse{
		"TASK-ALPHA": {
			{
				{ToolCalls: []provider.ToolCall{writeCall("a1", "alpha/a.txt", "after")}},
				{Done: true},
			},
			{{TextDelta: "alpha finished", Done: true}},
		},
	})
	parent := reviewTestParent(t, root, gen, newSnap(), false)
	result, err := newCodeTaskTool(parent).Execute(context.Background(), codeTaskArgs("alpha work", "TASK-ALPHA", "alpha/**"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if _, ok := result["review"]; ok {
		t.Error("disabled reviewer must not attach a review")
	}

	// Enabled but clean: a child that writes nothing gets no review key, and
	// no reviewer child is launched.
	gen2 := newRoutedGenerator(map[string][][]provider.StreamResponse{
		"TASK-BETA": {
			{{TextDelta: "nothing to change", Done: true}},
		},
	})
	parent2 := reviewTestParent(t, root, gen2, newSnap(), true)
	result2, err := newCodeTaskTool(parent2).Execute(context.Background(), codeTaskArgs("beta work", "TASK-BETA", "beta/**"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if _, ok := result2["review"]; ok {
		t.Error("a child that changed nothing must not be reviewed")
	}
	if gen2.turns("Review this finished change") != 0 {
		t.Error("reviewer launched with nothing to review")
	}
}
