package provider

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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
		if err := g.streamOnce(ctx, body, ch); err != nil {
			if g.useResponseChaining {
				ClearLastResponseID()
			}
			ch <- StreamResponse{Error: err}
		}
	}()
	return ch, nil
}

func (g *OpenAIResponsesGenerator) streamOnce(ctx context.Context, body []byte, ch chan<- StreamResponse) error {
	streamCtx := ctx
	if g.timeout > 0 {
		var cancel context.CancelFunc
		streamCtx, cancel = context.WithTimeout(ctx, g.timeout)
		defer cancel()
	}

	resp, err := g.doRequest(streamCtx, body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return mapOpenAIHTTPError(resp.StatusCode, readBodyPreview(resp.Body, 2048))
	}

	contentType := resp.Header.Get("Content-Type")
	if !isSSEContentType(contentType) {
		preview := readBodyPreview(resp.Body, 2048)
		return fmt.Errorf("provider at %s returned unexpected Content-Type %q (expected text/event-stream). Body preview: %s",
			g.url, contentType, preview)
	}

	state := NewResponsesSseMapperState()
	err = scanResponsesSSE(resp.Body, state, func(chunk StreamResponse) bool {
		select {
		case <-streamCtx.Done():
			return false
		case ch <- chunk:
			return true
		}
	})
	if err != nil {
		return err
	}
	if state.Completed && state.ResponseID != "" && g.useResponseChaining {
		SetLastResponseID(state.ResponseID)
	}
	return nil
}

func (g *OpenAIResponsesGenerator) buildRequestBody(req *GenerateRequest, model string, stream bool) ([]byte, error) {
	plan := BuildResponsesRequestPlan(req, g.toolsEnabled)
	input := plan.Input
	previousID := ""
	if g.useResponseChaining {
		previousID = LastResponseID()
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
	if effort := ResolveReasoningEffort(g.reasoningEffort); effort != "" {
		body.Reasoning = &responsesReasoning{Effort: effort}
	}
	if g.useResponseChaining && previousID != "" {
		body.PreviousResponseID = previousID
	}
	return json.Marshal(body)
}

func (g *OpenAIResponsesGenerator) doRequest(ctx context.Context, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.url, strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("create responses request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if g.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+g.bearer)
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, mapOpenAITransportError(err)
	}
	return resp, nil
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
