package agent

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/snapshot"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

// cancelTestGenerator drives the per-child cancel tests (AD-154). The parent
// delegates one code_task; depending on blockChild, either the child's second
// turn or the reviewer's first turn blocks until its context is canceled.
type cancelTestGenerator struct {
	mu              sync.Mutex
	parentTurns     int
	childTurns      int
	blockChild      bool
	childBlocked    chan struct{}
	reviewerBlocked chan struct{}
}

func newCancelTestGenerator(blockChild bool) *cancelTestGenerator {
	return &cancelTestGenerator{
		blockChild:      blockChild,
		childBlocked:    make(chan struct{}),
		reviewerBlocked: make(chan struct{}),
	}
}

func (g *cancelTestGenerator) GenerateContentStream(ctx context.Context, req *provider.GenerateRequest) (<-chan provider.StreamResponse, error) {
	text := firstRequestUserText(req)
	ch := make(chan provider.StreamResponse)
	g.mu.Lock()
	switch {
	case strings.Contains(text, "Review this finished batch"):
		g.mu.Unlock()
		go func() {
			defer close(ch)
			close(g.reviewerBlocked)
			<-ctx.Done()
		}()
		return ch, nil
	case strings.Contains(text, "TASK-BLOCK"):
		turn := g.childTurns
		g.childTurns++
		g.mu.Unlock()
		go func() {
			defer close(ch)
			if turn == 0 {
				if !sendOrDoneTest(ctx, ch, provider.StreamResponse{ToolCalls: []provider.ToolCall{writeCall("w1", "block/b.txt", "partial")}}) {
					return
				}
				sendOrDoneTest(ctx, ch, provider.StreamResponse{Done: true})
				return
			}
			if g.blockChild {
				close(g.childBlocked)
				<-ctx.Done()
				return
			}
			sendOrDoneTest(ctx, ch, provider.StreamResponse{TextDelta: "child done", Done: true})
		}()
		return ch, nil
	default: // PARENT
		turn := g.parentTurns
		g.parentTurns++
		g.mu.Unlock()
		go func() {
			defer close(ch)
			if turn == 0 {
				if !sendOrDoneTest(ctx, ch, provider.StreamResponse{ToolCalls: []provider.ToolCall{
					codeTaskCall("c1", "block work", "TASK-BLOCK", "block/**"),
				}}) {
					return
				}
				sendOrDoneTest(ctx, ch, provider.StreamResponse{Done: true})
				return
			}
			sendOrDoneTest(ctx, ch, provider.StreamResponse{TextDelta: "noted", Done: true})
		}()
		return ch, nil
	}
}

func sendOrDoneTest(ctx context.Context, ch chan<- provider.StreamResponse, resp provider.StreamResponse) bool {
	select {
	case <-ctx.Done():
		return false
	case ch <- resp:
		return true
	}
}

func cancelTestParent(t *testing.T, root string, gen *cancelTestGenerator, snapMgr *snapshot.Manager, reviewerOn bool) *Runner {
	t.Helper()
	on := true
	settings := &config.Settings{
		Sagittarius: &config.SagittariusSettings{
			Subagents: &config.SagittariusSubagents{
				Coding:   &config.SagittariusSubagentClass{Enabled: &on},
				Reviewer: &config.SagittariusSubagentClass{Enabled: &reviewerOn},
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

// drainAsync drains a turn's events in the background so the test can cancel
// a child mid-run.
func drainAsync(events <-chan ui.StreamEvent) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range events {
		}
	}()
	return done
}

func waitClosed(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

// TestCodeTaskUserCancelKeepsPartialWork: a child canceled from the UI returns
// a canceled hand-off that keeps the write it already made — not an error that
// would drop files_changed.
func TestCodeTaskUserCancelKeepsPartialWork(t *testing.T) {
	t.Setenv("SAGITTARIUS_HOME", t.TempDir())
	root := t.TempDir()
	seedFile(t, root, "block/b.txt", "before")

	snapMgr, err := snapshot.NewManager(root, "cancel", snapshot.Options{})
	if err != nil {
		t.Fatalf("snapshot.NewManager: %v", err)
	}
	gen := newCancelTestGenerator(true)
	parent := cancelTestParent(t, root, gen, snapMgr, false)

	done := drainAsync(mustRunTurn(t, parent, "PARENT"))
	waitClosed(t, gen.childBlocked, "the child to reach its blocking turn")

	if !parent.CancelSubagent("c1") {
		t.Fatal("CancelSubagent(c1) = false while the child is running")
	}
	waitClosed(t, done, "the turn to finish")

	resp := codeTaskResponses(t, parent)[0]
	if resp["status"] != subagentStatusCanceled {
		t.Errorf("status = %v, want canceled", resp["status"])
	}
	if _, failed := resp["error"]; failed {
		t.Errorf("a user cancel is not an error: %v", resp["error"])
	}
	files := stringList(resp["files_changed"])
	if len(files) != 1 || files[0] != "block/b.txt" {
		t.Errorf("files_changed = %v, want the write that landed", files)
	}
	if next, _ := resp["next_step"].(string); !strings.Contains(next, "canceled") {
		t.Errorf("next_step = %q, want the do-not-retry instruction", next)
	}
	assertFile(t, root, "block/b.txt", "partial")

	// The settled child's entry is gone: a second cancel finds nothing.
	if parent.CancelSubagent("c1") {
		t.Error("CancelSubagent after settle = true, want false")
	}
}

// TestBatchReviewReviewerCancel: canceling the review card ends the review as
// verdict error, and the batch hand-off is otherwise intact.
func TestBatchReviewReviewerCancel(t *testing.T) {
	t.Setenv("SAGITTARIUS_HOME", t.TempDir())
	root := t.TempDir()
	seedFile(t, root, "block/b.txt", "before")

	snapMgr, err := snapshot.NewManager(root, "cancel-review", snapshot.Options{})
	if err != nil {
		t.Fatalf("snapshot.NewManager: %v", err)
	}
	gen := newCancelTestGenerator(false)
	parent := cancelTestParent(t, root, gen, snapMgr, true)

	done := drainAsync(mustRunTurn(t, parent, "PARENT"))
	waitClosed(t, gen.reviewerBlocked, "the reviewer to start")

	if !parent.CancelSubagent("c1#review-1") {
		t.Fatal("CancelSubagent(c1#review-1) = false while the reviewer is running")
	}
	waitClosed(t, done, "the turn to finish")

	review, ok := codeTaskResponses(t, parent)[0]["batch_review"].(map[string]any)
	if !ok {
		t.Fatal("hand-off has no batch_review block")
	}
	if review["verdict"] != reviewVerdictError {
		t.Errorf("verdict = %v, want error", review["verdict"])
	}
	if f, _ := review["findings"].(string); !strings.Contains(f, "canceled by user") {
		t.Errorf("findings = %q, want the cancel named", f)
	}
	if review["status"] == "needs_changes" {
		t.Error("a canceled review is not needs_changes — there is nothing to fix")
	}
}
