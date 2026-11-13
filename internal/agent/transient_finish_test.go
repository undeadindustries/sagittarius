package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

func malformedBatch() []provider.StreamResponse {
	return []provider.StreamResponse{{FinishReason: "MALFORMED_RESPONSE"}, {Done: true}}
}

func newTransientRunner(t *testing.T, gen *fakeGenerator) *Runner {
	t.Helper()
	t.Setenv("SAGITTARIUS_HOME", t.TempDir())
	runner, err := NewRunner(RunnerConfig{
		Generator:    gen,
		Model:        "gemini-4-hydrogen",
		WorkDir:      t.TempDir(),
		ApprovalMode: ApprovalYolo,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	return runner
}

func userMessages(t *testing.T, runner *Runner) int {
	t.Helper()
	n := 0
	for _, msg := range runner.History() {
		if msg.Role == provider.RoleUser {
			n++
		}
	}
	return n
}

func TestTransientFinishRetriesOnceThenSucceeds(t *testing.T) {
	gen := &fakeGenerator{batches: [][]provider.StreamResponse{
		malformedBatch(),
		{{TextDelta: "recovered"}, {Done: true}},
	}}
	runner := newTransientRunner(t, gen)
	events, err := runner.RunTurn(testContext(t), "load the model")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	var text strings.Builder
	sawRetry := false
	for _, ev := range collectEvents(t, events) {
		switch ev.Type {
		case ui.StreamError:
			t.Fatalf("unexpected error: %q %v", ev.Text, ev.Err)
		case ui.StreamTextDelta:
			text.WriteString(ev.Text)
		case ui.StreamInfo:
			if strings.Contains(ev.Text, "Retrying (1/2)") {
				sawRetry = true
			}
		}
	}
	if !sawRetry {
		t.Fatal("missing retry notice")
	}
	if text.String() != "recovered" {
		t.Fatalf("text = %q, want recovered", text.String())
	}
	if got := gen.calls(); got != 2 {
		t.Fatalf("generator calls = %d, want 2", got)
	}
	if got := userMessages(t, runner); got != 1 {
		t.Fatalf("user messages = %d, want 1", got)
	}
}

func TestTransientFinishStopsAfterTwoRetries(t *testing.T) {
	gen := &fakeGenerator{batches: [][]provider.StreamResponse{
		malformedBatch(),
		malformedBatch(),
		malformedBatch(),
		{{TextDelta: "should not run"}, {Done: true}},
	}}
	runner := newTransientRunner(t, gen)
	events, err := runner.RunTurn(testContext(t), "load the model")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	sawNotice, sawDone := false, false
	retries := 0
	for _, ev := range collectEvents(t, events) {
		switch ev.Type {
		case ui.StreamError:
			if ev.Text == emptyModelReplyMsg || ev.Err != nil {
				t.Fatalf("unexpected error: %q %v", ev.Text, ev.Err)
			}
		case ui.StreamInfo:
			if strings.Contains(ev.Text, "Retrying") {
				retries++
			}
			if strings.Contains(ev.Text, "MALFORMED_RESPONSE") {
				sawNotice = true
			}
		case ui.StreamDone:
			sawDone = true
		}
	}
	if retries != maxTransientFinishRetries {
		t.Fatalf("retries = %d, want %d", retries, maxTransientFinishRetries)
	}
	if !sawNotice {
		t.Fatal("missing finish notice after retries were used up")
	}
	if !sawDone {
		t.Fatal("turn did not finish")
	}
	if got := gen.calls(); got != maxTransientFinishRetries+1 {
		t.Fatalf("generator calls = %d, want %d", got, maxTransientFinishRetries+1)
	}
}

func TestTransientFinishCancelDuringBackoff(t *testing.T) {
	gen := &fakeGenerator{batches: [][]provider.StreamResponse{
		malformedBatch(),
		{{TextDelta: "should not run"}, {Done: true}},
	}}
	runner := newTransientRunner(t, gen)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := runner.RunTurn(ctx, "load the model")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range events {
			if ev.Type == ui.StreamInfo && strings.Contains(ev.Text, "Retrying") {
				cancel()
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("turn did not exit during backoff")
	}
	if got := gen.calls(); got != 1 {
		t.Fatalf("generator calls = %d, want 1 (cancelled before the resend)", got)
	}
}
