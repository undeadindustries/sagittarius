package provider

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// capturedRequests records every request body a fake Responses endpoint received.
type capturedRequests struct {
	mu     sync.Mutex
	bodies []map[string]any
}

func (c *capturedRequests) record(r *http.Request) {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	c.mu.Lock()
	c.bodies = append(c.bodies, body)
	c.mu.Unlock()
}

func (c *capturedRequests) all() []map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]map[string]any(nil), c.bodies...)
}

// reasoningOf extracts the reasoning object from the nth captured request.
func reasoningOf(t *testing.T, bodies []map[string]any, n int) map[string]any {
	t.Helper()
	if len(bodies) <= n {
		t.Fatalf("request %d was never sent (got %d)", n, len(bodies))
	}
	reasoning, ok := bodies[n]["reasoning"].(map[string]any)
	if !ok {
		t.Fatalf("request %d carried no reasoning object: %#v", n, bodies[n])
	}
	return reasoning
}

func drain(t *testing.T, gen *OpenAIResponsesGenerator, req *GenerateRequest) error {
	t.Helper()
	ch, err := gen.GenerateContentStream(testContext(t), req)
	if err != nil {
		t.Fatalf("GenerateContentStream: %v", err)
	}
	var streamErr error
	for resp := range ch {
		if resp.Error != nil {
			streamErr = resp.Error
		}
	}
	return streamErr
}

// TestResponsesRequestsReasoningSummary is the wire half of the honest-indicator
// fix: without reasoning.summary the API emits no reasoning_summary_text.delta
// events at all, so the thinking box stays empty and the status label cannot
// tell reasoning apart from a network wait.
func TestResponsesRequestsReasoningSummary(t *testing.T) {
	t.Parallel()

	captured := &capturedRequests{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.record(r)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(responsesSSEResponse(`{"type":"response.completed","response":{"id":"r"}}`)))
	}))
	t.Cleanup(srv.Close)

	gen, err := NewOpenAIResponsesGenerator(OpenAIResponsesConfig{
		BaseURL:    srv.URL,
		Model:      "gpt-5-codex",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewOpenAIResponsesGenerator: %v", err)
	}

	if err := drain(t, gen, &GenerateRequest{
		Messages:  []Message{{Role: RoleUser, Parts: []Part{{Text: "hi"}}}},
		Reasoning: &ReasoningRequest{Effort: "high", Enabled: true},
	}); err != nil {
		t.Fatalf("stream error: %v", err)
	}

	reasoning := reasoningOf(t, captured.all(), 0)
	if got := reasoning["summary"]; got != responsesReasoningSummaryAuto {
		t.Errorf("summary = %v, want %q", got, responsesReasoningSummaryAuto)
	}
}

// TestResponsesOmitsSummaryWhenReasoningSuppressed guards against contradicting
// ourselves in one request: an effort of none turns thinking off, so asking for
// a summary of it is meaningless and gpt-5.1's default effort is none.
func TestResponsesOmitsSummaryWhenReasoningSuppressed(t *testing.T) {
	t.Parallel()

	captured := &capturedRequests{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.record(r)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(responsesSSEResponse(`{"type":"response.completed","response":{"id":"r"}}`)))
	}))
	t.Cleanup(srv.Close)

	gen, err := NewOpenAIResponsesGenerator(OpenAIResponsesConfig{
		BaseURL:    srv.URL,
		Model:      "gpt-5.1",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewOpenAIResponsesGenerator: %v", err)
	}

	if err := drain(t, gen, &GenerateRequest{
		Messages:  []Message{{Role: RoleUser, Parts: []Part{{Text: "hi"}}}},
		Reasoning: &ReasoningRequest{Effort: "none", Enabled: true},
	}); err != nil {
		t.Fatalf("stream error: %v", err)
	}

	if got := reasoningOf(t, captured.all(), 0)["summary"]; got != nil {
		t.Errorf("summary = %v, want the field omitted for effort none", got)
	}
}

// TestResponsesDowngradesOnSummaryRejection covers the endpoints that lag the
// upstream schema — Azure deployments and OpenAI-compatible gateways. A rejected
// field must cost one retry, not the turn, and must not be resent afterwards.
func TestResponsesDowngradesOnSummaryRejection(t *testing.T) {
	t.Parallel()

	captured := &capturedRequests{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.record(r)
		if len(captured.all()) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"Unknown parameter: 'reasoning.summary'."}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(responsesSSEResponse(
			`{"type":"response.output_text.delta","delta":"ok"}`,
			`{"type":"response.completed","response":{"id":"r"}}`,
		)))
	}))
	t.Cleanup(srv.Close)

	gen, err := NewOpenAIResponsesGenerator(OpenAIResponsesConfig{
		BaseURL:    srv.URL,
		Model:      "gpt-5-codex",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewOpenAIResponsesGenerator: %v", err)
	}

	req := func(text string) *GenerateRequest {
		return &GenerateRequest{
			Messages:  []Message{{Role: RoleUser, Parts: []Part{{Text: text}}}},
			Reasoning: &ReasoningRequest{Effort: "high", Enabled: true},
		}
	}
	if err := drain(t, gen, req("hi")); err != nil {
		t.Fatalf("rejection was not recovered: %v", err)
	}

	bodies := captured.all()
	if len(bodies) != 2 {
		t.Fatalf("requests = %d, want 2 (original plus one retry)", len(bodies))
	}
	if got := reasoningOf(t, bodies, 0)["summary"]; got != responsesReasoningSummaryAuto {
		t.Errorf("first request summary = %v, want %q", got, responsesReasoningSummaryAuto)
	}
	if got := reasoningOf(t, bodies, 1)["summary"]; got != nil {
		t.Errorf("retry resent the rejected field: summary = %v", got)
	}
	if got := reasoningOf(t, bodies, 1)["effort"]; got != "high" {
		t.Errorf("retry lost the effort pin: effort = %v, want high", got)
	}

	// The latch is per generator, so later turns must not pay for the field again.
	if err := drain(t, gen, req("again")); err != nil {
		t.Fatalf("second turn: %v", err)
	}
	bodies = captured.all()
	if len(bodies) != 3 {
		t.Fatalf("requests after second turn = %d, want 3", len(bodies))
	}
	if got := reasoningOf(t, bodies, 2)["summary"]; got != nil {
		t.Errorf("summary was resent after the latch: %v", got)
	}
}

// TestResponsesUnrelatedBadRequestIsNotRetried pins the blast radius of the
// downgrade: a client error we cannot attribute to reasoning.summary must
// surface as-is on the first attempt rather than burning a silent retry.
func TestResponsesUnrelatedBadRequestIsNotRetried(t *testing.T) {
	t.Parallel()

	captured := &capturedRequests{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.record(r)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"model not found"}}`))
	}))
	t.Cleanup(srv.Close)

	gen, err := NewOpenAIResponsesGenerator(OpenAIResponsesConfig{
		BaseURL:    srv.URL,
		Model:      "gpt-5-codex",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewOpenAIResponsesGenerator: %v", err)
	}

	streamErr := drain(t, gen, &GenerateRequest{
		Messages:  []Message{{Role: RoleUser, Parts: []Part{{Text: "hi"}}}},
		Reasoning: &ReasoningRequest{Effort: "high", Enabled: true},
	})
	if streamErr == nil {
		t.Fatal("expected the 400 to surface")
	}
	if !strings.Contains(streamErr.Error(), "model not found") {
		t.Errorf("error lost the server message: %v", streamErr)
	}
	if got := len(captured.all()); got != 1 {
		t.Errorf("requests = %d, want 1 (no retry for an unrelated 400)", got)
	}
	if gen.summaryUnsupported.Load() {
		t.Error("an unrelated 400 latched summaryUnsupported")
	}
}
