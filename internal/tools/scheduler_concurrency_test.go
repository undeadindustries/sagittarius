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

// TestFormatCodeTaskResultVia pins the routing line: a hand-off from a child
// on a different pair shows "via provider/model" on the card.
func TestFormatCodeTaskResultVia(t *testing.T) {
	t.Parallel()

	text, _, isErr := formatToolResult(CodeTaskToolName, map[string]any{
		"status":        "completed",
		"result":        "done",
		"files_changed": []string{"a.go"},
		"via":           "local/qwen3.8-27b",
	}, "")
	if isErr {
		t.Error("completed hand-off must not flag the card as an error")
	}
	if !strings.Contains(text, "via local/qwen3.8-27b") {
		t.Errorf("card %q does not name the child's pair", text)
	}
}

// TestFormatTaskResultVia covers the research card: status head only when not
// completed, via line when present, report last.
func TestFormatTaskResultVia(t *testing.T) {
	t.Parallel()

	text, _, _ := formatToolResult(TaskToolName, map[string]any{
		"status": "completed",
		"result": "the answer",
		"via":    "gemini/gemini-3-flash",
	}, "")
	if strings.Contains(text, "completed") {
		t.Errorf("completed status must not head the card: %q", text)
	}
	if !strings.Contains(text, "via gemini/gemini-3-flash") || !strings.Contains(text, "the answer") {
		t.Errorf("card %q missing via line or report", text)
	}

	text, _, _ = formatToolResult(TaskToolName, map[string]any{
		"status":     "failed",
		"result":     "boom",
		"tool_calls": 3,
	}, "")
	if !strings.Contains(text, "Subagent failed after 3 tool call(s)") {
		t.Errorf("card %q missing failure head", text)
	}
}

// TestNonInteractiveDenialNamesCause: a headless/subagent denial must not say
// "user denied" — no user was asked. The message names the gate and the way
// out, so a child stops retrying shell variants that all fail identically.
func TestNonInteractiveDenialNamesCause(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewBuiltinRegistry(ws)
	// interactive=false, default policy (not yolo): an unrecognized shell
	// command escalates and fails closed without a user.
	s := NewScheduler(registry, Policy{Mode: ApprovalDefault}, false, nil, ws,
		WithReadOnlyPolicy(func() ReadOnlyPolicy { return PolicyShellInspect }))

	var events []ui.StreamEvent
	emit := func(ev ui.StreamEvent) { events = append(events, ev) }
	calls := []provider.ToolCall{{
		Name: ShellToolName,
		ID:   "s1",
		Args: map[string]any{ShellParamCommand: "frobnicate --widget"},
	}}
	if _, err := s.Execute(context.Background(), calls, emit); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var got string
	for _, ev := range events {
		if ev.Type == ui.StreamToolResult && ev.IsError {
			got = ev.Text
		}
	}
	if got == "" {
		t.Fatal("expected an error result")
	}
	if strings.Contains(got, "user denied") {
		t.Errorf("non-interactive denial must not blame a user: %q", got)
	}
	for _, want := range []string{"non-interactive", "read-only"} {
		if !strings.Contains(got, want) {
			t.Errorf("denial %q does not name %q", got, want)
		}
	}
}

// badgeStub is a StartBadger: the scheduler must copy its label onto the
// StreamToolStart event so the card border can show it.
type badgeStub struct{ label string }

func (t *badgeStub) Name() string        { return "badge_stub" }
func (t *badgeStub) Description() string { return "badge probe" }
func (t *badgeStub) Declaration() provider.ToolDeclaration {
	return provider.ToolDeclaration{Name: "badge_stub"}
}
func (t *badgeStub) RequiresConfirmation() bool { return false }
func (t *badgeStub) StartBadge() string         { return t.label }
func (t *badgeStub) Execute(context.Context, map[string]any) (map[string]any, error) {
	return map[string]any{"ok": true}, nil
}

func startEvents(events []ui.StreamEvent) []ui.StreamEvent {
	var out []ui.StreamEvent
	for _, ev := range events {
		if ev.Type == ui.StreamToolStart {
			out = append(out, ev)
		}
	}
	return out
}

// TestStartBadgeFlowsToStartEvent pins the scheduler seam: a StartBadger's
// label lands on StreamToolStart.Badge; a plain tool gets an empty badge; an
// unknown tool still gets a start event (then the unknown-tool error), exactly
// as before the badge existed.
func TestStartBadgeFlowsToStartEvent(t *testing.T) {
	t.Parallel()

	stub := &badgeStub{label: "local/qwen3.8-27b"}
	registry := &Registry{
		byName:  map[string]Tool{"badge_stub": stub},
		aliases: map[string]string{},
	}
	s := NewScheduler(registry, Policy{}, false, nil, nil)

	var events []ui.StreamEvent
	emit := func(ev ui.StreamEvent) { events = append(events, ev) }
	calls := []provider.ToolCall{
		{Name: "badge_stub", ID: "b1"},
		{Name: "no_such_tool", ID: "x1"},
	}
	if _, err := s.Execute(context.Background(), calls, emit); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	starts := startEvents(events)
	if len(starts) != 2 {
		t.Fatalf("want 2 start events, got %d", len(starts))
	}
	byID := map[string]string{}
	for _, ev := range starts {
		byID[ev.ToolCallID] = ev.Badge
	}
	if byID["b1"] != "local/qwen3.8-27b" {
		t.Errorf("badge_stub start badge = %q, want the label", byID["b1"])
	}
	if byID["x1"] != "" {
		t.Errorf("unknown tool start badge = %q, want empty", byID["x1"])
	}
	var sawUnknownErr bool
	for _, ev := range events {
		if ev.Type == ui.StreamToolResult && ev.ToolCallID == "x1" && ev.IsError {
			sawUnknownErr = true
		}
	}
	if !sawUnknownErr {
		t.Error("unknown tool must still produce its error result after the start event")
	}
}
