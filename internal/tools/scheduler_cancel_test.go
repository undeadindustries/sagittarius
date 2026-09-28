package tools

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

// blockingTaskStub is a subagent tool that blocks until its context ends, so a
// test can cancel one sibling mid-batch.
type blockingTaskStub struct {
	started chan struct{}
	cause   error
}

func (t *blockingTaskStub) Name() string               { return TaskToolName }
func (t *blockingTaskStub) Description() string        { return "blocking probe" }
func (t *blockingTaskStub) RequiresConfirmation() bool { return false }
func (t *blockingTaskStub) Declaration() provider.ToolDeclaration {
	return provider.ToolDeclaration{Name: TaskToolName}
}
func (t *blockingTaskStub) Execute(ctx context.Context, _ map[string]any) (map[string]any, error) {
	close(t.started)
	<-ctx.Done()
	t.cause = context.Cause(ctx)
	return nil, ctx.Err()
}

// TestPerChildCancelStopsOnlyThatChild: canceling one sibling's call ID ends
// that child with ErrSubagentCanceledByUser while the other completes, and the
// batch itself succeeds.
func TestPerChildCancelStopsOnlyThatChild(t *testing.T) {
	t.Parallel()

	blocking := &blockingTaskStub{started: make(chan struct{})}
	quick := &sleepTaskStub{delay: 50 * time.Millisecond}
	// IsSubagentTool keys on the tool name, so both siblings share one
	// registered tool; the dispatching stub routes by an arg.
	dispatch := &dispatchTaskStub{blocking: blocking, quick: quick}
	registry := &Registry{
		byName:  map[string]Tool{TaskToolName: dispatch},
		aliases: map[string]string{},
	}

	var mu sync.Mutex
	cancels := map[string]context.CancelCauseFunc{}
	s := NewScheduler(registry, Policy{}, false, nil, nil,
		WithSubagentCancelRegistry(func(callID string, cancel context.CancelCauseFunc) func() {
			mu.Lock()
			cancels[callID] = cancel
			mu.Unlock()
			return func() {
				mu.Lock()
				delete(cancels, callID)
				mu.Unlock()
			}
		}))

	calls := []provider.ToolCall{
		{ID: "a", Name: TaskToolName, Args: map[string]any{"which": "blocking"}},
		{ID: "b", Name: TaskToolName, Args: map[string]any{"which": "quick"}},
	}
	type outcome struct {
		responses []provider.FunctionResponse
		err       error
	}
	done := make(chan outcome, 1)
	go func() {
		responses, err := s.Execute(context.Background(), calls, func(ui.StreamEvent) {})
		done <- outcome{responses, err}
	}()

	<-blocking.started
	mu.Lock()
	cancelA := cancels["a"]
	mu.Unlock()
	if cancelA == nil {
		t.Fatal("call a was never registered")
	}
	cancelA(ErrSubagentCanceledByUser)

	got := <-done
	if got.err != nil {
		t.Fatalf("Execute: %v (a sibling cancel must not fail the batch)", got.err)
	}
	if !errors.Is(blocking.cause, ErrSubagentCanceledByUser) {
		t.Errorf("blocking child's cause = %v, want ErrSubagentCanceledByUser", blocking.cause)
	}
	var aResp, bResp *provider.FunctionResponse
	for i := range got.responses {
		switch got.responses[i].CallID {
		case "a":
			aResp = &got.responses[i]
		case "b":
			bResp = &got.responses[i]
		}
	}
	if aResp == nil || bResp == nil {
		t.Fatalf("responses = %+v, want slots for a and b", got.responses)
	}
	if aResp.Response["error"] == nil {
		t.Error("canceled child a must carry an error in its slot")
	}
	if bResp.Response["error"] != nil {
		t.Errorf("sibling b must be unaffected, got %v", bResp.Response)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(cancels) != 0 {
		t.Errorf("registry still holds %d entries after the batch", len(cancels))
	}
}

// dispatchTaskStub routes by the "which" arg so two siblings can behave
// differently under one tool name.
type dispatchTaskStub struct {
	blocking *blockingTaskStub
	quick    *sleepTaskStub
}

func (d *dispatchTaskStub) Name() string               { return TaskToolName }
func (d *dispatchTaskStub) Description() string        { return "dispatch" }
func (d *dispatchTaskStub) RequiresConfirmation() bool { return false }
func (d *dispatchTaskStub) Declaration() provider.ToolDeclaration {
	return provider.ToolDeclaration{Name: TaskToolName}
}
func (d *dispatchTaskStub) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	if args["which"] == "blocking" {
		return d.blocking.Execute(ctx, args)
	}
	return d.quick.Execute(ctx, args)
}

// TestPerChildCancelParentStillPropagates: canceling the parent context (turn
// cancel) still reaches the child as plain context.Canceled — never the
// per-child sentinel — and the slot carries the error. (Execute converts tool
// errors into error slots rather than failing the batch; the turn ends when
// the next round's request fails on the canceled context.)
func TestPerChildCancelParentStillPropagates(t *testing.T) {
	t.Parallel()

	blocking := &blockingTaskStub{started: make(chan struct{})}
	dispatch := &dispatchTaskStub{blocking: blocking, quick: &sleepTaskStub{}}
	registry := &Registry{
		byName:  map[string]Tool{TaskToolName: dispatch},
		aliases: map[string]string{},
	}
	s := NewScheduler(registry, Policy{}, false, nil, nil,
		WithSubagentCancelRegistry(func(string, context.CancelCauseFunc) func() { return func() {} }))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan []provider.FunctionResponse, 1)
	go func() {
		responses, _ := s.Execute(ctx, []provider.ToolCall{
			{ID: "a", Name: TaskToolName, Args: map[string]any{"which": "blocking"}},
		}, func(ui.StreamEvent) {})
		done <- responses
	}()
	<-blocking.started
	cancel()
	responses := <-done
	if !errors.Is(blocking.cause, context.Canceled) {
		t.Errorf("cause = %v, want plain context.Canceled for a turn cancel", blocking.cause)
	}
	if errors.Is(blocking.cause, ErrSubagentCanceledByUser) {
		t.Error("a turn cancel must not masquerade as a user cancel of one child")
	}
	if len(responses) != 1 || responses[0].Response["error"] == nil {
		t.Errorf("responses = %+v, want one error slot", responses)
	}
}
