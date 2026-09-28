package tools

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

// codeTaskStub poses as the coding subagent tool, recording executions so a
// pre-dispatch denial is observable.
type codeTaskStub struct {
	executed atomic.Int32
}

func (t *codeTaskStub) Name() string               { return CodeTaskToolName }
func (t *codeTaskStub) Description() string        { return "contract probe" }
func (t *codeTaskStub) RequiresConfirmation() bool { return false }
func (t *codeTaskStub) Declaration() provider.ToolDeclaration {
	return provider.ToolDeclaration{Name: CodeTaskToolName}
}
func (t *codeTaskStub) Execute(_ context.Context, _ map[string]any) (map[string]any, error) {
	t.executed.Add(1)
	return map[string]any{"status": "completed", "summary": "done"}, nil
}

func contractScheduler(stub *codeTaskStub, opts ...SchedulerOption) *Scheduler {
	registry := &Registry{
		byName:  map[string]Tool{CodeTaskToolName: stub},
		aliases: map[string]string{},
	}
	return NewScheduler(registry, Policy{}, false, nil, nil, opts...)
}

func contractCall(id, desc, contract string) provider.ToolCall {
	args := map[string]any{TaskParamDescription: desc, TaskParamPrompt: "do " + desc}
	if contract != "" {
		args[CodeTaskParamContract] = contract
	}
	return provider.ToolCall{ID: id, Name: CodeTaskToolName, Args: args}
}

func executeForTest(t *testing.T, s *Scheduler, calls []provider.ToolCall) []provider.FunctionResponse {
	t.Helper()
	responses, err := s.Execute(context.Background(), calls, func(ui.StreamEvent) {})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return responses
}

// Parallel code_task calls without a contract are refused before any child
// launches: siblings cannot see each other's work, so the shared decisions
// have nowhere to live.
func TestContractGateDeniesMissingContract(t *testing.T) {
	t.Parallel()

	stub := &codeTaskStub{}
	responses := executeForTest(t, contractScheduler(stub), []provider.ToolCall{
		contractCall("c1", "alpha", "money is integer cents"),
		contractCall("c2", "beta", ""),
	})
	if stub.executed.Load() != 0 {
		t.Fatal("a child launched despite the missing contract")
	}
	for _, resp := range responses {
		if errText, _ := resp.Response["error"].(string); !strings.Contains(errText, "missing shared design contract") {
			t.Errorf("response = %v, want the missing-contract denial", resp.Response)
		}
	}
}

// Two different contracts is worse than none: each sibling would implement
// against its own private decisions.
func TestContractGateDeniesMismatchedContracts(t *testing.T) {
	t.Parallel()

	stub := &codeTaskStub{}
	responses := executeForTest(t, contractScheduler(stub), []provider.ToolCall{
		contractCall("c1", "alpha", "money is integer cents"),
		contractCall("c2", "beta", "money is float dollars"),
	})
	if stub.executed.Load() != 0 {
		t.Fatal("a child launched despite mismatched contracts")
	}
	for _, resp := range responses {
		if errText, _ := resp.Response["error"].(string); !strings.Contains(errText, "mismatched shared design contracts") {
			t.Errorf("response = %v, want the mismatch denial", resp.Response)
		}
	}
}

// Identical contracts pass, and whitespace-only differences (line wrapping)
// still count as identical.
func TestContractGateAllowsIdenticalContracts(t *testing.T) {
	t.Parallel()

	stub := &codeTaskStub{}
	responses := executeForTest(t, contractScheduler(stub), []provider.ToolCall{
		contractCall("c1", "alpha", "money is integer cents.\nValidate at the CLI."),
		contractCall("c2", "beta", "money is  integer cents. Validate at the CLI."),
	})
	if stub.executed.Load() != 2 {
		t.Fatalf("executed = %d, want both children", stub.executed.Load())
	}
	for _, resp := range responses {
		if resp.Response["error"] != nil {
			t.Errorf("response = %v, want success", resp.Response)
		}
	}
}

// A single code_task is a batch of one: the contract is optional.
func TestContractGateAllowsSingleCallWithoutContract(t *testing.T) {
	t.Parallel()

	stub := &codeTaskStub{}
	responses := executeForTest(t, contractScheduler(stub), []provider.ToolCall{
		contractCall("c1", "alpha", ""),
	})
	if stub.executed.Load() != 1 {
		t.Fatalf("executed = %d, want the single child to run", stub.executed.Load())
	}
	if responses[0].Response["error"] != nil {
		t.Errorf("response = %v, want success", responses[0].Response)
	}
}

// The finalizer receives exactly the code_task slots, after they settle, and
// amends them in place without touching call ids (AD-052 pairing).
func TestBatchFinalizerAmendsResponses(t *testing.T) {
	t.Parallel()

	stub := &codeTaskStub{}
	var gotBatch []SubagentBatchItem
	s := contractScheduler(stub, WithSubagentBatchFinalizer(
		func(_ context.Context, batch []SubagentBatchItem, _ func(ui.StreamEvent)) error {
			gotBatch = batch
			for _, item := range batch {
				item.Response.Response["batch_review"] = map[string]any{"verdict": "pass"}
			}
			return nil
		}))
	responses := executeForTest(t, s, []provider.ToolCall{
		contractCall("c1", "alpha", ""),
		{ID: "t1", Name: TaskToolName, Args: map[string]any{}},
	})
	if len(gotBatch) != 1 || gotBatch[0].Call.ID != "c1" {
		t.Fatalf("finalizer batch = %+v, want only the code_task slot", gotBatch)
	}
	for _, resp := range responses {
		if resp.Name == CodeTaskToolName {
			if resp.CallID != "c1" {
				t.Errorf("CallID changed to %q", resp.CallID)
			}
			if resp.Response["batch_review"] == nil {
				t.Error("finalizer amendment missing from the code_task response")
			}
		}
	}
}

// A finalizer error is logged, never fatal: the children's responses survive.
func TestBatchFinalizerErrorIsNotFatal(t *testing.T) {
	t.Parallel()

	stub := &codeTaskStub{}
	s := contractScheduler(stub, WithSubagentBatchFinalizer(
		func(context.Context, []SubagentBatchItem, func(ui.StreamEvent)) error {
			return errors.New("review exploded")
		}))
	responses := executeForTest(t, s, []provider.ToolCall{contractCall("c1", "alpha", "")})
	if responses[0].Response["error"] != nil {
		t.Errorf("response = %v, want the child's success despite the finalizer error", responses[0].Response)
	}
}

// Cancellation still propagates through the finalizer.
func TestBatchFinalizerCancellationPropagates(t *testing.T) {
	t.Parallel()

	stub := &codeTaskStub{}
	s := contractScheduler(stub, WithSubagentBatchFinalizer(
		func(ctx context.Context, _ []SubagentBatchItem, _ func(ui.StreamEvent)) error {
			return ctx.Err()
		}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Execute(ctx, []provider.ToolCall{contractCall("c1", "alpha", "")}, func(ui.StreamEvent) {}); err == nil {
		t.Fatal("Execute with a canceled context must return the cancellation")
	}
}
