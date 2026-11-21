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
	"strconv"
	"strings"
	"time"
)

const (
	maxAnthropicPreStreamRetries = 2
	anthropicRetryBackoffFirst   = 500 * time.Millisecond
	anthropicRetryBackoffSecond  = time.Second
	anthropicMaxRetryAfter       = 10 * time.Second
)

// AnthropicGenerator streams content from the native Anthropic Messages API.
type AnthropicGenerator struct {
	client               *http.Client
	url                  string
	model                string
	timeout              time.Duration
	bearer               string
	workspaceID          string
	systemPromptOverride string
	toolsEnabled         bool
	defaultMaxTokens     int
	thinkingTypes        []string
	supportedEfforts     []string
	providerID           string
	connectTimeout       time.Duration
	firstTokenTimeout    time.Duration
}

// AnthropicConfig holds runtime options for an AnthropicGenerator.
type AnthropicConfig struct {
	BaseURL              string
	Model                string
	Timeout              time.Duration
	Bearer               string
	WorkspaceID          string
	SystemPromptOverride string
	ToolsEnabled         bool
	DefaultMaxTokens     int
	ThinkingTypes        []string
	SupportedEfforts     []string
	HTTPClient           *http.Client
	ProviderID           string
	ConnectTimeout       time.Duration
	FirstTokenTimeout    time.Duration
}

// NewAnthropicGenerator constructs an AnthropicGenerator.
func NewAnthropicGenerator(cfg AnthropicConfig) (*AnthropicGenerator, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("anthropic generator: base url is required")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, fmt.Errorf("anthropic generator: model is required")
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{}
	}
	return &AnthropicGenerator{
		client:               client,
		url:                  MessagesURL(cfg.BaseURL),
		model:                cfg.Model,
		timeout:              cfg.Timeout,
		bearer:               cfg.Bearer,
		workspaceID:          strings.TrimSpace(cfg.WorkspaceID),
		systemPromptOverride: cfg.SystemPromptOverride,
		toolsEnabled:         cfg.ToolsEnabled,
		defaultMaxTokens:     cfg.DefaultMaxTokens,
		thinkingTypes:        append([]string(nil), cfg.ThinkingTypes...),
		supportedEfforts:     append([]string(nil), cfg.SupportedEfforts...),
		providerID:           cfg.ProviderID,
		connectTimeout:       cfg.ConnectTimeout,
		firstTokenTimeout:    cfg.FirstTokenTimeout,
	}, nil
}

// GenerateContentStream implements ContentGenerator.
func (g *AnthropicGenerator) GenerateContentStream(
	ctx context.Context,
	req *GenerateRequest,
) (<-chan StreamResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("generate content stream: request is required")
	}
	if len(req.Messages) == 0 {
		return nil, fmt.Errorf("generate content stream: at least one message is required")
	}

	model := g.model
	if req.Model != "" {
		model = req.Model
	}
	plan, body, err := g.buildRequestPlan(req, model)
	if err != nil {
		return nil, fmt.Errorf("encode anthropic request: %w", err)
	}

	ch := make(chan StreamResponse)
	go func() {
		defer close(ch)
		if err := g.streamWithRetry(ctx, plan, body, ch); err != nil {
			sendOrDone(ctx, ch, StreamResponse{Error: err})
		}
	}()
	return ch, nil
}

// DebugWireRequest implements WireRequestDebugger for /chat debug.
func (g *AnthropicGenerator) DebugWireRequest(req *GenerateRequest) ([]byte, error) {
	if req == nil {
		return nil, fmt.Errorf("debug wire request: request is required")
	}
	model := g.model
	if req.Model != "" {
		model = req.Model
	}
	_, body, err := g.buildRequestPlan(req, model)
	if err != nil {
		return nil, err
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, body, "", "  "); err != nil {
		return body, nil
	}
	return pretty.Bytes(), nil
}

func (g *AnthropicGenerator) buildRequestPlan(req *GenerateRequest, model string) (AnthropicRequestPlan, []byte, error) {
	cloned := *req
	cloned.Model = model
	opts := AnthropicPlanOptions{
		ToolsEnabled:         g.toolsEnabled,
		SystemPromptOverride: g.systemPromptOverride,
	}
	if req.ModelMaxOutputTokens > 0 || len(req.ModelThinkingTypes) > 0 || len(req.ModelSupportedEfforts) > 0 {
		opts.DefaultMaxTokens = req.ModelMaxOutputTokens
		opts.ThinkingTypes = req.ModelThinkingTypes
		opts.SupportedEfforts = req.ModelSupportedEfforts
	} else if model == g.model || g.model == "" {
		opts.DefaultMaxTokens = g.defaultMaxTokens
		opts.ThinkingTypes = g.thinkingTypes
		opts.SupportedEfforts = g.supportedEfforts
	}
	plan := BuildAnthropicRequestPlan(&cloned, opts)
	body, err := json.Marshal(plan.Body)
	return plan, body, err
}

func (g *AnthropicGenerator) streamWithRetry(ctx context.Context, plan AnthropicRequestPlan, body []byte, ch chan<- StreamResponse) error {
	fallbackUsed := false
	for attempt := 0; ; attempt++ {
		beta := ""
		if plan.IncludeBindingBeta {
			beta = AnthropicThinkingBindingBeta
		}
		retryAfter, retryable, thinkingErrBody, err := g.streamOnce(ctx, body, beta, ch)
		if err == nil {
			return nil
		}
		if thinkingErrBody != "" && !fallbackUsed {
			if nextPlan, changed := stripAnthropicThinkingForFallback(plan, thinkingErrBody); changed {
				nextBody, mErr := json.Marshal(nextPlan.Body)
				if mErr == nil {
					plan = nextPlan
					body = nextBody
					fallbackUsed = true
					attempt-- // fallback doesn't consume a 429/529 backoff slot
					continue
				}
			}
		}
		if !retryable || attempt >= maxAnthropicPreStreamRetries {
			return err
		}
		wait := retryAfter
		if wait <= 0 {
			if attempt == 0 {
				wait = anthropicRetryBackoffFirst
			} else {
				wait = anthropicRetryBackoffSecond
			}
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (g *AnthropicGenerator) streamOnce(
	ctx context.Context,
	body []byte,
	betaHeader string,
	ch chan<- StreamResponse,
) (retryAfter time.Duration, retryable bool, thinkingErrBody string, err error) {
	streamCtx := ctx
	if g.timeout > 0 {
		var cancel context.CancelFunc
		streamCtx, cancel = context.WithTimeout(ctx, g.timeout)
		defer cancel()
	}
	guard := newStallGuard(streamCtx, g.providerID, g.connectTimeout, g.firstTokenTimeout)
	defer guard.stop()
	defer func() { err = guard.classify(err) }()
	streamCtx = guard.ctx

	guard.armConnect()
	resp, err := postAnthropicSSE(streamCtx, g.client, g.url, g.bearer, g.workspaceID, betaHeader, body)
	if err != nil {
		return 0, false, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	guard.headersReceived()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		preview := readBodyPreview(resp.Body, 2048)
		mapped := mapHTTPError(resp.StatusCode, preview)
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == 529 {
			return parseRetryAfterHeader(resp.Header.Get("Retry-After")), true, "", mapped
		}
		if isAnthropicThinkingBindingError(resp.StatusCode, preview) {
			return 0, false, preview, mapped
		}
		return 0, false, "", mapped
	}

	contentType := resp.Header.Get("Content-Type")
	if !isSSEContentType(contentType) {
		preview := readBodyPreview(resp.Body, 2048)
		return 0, false, "", fmt.Errorf("provider at %s returned unexpected Content-Type %q (expected text/event-stream). Body preview: %s",
			g.url, contentType, preview)
	}

	state := NewAnthropicSseMapperState()
	emittedVisible := false
	err = scanAnthropicSSE(resp.Body, state, guard.firstOutput, func(chunk StreamResponse) bool {
		if chunk.TextDelta != "" || chunk.ReasoningDelta != "" || len(chunk.ToolCalls) > 0 {
			emittedVisible = true
		}
		return sendOrDone(streamCtx, ch, chunk)
	})
	if err != nil && !emittedVisible && errors.Is(err, ErrProviderOverloaded) {
		// An SSE overloaded_error before any visible output is equivalent to a
		// pre-stream HTTP 529 and is safe to retry cleanly without duplicating
		// deltas to the consumer.
		return parseRetryAfterHeader(resp.Header.Get("Retry-After")), true, "", err
	}
	return 0, false, "", err
}

func postAnthropicSSE(
	ctx context.Context,
	client *http.Client,
	url, bearer, workspaceID, betaHeader string,
	body []byte,
) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create provider request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("anthropic-version", AnthropicAPIVersion)
	if betaHeader != "" {
		req.Header.Set("anthropic-beta", betaHeader)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if workspaceID != "" {
		req.Header.Set("anthropic-workspace-id", workspaceID)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, mapHTTPTransportError(err)
	}
	return resp, nil
}

func parseRetryAfterHeader(raw string) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if secs, err := strconv.Atoi(raw); err == nil && secs > 0 {
		d := time.Duration(secs) * time.Second
		if d > anthropicMaxRetryAfter {
			return anthropicMaxRetryAfter
		}
		return d
	}
	return 0
}

func anthropicEventIsOutput(eventType string) bool {
	switch eventType {
	case "", "ping", "message_start":
		return false
	default:
		return true
	}
}

func scanAnthropicSSE(
	r io.Reader,
	state *AnthropicSseMapperState,
	onOutput func(),
	onChunk func(StreamResponse) bool,
) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") || strings.HasPrefix(line, "event:") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}

		var event AnthropicSseEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			preview := payload
			if len(preview) > 256 {
				preview = preview[:256] + "..."
			}
			return fmt.Errorf("decode anthropic SSE event (%s): %w", preview, err)
		}
		if onOutput != nil && anthropicEventIsOutput(event.Type) {
			onOutput()
		}

		chunks, err := MapAnthropicSseEvent(event, state)
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

// EncodeAnthropicRequestBody exposes the request body for httptest assertions.
func EncodeAnthropicRequestBody(g *AnthropicGenerator, req *GenerateRequest, model string) (map[string]any, error) {
	_, raw, err := g.buildRequestPlan(req, model)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}
