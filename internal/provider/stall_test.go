package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/genai"

	"github.com/undeadindustries/sagittarius/internal/config"
)

const (
	testStallBudget = 100 * time.Millisecond
	// testStallPause outlasts testStallBudget so a guard that is still armed
	// would fire during it.
	testStallPause = 300 * time.Millisecond
	// testHang is how long a stalled fake server waits before giving up on
	// its own; the guard must fire long before this.
	testHang = 3 * time.Second
)

// sseStep is one action of a scripted SSE server: write data (if any), flush,
// then pause.
type sseStep struct {
	data  string
	pause time.Duration
}

// scriptedSSE serves headers after headerDelay, then plays steps. Every wait
// ends early when the client goes away so srv.Close never blocks.
func scriptedSSE(t *testing.T, headerDelay time.Duration, steps ...sseStep) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The server watches for client disconnects only once the body is
		// consumed; without this a canceled request leaves the handler waiting.
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			return
		}
		wait := func(d time.Duration) bool {
			if d <= 0 {
				return true
			}
			select {
			case <-time.After(d):
				return true
			case <-r.Context().Done():
				return false
			}
		}
		if !wait(headerDelay) {
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		flusher.Flush()
		for _, step := range steps {
			if step.data != "" {
				if _, err := w.Write([]byte(step.data)); err != nil {
					return
				}
				flusher.Flush()
			}
			if !wait(step.pause) {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func sseData(payload string) string { return "data: " + payload + "\n\n" }

const (
	chatRoleOnly   = `{"choices":[{"index":0,"delta":{"role":"assistant"}}]}`
	chatText       = `{"choices":[{"index":0,"delta":{"content":"hi"}}]}`
	chatToolDelta  = `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"file_path\":"}}]}}]}`
	chatToolDelta2 = `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"a.go\"}"}}]}}]}`
	chatFinishTool = `{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`
	chatFinishStop = `{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`
	sseDone        = "data: [DONE]\n\n"
)

type streamOutcome struct {
	text      string
	toolCalls int
	done      bool
	err       error
}

func drainOutcome(ch <-chan StreamResponse) streamOutcome {
	var out streamOutcome
	var text strings.Builder
	for chunk := range ch {
		text.WriteString(chunk.TextDelta)
		out.toolCalls += len(chunk.ToolCalls)
		if chunk.Done {
			out.done = true
		}
		if chunk.Error != nil && out.err == nil {
			out.err = chunk.Error
		}
	}
	out.text = text.String()
	return out
}

func runChat(t *testing.T, ctx context.Context, srv *httptest.Server, connect, firstToken time.Duration) streamOutcome {
	t.Helper()
	gen, err := NewOpenAIChatGenerator(OpenAIChatConfig{
		BaseURL:           srv.URL + "/v1/chat/completions",
		Model:             "local",
		HTTPClient:        srv.Client(),
		ProviderID:        "local",
		ConnectTimeout:    connect,
		FirstTokenTimeout: firstToken,
	})
	if err != nil {
		t.Fatalf("NewOpenAIChatGenerator: %v", err)
	}
	ch, err := gen.GenerateContentStream(ctx, &GenerateRequest{
		Messages: []Message{{Role: RoleUser, Parts: []Part{{Text: "hi"}}}},
	})
	if err != nil {
		t.Fatalf("GenerateContentStream: %v", err)
	}
	return drainOutcome(ch)
}

func TestOpenAIChatStallGuard(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		headerDelay time.Duration
		steps       []sseStep
		connect     time.Duration
		firstToken  time.Duration
		wantKind    error // nil means the stream must finish cleanly
		wantTools   int
		wantText    string
	}{
		{
			name:        "no headers trips connect timeout",
			headerDelay: testHang,
			connect:     testStallBudget,
			firstToken:  testStallBudget,
			wantKind:    ErrConnectTimeout,
		},
		{
			name:       "headers then silence trips first-token timeout",
			steps:      []sseStep{{pause: testHang}},
			connect:    testStallBudget,
			firstToken: testStallBudget,
			wantKind:   ErrFirstTokenTimeout,
		},
		{
			name: "keep-alive comments are not output",
			steps: []sseStep{
				{data: ": ping\n\n", pause: testStallBudget / 2},
				{data: ": ping\n\n", pause: testStallBudget / 2},
				{data: ": ping\n\n", pause: testStallBudget / 2},
				{data: ": ping\n\n", pause: testHang},
			},
			connect:    testStallBudget,
			firstToken: testStallBudget,
			wantKind:   ErrFirstTokenTimeout,
		},
		{
			name:       "role-only delta is not output",
			steps:      []sseStep{{data: sseData(chatRoleOnly), pause: testHang}},
			connect:    testStallBudget,
			firstToken: testStallBudget,
			wantKind:   ErrFirstTokenTimeout,
		},
		{
			name: "buffered tool-call deltas disarm the guard",
			steps: []sseStep{
				{data: sseData(chatToolDelta), pause: testStallPause},
				{data: sseData(chatToolDelta2)},
				{data: sseData(chatFinishTool) + sseDone},
			},
			connect:    testStallBudget,
			firstToken: testStallBudget,
			wantTools:  1,
		},
		{
			name: "pause after output is never cut off",
			steps: []sseStep{
				{data: sseData(chatText), pause: testStallPause},
				{data: sseData(chatFinishStop) + sseDone},
			},
			connect:    testStallBudget,
			firstToken: testStallBudget,
			wantText:   "hi",
		},
		{
			name:        "zero budgets disable both stages",
			headerDelay: testStallPause,
			steps: []sseStep{
				{pause: testStallPause},
				{data: sseData(chatText) + sseData(chatFinishStop) + sseDone},
			},
			wantText: "hi",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := scriptedSSE(t, tt.headerDelay, tt.steps...)
			out := runChat(t, testContext(t), srv, tt.connect, tt.firstToken)
			if tt.wantKind != nil {
				assertStall(t, out.err, tt.wantKind)
				return
			}
			if out.err != nil {
				t.Fatalf("unexpected error: %v", out.err)
			}
			if !out.done {
				t.Fatal("stream ended without Done")
			}
			if out.toolCalls != tt.wantTools {
				t.Fatalf("tool calls = %d, want %d", out.toolCalls, tt.wantTools)
			}
			if out.text != tt.wantText {
				t.Fatalf("text = %q, want %q", out.text, tt.wantText)
			}
		})
	}
}

func assertStall(t *testing.T, err, wantKind error) {
	t.Helper()
	var stall *StallError
	if !errors.As(err, &stall) {
		t.Fatalf("error = %v, want *StallError", err)
	}
	if !errors.Is(err, wantKind) {
		t.Fatalf("error = %v, want kind %v", err, wantKind)
	}
	if stall.After != testStallBudget {
		t.Fatalf("After = %s, want %s", stall.After, testStallBudget)
	}
	setting := "connectTimeout"
	if errors.Is(wantKind, ErrFirstTokenTimeout) {
		setting = "firstTokenTimeout"
	}
	if !strings.Contains(err.Error(), "providers.local."+setting) {
		t.Fatalf("message %q does not name the setting %s", err.Error(), setting)
	}
}

func TestOpenAIChatUserCancelIsNotStall(t *testing.T) {
	t.Parallel()
	srv := scriptedSSE(t, testHang)
	ctx, cancel := context.WithCancel(testContext(t))
	time.AfterFunc(testStallBudget/2, cancel)
	start := time.Now()
	out := runChat(t, ctx, srv, testStallBudget*10, testStallBudget*10)
	if elapsed := time.Since(start); elapsed > testStallBudget*5 {
		t.Fatalf("cancel took %s to end the stream", elapsed)
	}
	if out.err == nil {
		t.Fatal("expected an error after cancel")
	}
	var stall *StallError
	if errors.As(out.err, &stall) {
		t.Fatalf("user cancel reported as stall: %v", out.err)
	}
}

func TestOpenAIResponsesStallGuard(t *testing.T) {
	t.Parallel()

	created := `{"type":"response.created","response":{"id":"resp_1"}}`
	inProgress := `{"type":"response.in_progress","response":{"id":"resp_1"}}`
	delta := `{"type":"response.output_text.delta","delta":"hi"}`
	completed := `{"type":"response.completed","response":{"id":"resp_1","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`

	tests := []struct {
		name     string
		steps    []sseStep
		wantKind error
		wantText string
	}{
		{
			name:     "lifecycle events are not output",
			steps:    []sseStep{{data: sseData(created) + sseData(inProgress), pause: testHang}},
			wantKind: ErrFirstTokenTimeout,
		},
		{
			name: "pause after output is never cut off",
			steps: []sseStep{
				{data: sseData(created) + sseData(delta), pause: testStallPause},
				{data: sseData(completed)},
			},
			wantText: "hi",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := scriptedSSE(t, 0, tt.steps...)
			gen, err := NewOpenAIResponsesGenerator(OpenAIResponsesConfig{
				BaseURL:           srv.URL + "/v1/responses",
				Model:             "gpt-test",
				HTTPClient:        srv.Client(),
				ProviderID:        "local",
				ConnectTimeout:    testStallBudget,
				FirstTokenTimeout: testStallBudget,
			})
			if err != nil {
				t.Fatalf("NewOpenAIResponsesGenerator: %v", err)
			}
			ch, err := gen.GenerateContentStream(testContext(t), &GenerateRequest{
				Messages: []Message{{Role: RoleUser, Parts: []Part{{Text: "hi"}}}},
			})
			if err != nil {
				t.Fatalf("GenerateContentStream: %v", err)
			}
			out := drainOutcome(ch)
			if tt.wantKind != nil {
				assertStall(t, out.err, tt.wantKind)
				return
			}
			if out.err != nil {
				t.Fatalf("unexpected error: %v", out.err)
			}
			if out.text != tt.wantText {
				t.Fatalf("text = %q, want %q", out.text, tt.wantText)
			}
		})
	}
}

// delayedStreamer waits before each chunk, honoring ctx the way the genai SDK
// does when its request is canceled.
type delayedStreamer struct {
	delays []time.Duration
	chunks []*genai.GenerateContentResponse
}

func (d delayedStreamer) GenerateContentStream(
	ctx context.Context,
	_ string,
	_ []*genai.Content,
	_ *genai.GenerateContentConfig,
) iter.Seq2[*genai.GenerateContentResponse, error] {
	return func(yield func(*genai.GenerateContentResponse, error) bool) {
		for i, chunk := range d.chunks {
			select {
			case <-time.After(d.delays[i]):
			case <-ctx.Done():
				yield(nil, fmt.Errorf("genai stream: %w", ctx.Err()))
				return
			}
			if !yield(chunk, nil) {
				return
			}
		}
	}
}

func TestGeminiStallGuard(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		streamer delayedStreamer
		wantKind error
		wantText string
	}{
		{
			name: "no first chunk trips first-token timeout",
			streamer: delayedStreamer{
				delays: []time.Duration{testHang},
				chunks: []*genai.GenerateContentResponse{textResponse("late")},
			},
			wantKind: ErrFirstTokenTimeout,
		},
		{
			name: "pause after first chunk is never cut off",
			streamer: delayedStreamer{
				delays: []time.Duration{0, testStallPause},
				chunks: []*genai.GenerateContentResponse{textResponse("h"), textResponse("i")},
			},
			wantText: "hi",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gen, err := NewGeminiGenerator(testContext(t), GeminiConfig{
				Model:             "gemini-test",
				Streamer:          tt.streamer,
				ProviderID:        "local",
				FirstTokenTimeout: testStallBudget,
			})
			if err != nil {
				t.Fatalf("NewGeminiGenerator: %v", err)
			}
			ch, err := gen.GenerateContentStream(testContext(t), &GenerateRequest{
				Messages: []Message{{Role: RoleUser, Parts: []Part{{Text: "hi"}}}},
			})
			if err != nil {
				t.Fatalf("GenerateContentStream: %v", err)
			}
			out := drainOutcome(ch)
			if tt.wantKind != nil {
				assertStall(t, out.err, tt.wantKind)
				return
			}
			if out.err != nil {
				t.Fatalf("unexpected error: %v", out.err)
			}
			if out.text != tt.wantText {
				t.Fatalf("text = %q, want %q", out.text, tt.wantText)
			}
		})
	}
}

func TestWithStallTimeouts(t *testing.T) {
	t.Parallel()
	intPtr := func(n int) *int { return &n }

	tests := []struct {
		name           string
		inst           *config.ProviderInstanceConfig
		wantConnect    time.Duration
		wantFirstToken time.Duration
	}{
		{"nil instance uses defaults", nil, defaultConnectTimeout, defaultFirstTokenTimeout},
		{"unset fields use defaults", &config.ProviderInstanceConfig{}, defaultConnectTimeout, defaultFirstTokenTimeout},
		{
			"explicit values",
			&config.ProviderInstanceConfig{ConnectTimeout: intPtr(5), FirstTokenTimeout: intPtr(900)},
			5 * time.Second, 900 * time.Second,
		},
		{
			"zero disables",
			&config.ProviderInstanceConfig{ConnectTimeout: intPtr(0), FirstTokenTimeout: intPtr(0)},
			0, 0,
		},
		{
			"negative falls back to default",
			&config.ProviderInstanceConfig{ConnectTimeout: intPtr(-1)},
			defaultConnectTimeout, defaultFirstTokenTimeout,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := withStallTimeouts(EndpointConfig{}, tt.inst)
			if got.ConnectTimeout != tt.wantConnect || got.FirstTokenTimeout != tt.wantFirstToken {
				t.Fatalf("got connect=%s firstToken=%s, want %s/%s",
					got.ConnectTimeout, got.FirstTokenTimeout, tt.wantConnect, tt.wantFirstToken)
			}
		})
	}
}

func TestParseNonNegativeInt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		value   string
		want    int
		wantErr bool
	}{
		{"0", 0, false},
		{"600", 600, false},
		{"-5", 0, true},
		{"soon", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			t.Parallel()
			got, err := parseNonNegativeInt("firstTokenTimeout", tt.value)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parse %q: expected error", tt.value)
				}
				return
			}
			if err != nil || *got != tt.want {
				t.Fatalf("parse %q = %v, %v; want %d", tt.value, got, err, tt.want)
			}
		})
	}
}
