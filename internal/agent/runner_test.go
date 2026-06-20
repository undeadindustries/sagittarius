package agent

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

type fakeGenerator struct {
	mu        sync.Mutex
	lastReq   *provider.GenerateRequest
	responses []provider.StreamResponse
	block     chan struct{}
}

func (f *fakeGenerator) GenerateContentStream(ctx context.Context, req *provider.GenerateRequest) (<-chan provider.StreamResponse, error) {
	f.mu.Lock()
	f.lastReq = req
	responses := append([]provider.StreamResponse(nil), f.responses...)
	block := f.block
	f.mu.Unlock()

	ch := make(chan provider.StreamResponse)
	go func() {
		defer close(ch)
		for _, resp := range responses {
			if block != nil {
				select {
				case <-ctx.Done():
					ch <- provider.StreamResponse{Error: ctx.Err()}
					return
				case <-block:
				}
			}
			select {
			case <-ctx.Done():
				ch <- provider.StreamResponse{Error: ctx.Err()}
				return
			case ch <- resp:
			}
		}
	}()
	return ch, nil
}

func (f *fakeGenerator) lastRequest() *provider.GenerateRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastReq
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func collectEvents(t *testing.T, events <-chan ui.StreamEvent) []ui.StreamEvent {
	t.Helper()
	var out []ui.StreamEvent
	for ev := range events {
		out = append(out, ev)
	}
	return out
}

func TestRunnerSingleTurnMock(t *testing.T) {
	t.Parallel()

	gen := &fakeGenerator{
		responses: []provider.StreamResponse{
			{TextDelta: "Hello"},
			{TextDelta: ", world!"},
			{Done: true},
		},
	}

	runner, err := NewRunner(RunnerConfig{
		Generator: gen,
		Model:     "test-model",
		WorkDir:   t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	events, err := runner.RunTurn(testContext(t), "hi there")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	got := collectEvents(t, events)
	if len(got) < 3 {
		t.Fatalf("events = %#v, want text deltas and done", got)
	}
	if got[0].Type != ui.StreamTextDelta || got[0].Text != "Hello" {
		t.Fatalf("first event = %#v", got[0])
	}
	if got[len(got)-1].Type != ui.StreamDone {
		t.Fatalf("last event = %#v, want StreamDone", got[len(got)-1])
	}
	if runner.State() != StateDone {
		t.Fatalf("state = %v, want StateDone", runner.State())
	}
	if req := gen.lastRequest(); req == nil || len(req.Messages) != 1 {
		t.Fatalf("request messages = %#v", req)
	}
}

func TestHeadlessPromptFlag(t *testing.T) {
	t.Parallel()

	gen := &fakeGenerator{
		responses: []provider.StreamResponse{
			{TextDelta: "headless "},
			{TextDelta: "output"},
			{Done: true},
		},
	}

	runner, err := NewRunner(RunnerConfig{
		Generator: gen,
		Model:     "test-model",
		WorkDir:   t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	var buf bytes.Buffer
	if err := runner.RunHeadless(testContext(t), "run this", &buf); err != nil {
		t.Fatalf("RunHeadless: %v", err)
	}
	if buf.String() != "headless output" {
		t.Fatalf("output = %q, want %q", buf.String(), "headless output")
	}
}

func TestCancelMidStream(t *testing.T) {
	t.Parallel()

	block := make(chan struct{})
	gen := &fakeGenerator{
		block: block,
		responses: []provider.StreamResponse{
			{TextDelta: "partial"},
			{Done: true},
		},
	}

	runner, err := NewRunner(RunnerConfig{
		Generator: gen,
		Model:     "test-model",
		WorkDir:   t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, err := runner.RunTurn(ctx, "cancel me")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	cancel()

	var sawCancel bool
	for ev := range events {
		if ev.Type == ui.StreamError && errors.Is(ev.Err, context.Canceled) {
			sawCancel = true
		}
	}
	if !sawCancel {
		t.Fatal("expected context.Canceled stream error")
	}
}

func TestGEMINIMDInjection(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	projectDir := filepath.Join(root, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	memoryPath := filepath.Join(projectDir, "GEMINI.md")
	if err := os.WriteFile(memoryPath, []byte("Use Go idioms."), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	prevGetWorkDir := getWorkDir
	getWorkDir = func() (string, error) { return projectDir, nil }
	t.Cleanup(func() { getWorkDir = prevGetWorkDir })

	gen := &fakeGenerator{
		responses: []provider.StreamResponse{{Done: true}},
	}

	runner, err := NewRunner(RunnerConfig{
		Generator: gen,
		Model:     "test-model",
		WorkDir:   projectDir,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	events, err := runner.RunTurn(testContext(t), "hello")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	drainEvents(t, events)

	req := gen.lastRequest()
	if req == nil {
		t.Fatal("expected generate request")
	}
	if !strings.Contains(req.SystemInstruction, "Use Go idioms.") {
		t.Fatalf("system instruction = %q, want GEMINI.md content", req.SystemInstruction)
	}
	if !strings.Contains(req.SystemInstruction, memoryPath) {
		t.Fatalf("system instruction = %q, want memory path", req.SystemInstruction)
	}
}

func TestRunnerToolCallStub(t *testing.T) {
	t.Parallel()

	gen := &fakeGenerator{
		responses: []provider.StreamResponse{
			{ToolCalls: []provider.ToolCall{{Name: "grep", Args: map[string]any{"pattern": "foo"}}}},
			{Done: true},
		},
	}

	runner, err := NewRunner(RunnerConfig{
		Generator: gen,
		Model:     "test-model",
		WorkDir:   t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	events, err := runner.RunTurn(testContext(t), "search")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	got := collectEvents(t, events)
	foundTool := false
	for _, ev := range got {
		if ev.Type == ui.StreamToolStart && ev.ToolName == "grep" {
			foundTool = true
		}
	}
	if !foundTool {
		t.Fatalf("events = %#v, want StreamToolStart", got)
	}
	if runner.State() != StateDone {
		t.Fatalf("state = %v, want StateDone after tool stub", runner.State())
	}
}

func drainEvents(t *testing.T, events <-chan ui.StreamEvent) {
	t.Helper()
	for range events {
	}
}

func TestRunHeadlessWriteError(t *testing.T) {
	t.Parallel()

	gen := &fakeGenerator{
		responses: []provider.StreamResponse{{TextDelta: "x", Done: true}},
	}
	runner, err := NewRunner(RunnerConfig{
		Generator: gen,
		Model:     "test-model",
		WorkDir:   t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	err = runner.RunHeadless(testContext(t), "hello", failingWriter{})
	if err == nil {
		t.Fatal("expected write error")
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, io.ErrClosedPipe
}
