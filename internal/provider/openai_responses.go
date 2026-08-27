package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// OpenAIResponsesGenerator streams content from the OpenAI Responses API.
type OpenAIResponsesGenerator struct {
	client               *http.Client
	url                  string
	model                string
	timeout              time.Duration
	bearer               string
	reasoningEffort      string
	useResponseChaining  bool
	temperature          *float64
	systemPromptOverride string
	toolsEnabled         bool
	// chainMu guards lastResponseID, the Responses API previous_response_id used
	// for response chaining. It is per-generator (not a process global) so two
	// logical sessions — or a provider switch that builds a fresh generator —
	// never chain off each other's response id. GeneratorCache keys instances by
	// connection params, so this id stays correctly scoped across mode switches
	// that return to the same provider/model.
	chainMu        sync.Mutex
	lastResponseID string
	// summaryUnsupported latches when an endpoint rejects reasoning.summary.
	// Azure deployments and OpenAI-compatible gateways lag the upstream schema,
	// and a rejected field would otherwise fail every subsequent turn. Atomic
	// rather than chainMu-guarded: it is read while building each request body,
	// which happens outside that lock.
	summaryUnsupported atomic.Bool
}

// OpenAIResponsesConfig holds runtime options for an OpenAIResponsesGenerator.
type OpenAIResponsesConfig struct {
	BaseURL              string
	Model                string
	Timeout              time.Duration
	Bearer               string
	ReasoningEffort      string
	UseResponseChaining  bool
	Temperature          *float64
	SystemPromptOverride string
	ToolsEnabled         bool
	HTTPClient           *http.Client
}

// NewOpenAIResponsesGenerator constructs an OpenAIResponsesGenerator.
func NewOpenAIResponsesGenerator(cfg OpenAIResponsesConfig) (*OpenAIResponsesGenerator, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("openai responses generator: base url is required")
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("openai responses generator: model is required")
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{}
	}
	return &OpenAIResponsesGenerator{
		client:               client,
		url:                  ResponsesURL(cfg.BaseURL),
		model:                cfg.Model,
		timeout:              cfg.Timeout,
		bearer:               cfg.Bearer,
		reasoningEffort:      cfg.ReasoningEffort,
		useResponseChaining:  cfg.UseResponseChaining,
		temperature:          cfg.Temperature,
		systemPromptOverride: cfg.SystemPromptOverride,
		toolsEnabled:         cfg.ToolsEnabled,
	}, nil
}

// GenerateContentStream implements ContentGenerator.
func (g *OpenAIResponsesGenerator) GenerateContentStream(
	ctx context.Context,
	req *GenerateRequest,
) (<-chan StreamResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("generate content stream: request is required")
	}
	if len(req.Messages) == 0 {
		return nil, fmt.Errorf("generate content stream: at least one message is required")
	}
	if err := g.assertModelOrLocalhost(); err != nil {
		return nil, err
	}

	model := g.model
	if req.Model != "" {
		model = req.Model
	}

	body, err := g.buildRequestBody(req, model, true)
	if err != nil {
		return nil, fmt.Errorf("encode responses request: %w", err)
	}

	ch := make(chan StreamResponse)
	go func() {
		defer close(ch)
		err := g.streamOnce(ctx, body, ch)
		if err = g.retryWithoutSummary(ctx, req, model, err, ch); err != nil {
			if g.useResponseChaining {
				g.setLastResponseID("")
			}
			sendOrDone(ctx, ch, StreamResponse{Error: err})
		}
	}()
	return ch, nil
}

// retryWithoutSummary reissues a request that the endpoint rejected solely for
// carrying reasoning.summary, returning the outcome of that second attempt. Any
// other error passes through untouched.
//
// This is safe to retry because the rejection is an HTTP status read before the
// SSE body, so no chunk has reached the consumer and nothing is duplicated. Only
// one retry is possible: the rebuilt body omits the field that was rejected.
func (g *OpenAIResponsesGenerator) retryWithoutSummary(
	ctx context.Context,
	req *GenerateRequest,
	model string,
	err error,
	ch chan<- StreamResponse,
) error {
	var rejected *reasoningSummaryRejectedError
	if !errors.As(err, &rejected) {
		return err
	}
	g.summaryUnsupported.Store(true)
	body, buildErr := g.buildRequestBody(req, model, true)
	if buildErr != nil {
		return err // report the server's rejection, not a secondary encode failure
	}
	return g.streamOnce(ctx, body, ch)
}

// setLastResponseID stores the trailing Responses API response id for chaining.
func (g *OpenAIResponsesGenerator) setLastResponseID(id string) {
	g.chainMu.Lock()
	g.lastResponseID = strings.TrimSpace(id)
	g.chainMu.Unlock()
}

// lastID returns the stored response id for chaining the next request.
func (g *OpenAIResponsesGenerator) lastID() string {
	g.chainMu.Lock()
	defer g.chainMu.Unlock()
	return g.lastResponseID
}

func (g *OpenAIResponsesGenerator) streamOnce(ctx context.Context, body []byte, ch chan<- StreamResponse) error {
	streamCtx := ctx
	if g.timeout > 0 {
		var cancel context.CancelFunc
		streamCtx, cancel = context.WithTimeout(ctx, g.timeout)
		defer cancel()
	}

	resp, err := postSSE(streamCtx, g.client, g.url, g.bearer, body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		preview := readBodyPreview(resp.Body, 2048)
		mapped := mapOpenAIHTTPError(resp.StatusCode, preview)
		if g.rejectedSummary(resp.StatusCode, preview) {
			return &reasoningSummaryRejectedError{err: mapped}
		}
		return mapped
	}

	contentType := resp.Header.Get("Content-Type")
	if !isSSEContentType(contentType) {
		preview := readBodyPreview(resp.Body, 2048)
		return fmt.Errorf("provider at %s returned unexpected Content-Type %q (expected text/event-stream). Body preview: %s",
			g.url, contentType, preview)
	}

	state := NewResponsesSseMapperState()
	err = scanResponsesSSE(resp.Body, state, func(chunk StreamResponse) bool {
		return sendOrDone(streamCtx, ch, chunk)
	})
	if err != nil {
		return err
	}
	if state.Completed && state.ResponseID != "" && g.useResponseChaining {
		g.setLastResponseID(state.ResponseID)
	}
	return nil
}

// reasoningSummaryRejectedError marks a pre-stream client error the server
// attributed to the reasoning.summary field, so the caller can drop that field
// and retry instead of failing the turn.
type reasoningSummaryRejectedError struct{ err error }

func (e *reasoningSummaryRejectedError) Error() string { return e.err.Error() }
func (e *reasoningSummaryRejectedError) Unwrap() error { return e.err }

// rejectedSummary reports whether a non-2xx response blames reasoning.summary.
// It answers false once the field has been dropped, so an unrelated 4xx that
// merely mentions the word cannot be mistaken for a second rejection.
func (g *OpenAIResponsesGenerator) rejectedSummary(status int, body string) bool {
	if status < 400 || status >= 500 || g.summaryUnsupported.Load() {
		return false
	}
	return strings.Contains(strings.ToLower(body), "summary")
}

// DebugWireRequest implements WireRequestDebugger: it builds the Responses API
// request body exactly as GenerateContentStream would and returns it as indented
// JSON for /chat debug.
func (g *OpenAIResponsesGenerator) DebugWireRequest(req *GenerateRequest) ([]byte, error) {
	if req == nil {
		return nil, fmt.Errorf("debug wire request: request is required")
	}
	model := g.model
	if req.Model != "" {
		model = req.Model
	}
	body, err := g.buildRequestBody(req, model, true)
	if err != nil {
		return nil, err
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, body, "", "  "); err != nil {
		return body, nil // fall back to compact body rather than failing debug
	}
	return pretty.Bytes(), nil
}

func (g *OpenAIResponsesGenerator) buildRequestBody(req *GenerateRequest, model string, stream bool) ([]byte, error) {
	plan := BuildResponsesRequestPlan(req, g.toolsEnabled)
	input := plan.Input
	previousID := ""
	if g.useResponseChaining {
		previousID = g.lastID()
		if previousID != "" {
			input = TrimInputForChaining(plan.Input)
		}
	}

	instructions := plan.Instructions
	if g.systemPromptOverride != "" {
		instructions = g.systemPromptOverride
	}

	body := responsesRequestBody{
		Model:       model,
		Input:       input,
		Stream:      stream,
		Tools:       plan.Tools,
		Temperature: g.temperature,
	}
	if instructions != "" {
		body.Instructions = instructions
	}
	// req.Reasoning (resolved fresh per round by Runner.buildGenerateRequest
	// via config.ResolveReasoningRequest) wins over the generator's
	// construction-time effort, which remains the fallback for callers that
	// build a GenerateRequest directly (DebugWireRequest, tests) without
	// going through the Runner.
	effort := strings.TrimSpace(g.reasoningEffort)
	if req.Reasoning != nil && req.Reasoning.Effort != "" {
		effort = req.Reasoning.Effort
	}
	if effort != "" {
		body.Reasoning = &responsesReasoning{Effort: effort}
		// Ask for the reasoning summary unless this effort suppresses thinking
		// outright, or an earlier round proved the endpoint rejects the field.
		if !effortSuppressesReasoning(effort) && !g.summaryUnsupported.Load() {
			body.Reasoning.Summary = responsesReasoningSummaryAuto
		}
	}
	if g.useResponseChaining && previousID != "" {
		body.PreviousResponseID = previousID
	}
	return json.Marshal(body)
}

func (g *OpenAIResponsesGenerator) assertModelOrLocalhost() error {
	if g.model != "local-model" {
		return nil
	}
	u, err := http.NewRequest(http.MethodGet, g.url, nil)
	if err != nil {
		return nil
	}
	host := strings.ToLower(u.URL.Hostname())
	host = strings.Trim(host, "[]")
	if isLocalResponsesHost(host) {
		return nil
	}
	return fmt.Errorf(
		"no model configured for Responses API provider at %s. Set a model with /model <name> or /provider set <id> model <name>",
		g.url,
	)
}

func isLocalResponsesHost(host string) bool {
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}
	if strings.HasPrefix(host, "10.") || strings.HasPrefix(host, "192.168.") {
		return true
	}
	if strings.HasSuffix(host, ".local") {
		return true
	}
	parts := strings.Split(host, ".")
	if len(parts) == 4 && parts[0] == "172" {
		second := 0
		_, _ = fmt.Sscanf(parts[1], "%d", &second)
		if second >= 16 && second <= 31 {
			return true
		}
	}
	return false
}

func scanResponsesSSE(
	r io.Reader,
	state *ResponsesSseMapperState,
	onChunk func(StreamResponse) bool,
) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if line == "data: [DONE]" {
			return nil
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		var event ResponsesSseEvent
		if err := json.Unmarshal([]byte(line[6:]), &event); err != nil {
			continue
		}

		chunks, err := MapResponsesSseEvent(event, state)
		if err != nil {
			return err
		}
		for _, chunk := range chunks {
			if !onChunk(chunk) {
				return nil
			}
		}
	}
	return scanner.Err()
}

// EncodeResponsesRequestBody exposes the request body for httptest assertions.
func EncodeResponsesRequestBody(g *OpenAIResponsesGenerator, req *GenerateRequest, model string) (map[string]any, error) {
	raw, err := g.buildRequestBody(req, model, true)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}
