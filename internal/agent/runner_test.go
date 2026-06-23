package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/tools"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

type fakeGenerator struct {
	mu      sync.Mutex
	lastReq *provider.GenerateRequest
	batches [][]provider.StreamResponse
	call    int
	block   chan struct{}
}

func (f *fakeGenerator) GenerateContentStream(ctx context.Context, req *provider.GenerateRequest) (<-chan provider.StreamResponse, error) {
	f.mu.Lock()
	f.lastReq = req
	var responses []provider.StreamResponse
	if f.call < len(f.batches) {
		responses = append([]provider.StreamResponse(nil), f.batches[f.call]...)
	}
	f.call++
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

func TestSessionMetricsAccumulate(t *testing.T) {
	t.Parallel()

	gen := &fakeGenerator{
		batches: [][]provider.StreamResponse{{
			{TextDelta: "Hello there, this is a reply."},
			{Done: true},
		}},
	}
	runner, err := NewRunner(RunnerConfig{
		Generator:   gen,
		Model:       "test-model",
		WorkDir:     t.TempDir(),
		Interactive: false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	events, err := runner.RunTurn(testContext(t), "hi there")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	collectEvents(t, events)

	stats := runner.Stats()
	if stats.Turns != 1 {
		t.Errorf("Turns = %d, want 1", stats.Turns)
	}
	if stats.OutputTokens <= 0 {
		t.Errorf("OutputTokens = %d, want > 0", stats.OutputTokens)
	}
	if stats.InputTokens <= 0 {
		t.Errorf("InputTokens = %d, want > 0", stats.InputTokens)
	}
	if stats.ContextTokens <= 0 {
		t.Errorf("ContextTokens = %d, want > 0", stats.ContextTokens)
	}
	// No context manager configured, so no limit is known.
	if stats.ContextLimit != 0 {
		t.Errorf("ContextLimit = %d, want 0 (no manager)", stats.ContextLimit)
	}
	if got := stats.ContextPercent(); got != -1 {
		t.Errorf("ContextPercent = %d, want -1 when no limit", got)
	}
}

func TestSessionMetricsPerModelUsage(t *testing.T) {
	t.Parallel()

	gen := &fakeGenerator{
		batches: [][]provider.StreamResponse{{
			{TextDelta: "Reply from the model."},
			{Done: true},
		}},
	}
	runner, err := NewRunner(RunnerConfig{
		Generator:   gen,
		Model:       "test-model",
		WorkDir:     t.TempDir(),
		Interactive: false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	events, err := runner.RunTurn(testContext(t), "hello")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	collectEvents(t, events)

	stats := runner.Stats()
	if len(stats.ModelUsage) == 0 {
		t.Fatal("expected ModelUsage entries, got none")
	}

	var mainEntry *ui.ModelUsageStat
	for i := range stats.ModelUsage {
		if stats.ModelUsage[i].Model == "test-model" {
			mainEntry = &stats.ModelUsage[i]
			break
		}
	}
	if mainEntry == nil {
		t.Fatal("expected a 'test-model' entry in ModelUsage")
	}
	if mainEntry.Model != "test-model" {
		t.Errorf("Model = %q, want %q", mainEntry.Model, "test-model")
	}
	if mainEntry.InTokens <= 0 {
		t.Errorf("InTokens = %d, want > 0", mainEntry.InTokens)
	}
	if mainEntry.OutTokens <= 0 {
		t.Errorf("OutTokens = %d, want > 0", mainEntry.OutTokens)
	}

	// RecordUsage (auxiliary/compression) should aggregate into the perKey map
	// under a different model entry.
	runner.RecordUsage("openai", "compression-model", "agent", 100, 50, 0, false)
	stats2 := runner.Stats()
	var compEntry *ui.ModelUsageStat
	for i := range stats2.ModelUsage {
		if stats2.ModelUsage[i].Model == "compression-model" {
			compEntry = &stats2.ModelUsage[i]
			break
		}
	}
	if compEntry == nil {
		t.Fatal("expected a 'compression-model' entry after RecordUsage")
	}
	if compEntry.InTokens != 100 || compEntry.OutTokens != 50 {
		t.Errorf("compression entry = in:%d out:%d, want in:100 out:50", compEntry.InTokens, compEntry.OutTokens)
	}
}

func TestRunnerSingleTurnMock(t *testing.T) {
	t.Parallel()

	gen := &fakeGenerator{
		batches: [][]provider.StreamResponse{{
			{TextDelta: "Hello"},
			{TextDelta: ", world!"},
			{Done: true},
		}},
	}

	runner, err := NewRunner(RunnerConfig{
		Generator:   gen,
		Model:       "test-model",
		WorkDir:     t.TempDir(),
		Interactive: false,
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
	req := gen.lastRequest()
	if req == nil || len(req.Messages) != 1 {
		t.Fatalf("request messages = %#v", req)
	}
	if len(req.Tools) == 0 {
		t.Fatal("expected tool declarations on generate request")
	}
}

func TestHeadlessPromptFlag(t *testing.T) {
	t.Parallel()

	gen := &fakeGenerator{
		batches: [][]provider.StreamResponse{{
			{TextDelta: "headless "},
			{TextDelta: "output"},
			{Done: true},
		}},
	}

	runner, err := NewRunner(RunnerConfig{
		Generator:   gen,
		Model:       "test-model",
		WorkDir:     t.TempDir(),
		Interactive: false,
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
		batches: [][]provider.StreamResponse{{
			{TextDelta: "partial"},
			{Done: true},
		}},
	}

	runner, err := NewRunner(RunnerConfig{
		Generator:   gen,
		Model:       "test-model",
		WorkDir:     t.TempDir(),
		Interactive: false,
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

func TestAGENTSMDInjection(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	projectDir := filepath.Join(root, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	memoryPath := filepath.Join(projectDir, "AGENTS.md")
	if err := os.WriteFile(memoryPath, []byte("Use Go idioms."), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	prevGetWorkDir := getWorkDir
	getWorkDir = func() (string, error) { return projectDir, nil }
	t.Cleanup(func() { getWorkDir = prevGetWorkDir })

	gen := &fakeGenerator{
		batches: [][]provider.StreamResponse{{{Done: true}}},
	}

	runner, err := NewRunner(RunnerConfig{
		Generator:   gen,
		Model:       "test-model",
		WorkDir:     projectDir,
		Interactive: false,
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
		t.Fatalf("system instruction = %q, want AGENTS.md content", req.SystemInstruction)
	}
	if !strings.Contains(req.SystemInstruction, memoryPath) {
		t.Fatalf("system instruction = %q, want memory path", req.SystemInstruction)
	}
}

// TestSystemPromptComposesPersonalityAndMemory verifies the runner sends the
// programmer base prompt composed with AGENTS.md memory, and that a provider
// promptMode of "lite" selects the low-context variant.
func TestSystemPromptComposesPersonalityAndMemory(t *testing.T) {
	t.Parallel()

	newRunnerWith := func(t *testing.T, settings *config.Settings) *provider.GenerateRequest {
		t.Helper()
		projectDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(projectDir, "AGENTS.md"), []byte("Use Go idioms."), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		gen := &fakeGenerator{batches: [][]provider.StreamResponse{{{Done: true}}}}
		runner, err := NewRunner(RunnerConfig{
			Generator:   gen,
			Model:       "test-model",
			WorkDir:     projectDir,
			Interactive: false,
			Settings:    settings,
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
		return req
	}

	t.Run("full default", func(t *testing.T) {
		req := newRunnerWith(t, nil)
		if !strings.Contains(req.SystemInstruction, "# Core Mandates") {
			t.Errorf("system instruction missing programmer full anchor:\n%s", req.SystemInstruction)
		}
		if !strings.Contains(req.SystemInstruction, "Use Go idioms.") {
			t.Error("system instruction should include AGENTS.md memory")
		}
		if !strings.Contains(req.SystemInstruction, "test-model") {
			t.Error("system instruction should name the model in its identity line")
		}
	})

	t.Run("lite via provider promptMode", func(t *testing.T) {
		settings := &config.Settings{
			Providers: &config.ProvidersSettings{
				Active: "openai",
				OpenAI: &config.ProviderInstanceConfig{PromptMode: config.PromptModeLite},
			},
		}
		req := newRunnerWith(t, settings)
		if !strings.Contains(req.SystemInstruction, "## Tool Usage") {
			t.Errorf("lite system instruction missing anchor:\n%s", req.SystemInstruction)
		}
		if strings.Contains(req.SystemInstruction, "# Core Mandates") {
			t.Error("lite variant should not contain the full Core Mandates section")
		}
		if !strings.Contains(req.SystemInstruction, "Use Go idioms.") {
			t.Error("lite system instruction should still include AGENTS.md memory")
		}
	})
}

func TestRunnerToolCallStub(t *testing.T) {
	t.Parallel()

	gen := &fakeGenerator{
		batches: [][]provider.StreamResponse{{
			{ToolCalls: []provider.ToolCall{{Name: "grep", Args: map[string]any{"pattern": "foo"}}}},
			{Done: true},
		}},
	}

	runner, err := NewRunner(RunnerConfig{
		Generator:    gen,
		Model:        "test-model",
		WorkDir:      t.TempDir(),
		ApprovalMode: ApprovalYolo,
		Interactive:  false,
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

func TestRunnerToolRoundTrip(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dataPath := filepath.Join(root, "data.txt")
	if err := os.WriteFile(dataPath, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}

	gen := &fakeGenerator{
		batches: [][]provider.StreamResponse{
			{
				{ToolCalls: []provider.ToolCall{{
					Name: tools.ReadFileToolName,
					Args: map[string]any{tools.ParamFilePath: "data.txt"},
				}}},
				{Done: true},
			},
			{
				{TextDelta: "read ok"},
				{Done: true},
			},
		},
	}

	runner, err := NewRunner(RunnerConfig{
		Generator:    gen,
		Model:        "test-model",
		WorkDir:      root,
		ApprovalMode: ApprovalYolo,
		Interactive:  false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	events, err := runner.RunTurn(testContext(t), "read file")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	got := collectEvents(t, events)

	var sawToolStart, sawToolResult, sawText bool
	for _, ev := range got {
		switch ev.Type {
		case ui.StreamToolStart:
			sawToolStart = true
		case ui.StreamToolResult:
			sawToolResult = true
		case ui.StreamTextDelta:
			if ev.Text == "read ok" {
				sawText = true
			}
		}
	}
	if !sawToolStart || !sawToolResult || !sawText {
		t.Fatalf("events = %#v, want tool start, result, and text", got)
	}
	if gen.call != 2 {
		t.Fatalf("generate calls = %d, want 2", gen.call)
	}
	if runner.State() != StateDone {
		t.Fatalf("state = %v, want StateDone", runner.State())
	}
}

func TestNeedsGoplsHint(t *testing.T) {
	t.Parallel()

	goRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(goRoot, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	nonGoRoot := t.TempDir()

	withGopls := &config.Settings{Raw: map[string]json.RawMessage{
		"mcpServers": json.RawMessage(`{"gopls":{"command":"gopls","args":["mcp"]}}`),
	}}

	if !needsGoplsHint(nil, goRoot) {
		t.Fatal("go project without gopls server should need the hint")
	}
	if needsGoplsHint(withGopls, goRoot) {
		t.Fatal("configured gopls server should suppress the hint")
	}
	if needsGoplsHint(nil, nonGoRoot) {
		t.Fatal("non-Go project should not get the hint")
	}
}

func TestRunnerSuggestVerifyAfterWrite(t *testing.T) {
	t.Parallel()

	writeThenFinish := func() [][]provider.StreamResponse {
		return [][]provider.StreamResponse{
			{
				{ToolCalls: []provider.ToolCall{{
					Name: tools.WriteFileToolName,
					Args: map[string]any{
						tools.ParamFilePath:         "out.txt",
						tools.WriteFileParamContent: "hello",
					},
				}}},
				{Done: true},
			},
			{
				{TextDelta: "done"},
				{Done: true},
			},
		}
	}

	cases := []struct {
		name    string
		suggest bool
		want    bool
	}{
		{name: "enabled emits reminder", suggest: true, want: true},
		{name: "disabled stays silent", suggest: false, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			runner, err := NewRunner(RunnerConfig{
				Generator:               &fakeGenerator{batches: writeThenFinish()},
				Model:                   "test-model",
				WorkDir:                 root,
				ApprovalMode:            ApprovalYolo,
				Interactive:             false,
				SuggestVerifyAfterWrite: tc.suggest,
			})
			if err != nil {
				t.Fatalf("NewRunner: %v", err)
			}
			events, err := runner.RunTurn(testContext(t), "write a file")
			if err != nil {
				t.Fatalf("RunTurn: %v", err)
			}
			sawReminder := false
			for _, ev := range collectEvents(t, events) {
				if ev.Type == ui.StreamInfo && ev.Text == verifyReminder {
					sawReminder = true
				}
			}
			if sawReminder != tc.want {
				t.Fatalf("reminder emitted = %v, want %v", sawReminder, tc.want)
			}
		})
	}
}

func TestRunnerHeadlessApprovalGatesWrite(t *testing.T) {
	t.Parallel()

	writeBatch := func() [][]provider.StreamResponse {
		return [][]provider.StreamResponse{
			{
				{ToolCalls: []provider.ToolCall{{
					Name: tools.WriteFileToolName,
					Args: map[string]any{
						tools.ParamFilePath:         "out.txt",
						tools.WriteFileParamContent: "hello",
					},
				}}},
				{Done: true},
			},
			{
				{TextDelta: "done"},
				{Done: true},
			},
		}
	}

	cases := []struct {
		name       string
		approval   ApprovalMode
		wantExists bool
	}{
		{"default denies headless write", ApprovalDefault, false},
		{"yolo allows headless write", ApprovalYolo, true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			runner, err := NewRunner(RunnerConfig{
				Generator:    &fakeGenerator{batches: writeBatch()},
				Model:        "test-model",
				WorkDir:      root,
				ApprovalMode: tc.approval,
				Interactive:  false,
			})
			if err != nil {
				t.Fatalf("NewRunner: %v", err)
			}
			events, err := runner.RunTurn(testContext(t), "write a file")
			if err != nil {
				t.Fatalf("RunTurn: %v", err)
			}
			collectEvents(t, events)

			_, statErr := os.Stat(filepath.Join(root, "out.txt"))
			exists := statErr == nil
			if exists != tc.wantExists {
				t.Fatalf("file exists = %v, want %v", exists, tc.wantExists)
			}
		})
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
		batches: [][]provider.StreamResponse{{{TextDelta: "x", Done: true}}},
	}
	runner, err := NewRunner(RunnerConfig{
		Generator:   gen,
		Model:       "test-model",
		WorkDir:     t.TempDir(),
		Interactive: false,
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

func TestRunnerKeylessStartupRecovers(t *testing.T) {
	t.Parallel()

	missing := errors.New("api key missing: set OPENAI_API_KEY")
	runner, err := NewRunner(RunnerConfig{
		Model:       "test-model",
		WorkDir:     t.TempDir(),
		Interactive: true,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	runner.SetGeneratorError(missing)

	if got := runner.GeneratorError(); !errors.Is(got, missing) {
		t.Fatalf("GeneratorError = %v, want %v", got, missing)
	}

	events, err := runner.RunTurn(testContext(t), "hello")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	got := collectEvents(t, events)
	if len(got) == 0 || got[0].Type != ui.StreamError || !errors.Is(got[0].Err, missing) {
		t.Fatalf("events = %#v, want leading StreamError with missing key", got)
	}
	if len(runner.history) != 0 {
		t.Fatalf("history = %#v, want empty after keyless turn", runner.history)
	}

	gen := &fakeGenerator{
		batches: [][]provider.StreamResponse{{{TextDelta: "recovered", Done: true}}},
	}
	runner.SetGenerator(gen)
	if runner.GeneratorError() != nil {
		t.Fatalf("GeneratorError after SetGenerator = %v, want nil", runner.GeneratorError())
	}

	events, err = runner.RunTurn(testContext(t), "again")
	if err != nil {
		t.Fatalf("RunTurn after recovery: %v", err)
	}
	got = collectEvents(t, events)
	if got[0].Type != ui.StreamTextDelta || got[0].Text != "recovered" {
		t.Fatalf("recovered events = %#v", got)
	}
}
