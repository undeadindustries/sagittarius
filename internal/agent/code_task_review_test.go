package agent

import (
	"context"
	"strings"
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

// batchReviewParent builds a parent runner with coding subagents on and the
// given reviewer class config (nil = the default, which follows coding).
func batchReviewParent(t *testing.T, root string, gen *routedGenerator, snapMgr *snapshot.Manager, reviewer *config.SagittariusSubagentClass) *Runner {
	t.Helper()
	on := true
	settings := &config.Settings{
		Sagittarius: &config.SagittariusSettings{
			Subagents: &config.SagittariusSubagents{
				Coding:   &config.SagittariusSubagentClass{Enabled: &on},
				Research: &config.SagittariusSubagentClass{Enabled: &on},
				Reviewer: reviewer,
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

// codeTaskResponses returns the hand-off maps the parent recorded, in order.
func codeTaskResponses(t *testing.T, r *Runner) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, msg := range r.History() {
		for _, p := range msg.Parts {
			if p.FunctionResponse != nil && p.FunctionResponse.Name == tools.CodeTaskToolName {
				out = append(out, p.FunctionResponse.Response)
			}
		}
	}
	if len(out) == 0 {
		t.Fatal("history holds no code_task responses")
	}
	return out
}

// userMessagesContaining counts user-role history messages carrying marker.
func userMessagesContaining(r *Runner, marker string) int {
	n := 0
	for _, msg := range r.History() {
		if msg.Role != provider.RoleUser {
			continue
		}
		for _, p := range msg.Parts {
			if strings.Contains(p.Text, marker) {
				n++
			}
		}
	}
	return n
}

const batchContract = "Amounts are integer cents. The CLI layer validates input."

// batchScripts is the shared wiring for the review tests: the parent delegates
// alpha and beta with one contract, each child writes inside its lease.
func batchScripts() map[string][][]provider.StreamResponse {
	return map[string][][]provider.StreamResponse{
		"PARENT": {
			{
				{ToolCalls: []provider.ToolCall{
					codeTaskCallWithContract("c1", "alpha work", "TASK-ALPHA", batchContract, "alpha/**"),
					codeTaskCallWithContract("c2", "beta work", "TASK-BETA", batchContract, "beta/**"),
				}},
				{Done: true},
			},
			{{TextDelta: "all done", Done: true}},
		},
		"TASK-ALPHA": {
			{
				{ToolCalls: []provider.ToolCall{writeCall("a1", "alpha/a.txt", "alpha-after")}},
				{Done: true},
			},
			{{TextDelta: "alpha finished", Done: true}},
		},
		"TASK-BETA": {
			{
				{ToolCalls: []provider.ToolCall{writeCall("b1", "beta/b.txt", "beta-after")}},
				{Done: true},
			},
			{{TextDelta: "beta finished", Done: true}},
		},
	}
}

// TestBatchReviewFailFixPass is the AD-153 happy path: the reviewer sees both
// siblings at once, fails the batch on a cross-file seam, the fix child
// repairs it, and the re-review passes. The first hand-off carries the full
// batch_review block; the sibling carries a reference.
func TestBatchReviewFailFixPass(t *testing.T) {
	t.Setenv("SAGITTARIUS_HOME", t.TempDir())
	root := t.TempDir()
	seedFile(t, root, "alpha/a.txt", "alpha-before")
	seedFile(t, root, "beta/b.txt", "beta-before")

	snapMgr, err := snapshot.NewManager(root, "batch-review", snapshot.Options{})
	if err != nil {
		t.Fatalf("snapshot.NewManager: %v", err)
	}
	scripts := batchScripts()
	scripts["Review this finished batch"] = [][]provider.StreamResponse{
		{{TextDelta: "alpha/a.txt:1 and beta/b.txt:1 disagree on the amount type.\nVERDICT: FAIL", Done: true}},
		{{TextDelta: "the seam is consistent now.\nVERDICT: PASS", Done: true}},
	}
	scripts["A review of the batch"] = [][]provider.StreamResponse{
		{
			{ToolCalls: []provider.ToolCall{writeCall("f1", "alpha/a.txt", "alpha-fixed")}},
			{Done: true},
		},
		{{TextDelta: "fixed the mismatch", Done: true}},
	}
	gen := newRoutedGenerator(scripts)
	parent := batchReviewParent(t, root, gen, snapMgr, nil)

	drainToSlice(t, mustRunTurn(t, parent, "PARENT"))

	assertFile(t, root, "alpha/a.txt", "alpha-fixed")
	if got := gen.turns("Review this finished batch"); got != 2 {
		t.Errorf("reviewer ran %d times, want 2 (fail, re-review)", got)
	}
	// The fix child generates twice: once to write, once to report.
	if got := gen.turns("A review of the batch"); got != 2 {
		t.Errorf("fix child generated %d turns, want 2 (write, report)", got)
	}

	responses := codeTaskResponses(t, parent)
	if len(responses) != 2 {
		t.Fatalf("want 2 code_task responses, got %d", len(responses))
	}
	review, ok := responses[0]["batch_review"].(map[string]any)
	if !ok {
		t.Fatalf("first hand-off has no batch_review block: %v", responses[0])
	}
	if review["verdict"] != reviewVerdictPass {
		t.Errorf("verdict = %v, want pass after the fix", review["verdict"])
	}
	if review["fix_rounds"] != 1 {
		t.Errorf("fix_rounds = %v, want 1", review["fix_rounds"])
	}
	fixed := stringList(review["fix_files_changed"])
	if len(fixed) != 1 || fixed[0] != "alpha/a.txt" {
		t.Errorf("fix_files_changed = %v, want [alpha/a.txt]", fixed)
	}
	if review["status"] == "needs_changes" {
		t.Error("a passing re-review must not be marked needs_changes")
	}
	ref, _ := responses[1]["batch_review"].(string)
	if !strings.Contains(ref, "pass") {
		t.Errorf("sibling reference = %q, want a verdict pointer", ref)
	}
	if n := userMessagesContaining(parent, "The batch review still has unresolved findings"); n != 0 {
		t.Errorf("a passing batch must not trigger the turn guard, got %d reminders", n)
	}
}

// TestBatchReviewNeedsChangesTurnGuard: the fix round fails too, so the batch
// is handed back as needs_changes — and a parent that answers with text alone
// gets exactly one harness reminder to fix the findings itself.
func TestBatchReviewNeedsChangesTurnGuard(t *testing.T) {
	t.Setenv("SAGITTARIUS_HOME", t.TempDir())
	root := t.TempDir()
	seedFile(t, root, "alpha/a.txt", "alpha-before")
	seedFile(t, root, "beta/b.txt", "beta-before")

	snapMgr, err := snapshot.NewManager(root, "batch-fail", snapshot.Options{})
	if err != nil {
		t.Fatalf("snapshot.NewManager: %v", err)
	}
	scripts := batchScripts()
	scripts["Review this finished batch"] = [][]provider.StreamResponse{
		{{TextDelta: "the seam is still wrong.\nVERDICT: FAIL", Done: true}},
		{{TextDelta: "still wrong after the fix.\nVERDICT: FAIL", Done: true}},
	}
	scripts["A review of the batch"] = [][]provider.StreamResponse{
		{
			{ToolCalls: []provider.ToolCall{writeCall("f1", "alpha/a.txt", "alpha-fixed")}},
			{Done: true},
		},
		{{TextDelta: "tried", Done: true}},
	}
	gen := newRoutedGenerator(scripts)
	parent := batchReviewParent(t, root, gen, snapMgr, nil)

	drainToSlice(t, mustRunTurn(t, parent, "PARENT"))

	review, ok := codeTaskResponses(t, parent)[0]["batch_review"].(map[string]any)
	if !ok {
		t.Fatal("first hand-off has no batch_review block")
	}
	if review["status"] != "needs_changes" {
		t.Errorf("status = %v, want needs_changes", review["status"])
	}
	if next, _ := review["next_step"].(string); !strings.Contains(next, "yourself") {
		t.Errorf("next_step = %q, want the parent-fix instruction", next)
	}
	if n := userMessagesContaining(parent, "The batch review still has unresolved findings"); n != 1 {
		t.Errorf("reminders in history = %d, want exactly 1", n)
	}
	// The reminder bought the parent one more round: initial delegation, the
	// text-only reply that triggered the guard, and the post-reminder reply.
	if got := gen.turns("PARENT"); got != 3 {
		t.Errorf("parent ran %d turns, want 3 (the reminder adds one)", got)
	}
}

// TestBatchReviewRetriesEmptyReviewer: a reasoning-only reply ends a child
// turn cleanly with no deliverable (the empty-reply guard exempts it), so the
// reviewer gets one nudge retry before the batch accepts "unknown".
func TestBatchReviewRetriesEmptyReviewer(t *testing.T) {
	t.Setenv("SAGITTARIUS_HOME", t.TempDir())
	root := t.TempDir()
	seedFile(t, root, "alpha/a.txt", "alpha-before")
	seedFile(t, root, "beta/b.txt", "beta-before")

	snapMgr, err := snapshot.NewManager(root, "batch-retry", snapshot.Options{})
	if err != nil {
		t.Fatalf("snapshot.NewManager: %v", err)
	}
	scripts := batchScripts()
	scripts["Review this finished batch"] = [][]provider.StreamResponse{
		// Reasoning with no text: a clean turn end that produced nothing.
		{{ReasoningDelta: "let me think about this", Done: true}},
		{{TextDelta: "all consistent.\nVERDICT: PASS", Done: true}},
	}
	gen := newRoutedGenerator(scripts)
	parent := batchReviewParent(t, root, gen, snapMgr, nil)

	drainToSlice(t, mustRunTurn(t, parent, "PARENT"))

	if got := gen.turns("Review this finished batch"); got != 2 {
		t.Errorf("reviewer ran %d times, want 2 (empty, then the nudge retry)", got)
	}
	review, ok := codeTaskResponses(t, parent)[0]["batch_review"].(map[string]any)
	if !ok {
		t.Fatal("first hand-off has no batch_review block")
	}
	if review["verdict"] != reviewVerdictPass {
		t.Errorf("verdict = %v, want pass from the retry", review["verdict"])
	}
}

// TestBatchReviewMaxFixRoundsZero: review-only mode attaches the failing
// verdict without launching a fix child.
func TestBatchReviewMaxFixRoundsZero(t *testing.T) {
	t.Setenv("SAGITTARIUS_HOME", t.TempDir())
	root := t.TempDir()
	seedFile(t, root, "alpha/a.txt", "alpha-before")
	seedFile(t, root, "beta/b.txt", "beta-before")

	snapMgr, err := snapshot.NewManager(root, "batch-zero", snapshot.Options{})
	if err != nil {
		t.Fatalf("snapshot.NewManager: %v", err)
	}
	scripts := batchScripts()
	scripts["Review this finished batch"] = [][]provider.StreamResponse{
		{{TextDelta: "wrong seam.\nVERDICT: FAIL", Done: true}},
	}
	gen := newRoutedGenerator(scripts)
	on := true
	zero := 0
	parent := batchReviewParent(t, root, gen, snapMgr, &config.SagittariusSubagentClass{Enabled: &on, MaxFixRounds: &zero})

	drainToSlice(t, mustRunTurn(t, parent, "PARENT"))

	if got := gen.turns("A review of the batch"); got != 0 {
		t.Errorf("fix child ran %d times with maxFixRounds=0, want 0", got)
	}
	review, ok := codeTaskResponses(t, parent)[0]["batch_review"].(map[string]any)
	if !ok {
		t.Fatal("first hand-off has no batch_review block")
	}
	if review["verdict"] != reviewVerdictFail || review["status"] != "needs_changes" {
		t.Errorf("batch_review = %v, want fail + needs_changes", review)
	}
	if review["fix_rounds"] != 0 {
		t.Errorf("fix_rounds = %v, want 0", review["fix_rounds"])
	}
}

// TestBatchReviewSkippedWhenDisabledOrClean covers both off-ramps: the switch
// off, and a batch that changed nothing.
func TestBatchReviewSkippedWhenDisabledOrClean(t *testing.T) {
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

	// Disabled: a writing child gets no batch_review key and no reviewer runs.
	off := false
	gen := newRoutedGenerator(map[string][][]provider.StreamResponse{
		"PARENT": {
			{
				{ToolCalls: []provider.ToolCall{codeTaskCall("c1", "alpha work", "TASK-ALPHA", "alpha/**")}},
				{Done: true},
			},
			{{TextDelta: "done", Done: true}},
		},
		"TASK-ALPHA": {
			{
				{ToolCalls: []provider.ToolCall{writeCall("a1", "alpha/a.txt", "after")}},
				{Done: true},
			},
			{{TextDelta: "alpha finished", Done: true}},
		},
	})
	parent := batchReviewParent(t, root, gen, newSnap(), &config.SagittariusSubagentClass{Enabled: &off})
	drainToSlice(t, mustRunTurn(t, parent, "PARENT"))
	if _, ok := codeTaskResponses(t, parent)[0]["batch_review"]; ok {
		t.Error("disabled reviewer must not attach a batch review")
	}
	if gen.turns("Review this finished batch") != 0 {
		t.Error("reviewer launched while disabled")
	}

	// Enabled but clean: a child that writes nothing is not reviewed.
	gen2 := newRoutedGenerator(map[string][][]provider.StreamResponse{
		"PARENT": {
			{
				{ToolCalls: []provider.ToolCall{codeTaskCall("c1", "beta work", "TASK-BETA", "beta/**")}},
				{Done: true},
			},
			{{TextDelta: "done", Done: true}},
		},
		"TASK-BETA": {
			{{TextDelta: "nothing to change", Done: true}},
		},
	})
	parent2 := batchReviewParent(t, root, gen2, newSnap(), nil)
	drainToSlice(t, mustRunTurn(t, parent2, "PARENT"))
	if _, ok := codeTaskResponses(t, parent2)[0]["batch_review"]; ok {
		t.Error("a child that changed nothing must not be reviewed")
	}
	if gen2.turns("Review this finished batch") != 0 {
		t.Error("reviewer launched with nothing to review")
	}
}
