package tools

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

// sleepTaskStub poses as a subagent tool (name "task") while measuring how
// many invocations overlap. It sleeps long enough that any two calls admitted
// together must overlap.
type sleepTaskStub struct {
	current atomic.Int32
	maxSeen atomic.Int32
	delay   time.Duration
}

func (t *sleepTaskStub) Name() string { return TaskToolName }

func (t *sleepTaskStub) Description() string { return "concurrency probe" }

func (t *sleepTaskStub) Declaration() provider.ToolDeclaration {
	return provider.ToolDeclaration{Name: TaskToolName}
}

func (t *sleepTaskStub) RequiresConfirmation() bool { return false }

func (t *sleepTaskStub) Execute(_ context.Context, _ map[string]any) (map[string]any, error) {
	n := t.current.Add(1)
	for {
		m := t.maxSeen.Load()
		if n <= m || t.maxSeen.CompareAndSwap(m, n) {
			break
		}
	}
	time.Sleep(t.delay)
	t.current.Add(-1)
	return map[string]any{"status": "completed", "result": "ok"}, nil
}

func concurrencyScheduler(stub *sleepTaskStub, opts ...SchedulerOption) *Scheduler {
	registry := &Registry{
		byName:  map[string]Tool{TaskToolName: stub},
		aliases: map[string]string{},
	}
	return NewScheduler(registry, Policy{}, false, nil, nil, opts...)
}

func subagentCalls(n int) []provider.ToolCall {
	calls := make([]provider.ToolCall, n)
	for i := range calls {
		calls[i] = provider.ToolCall{Name: TaskToolName, ID: string(rune('a' + i))}
	}
	return calls
}

func drainExecute(t *testing.T, s *Scheduler, calls []provider.ToolCall) {
	t.Helper()
	emit := func(ui.StreamEvent) {}
	if _, err := s.Execute(context.Background(), calls, emit); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

// TestSubagentConcurrencyDefaultsToFanOut pins the default: without an
// option, sibling subagents run together.
func TestSubagentConcurrencyDefaultsToFanOut(t *testing.T) {
	t.Parallel()

	stub := &sleepTaskStub{delay: 200 * time.Millisecond}
	drainExecute(t, concurrencyScheduler(stub), subagentCalls(4))
	if got := stub.maxSeen.Load(); got != 4 {
		t.Errorf("max in-flight = %d, want 4 (default fans out)", got)
	}
}

// TestSubagentConcurrencyOneSerializes proves maxConcurrent=1 (concurrency
// disabled) never overlaps two children.
func TestSubagentConcurrencyOneSerializes(t *testing.T) {
	t.Parallel()

	stub := &sleepTaskStub{delay: 100 * time.Millisecond}
	s := concurrencyScheduler(stub, WithSubagentConcurrency(func() int { return 1 }))
	drainExecute(t, s, subagentCalls(4))
	if got := stub.maxSeen.Load(); got != 1 {
		t.Errorf("max in-flight = %d, want 1 (serial)", got)
	}
}

// TestSubagentConcurrencyReadsLive proves the cap is read at batch time: the
// same scheduler serializes one batch and fans out the next after the reader
// changes, with no rebuild.
func TestSubagentConcurrencyReadsLive(t *testing.T) {
	t.Parallel()

	var limit atomic.Int32
	limit.Store(1)
	stub := &sleepTaskStub{delay: 100 * time.Millisecond}
	s := concurrencyScheduler(stub, WithSubagentConcurrency(func() int { return int(limit.Load()) }))

	drainExecute(t, s, subagentCalls(3))
	if got := stub.maxSeen.Load(); got != 1 {
		t.Fatalf("max in-flight = %d, want 1 before the change", got)
	}
	limit.Store(3)
	stub.maxSeen.Store(0)
	drainExecute(t, s, subagentCalls(3))
	if got := stub.maxSeen.Load(); got != 3 {
		t.Errorf("max in-flight = %d, want 3 after the change", got)
	}
}

// TestSubagentConcurrencyIgnoresNonPositive proves a broken reader cannot
// wedge the scheduler: it falls back to the default instead of SetLimit(0),
// which would deadlock the batch.
func TestSubagentConcurrencyIgnoresNonPositive(t *testing.T) {
	t.Parallel()

	s := concurrencyScheduler(&sleepTaskStub{}, WithSubagentConcurrency(func() int { return 0 }))
	if got := s.subagentConcurrencyLimit(); got != config.DefaultSubagentMaxConcurrent {
		t.Errorf("limit = %d, want default %d", got, config.DefaultSubagentMaxConcurrent)
	}
}

// TestFormatCodeTaskResultIncomplete renders an incomplete hand-off the way
// the harness builds one (no "error" key): status head, files, checks, and
// the follow-up instruction must all survive onto the card.
func TestFormatCodeTaskResultIncomplete(t *testing.T) {
	t.Parallel()

	text, _, isErr := formatToolResult(CodeTaskToolName, map[string]any{
		"status":        "incomplete",
		"result":        "partial work",
		"files_changed": []string{"a.go"},
		"files_written": []string{"a.go"},
		"checks":        map[string]any{"ran": true, "ok": false},
		"tool_calls":    12,
		"attempt":       2,
		"max_attempts":  2,
		"next_step":     "finish it yourself",
	}, "")
	if isErr {
		t.Error("incomplete hand-off must not flag the card as an error")
	}
	for _, want := range []string{"incomplete", "a.go", "Checks FAILED", "attempt 2 of 2", "finish it yourself"} {
		if !strings.Contains(text, want) {
			t.Errorf("card %q does not contain %q", text, want)
		}
	}
}
