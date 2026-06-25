package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/credentials"
)

// OpenAIChatGenerator streams content from an OpenAI-compatible chat endpoint.
type OpenAIChatGenerator struct {
	client          *http.Client
	url             string
	model           string
	timeout         time.Duration
	bearer          string
	toolCallParsing config.ToolCallParsingMode
	// temperature is the effective default sent when a request does not carry
	// its own. Nil means "send none" (let the server decide).
	temperature *float64
}

// OpenAIChatConfig holds runtime options for an OpenAIChatGenerator.
type OpenAIChatConfig struct {
	BaseURL         string
	Model           string
	Timeout         time.Duration
	Bearer          string
	ToolCallParsing config.ToolCallParsingMode
	Temperature     *float64
	HTTPClient      *http.Client
}

// NewOpenAIChatGenerator constructs an OpenAIChatGenerator.
func NewOpenAIChatGenerator(cfg OpenAIChatConfig) (*OpenAIChatGenerator, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("openai chat generator: base url is required")
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("openai chat generator: model is required")
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{}
	}
	parseMode := cfg.ToolCallParsing
	if parseMode == "" {
		parseMode = config.ToolCallParsingLenient
	}
	if cfg.Bearer != "" {
		slog.Debug("openai chat client configured",
			"bearer", credentials.Redact(cfg.Bearer),
			"model", cfg.Model,
			"url", cfg.BaseURL,
		)
	}
	return &OpenAIChatGenerator{
		client:          client,
		url:             ChatCompletionsURL(cfg.BaseURL),
		model:           cfg.Model,
		timeout:         cfg.Timeout,
		bearer:          cfg.Bearer,
		toolCallParsing: parseMode,
		temperature:     cfg.Temperature,
	}, nil
}

// DebugWireRequest implements WireRequestDebugger: it builds the chat-completions
// request body exactly as GenerateContentStream would and returns it as indented
// JSON for /chat debug.
func (g *OpenAIChatGenerator) DebugWireRequest(req *GenerateRequest) ([]byte, error) {
	if req == nil {
		return nil, fmt.Errorf("debug wire request: request is required")
	}
	model := g.model
	if req.Model != "" {
		model = req.Model
	}
	chatReq := BuildOpenAIChatRequest(req, model, g.toolCallParsing, g.temperature)
	return json.MarshalIndent(chatReq, "", "  ")
}

// GenerateContentStream implements ContentGenerator.
func (g *OpenAIChatGenerator) GenerateContentStream(
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

	chatReq := BuildOpenAIChatRequest(req, model, g.toolCallParsing, g.temperature)
	body, err := encodeChatRequestBody(chatReq)
	if err != nil {
		return nil, fmt.Errorf("encode openai request: %w", err)
	}

	ch := make(chan StreamResponse)
	go func() {
		defer close(ch)
		if err := g.streamOnce(ctx, body, ch); err != nil {
			ch <- StreamResponse{Error: err}
		}
	}()
	return ch, nil
}

func (g *OpenAIChatGenerator) streamOnce(ctx context.Context, body []byte, ch chan<- StreamResponse) error {
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
		return mapOpenAIHTTPError(resp.StatusCode, readBodyPreview(resp.Body, 2048))
	}

	contentType := resp.Header.Get("Content-Type")
	if !isSSEContentType(contentType) {
		preview := readBodyPreview(resp.Body, 2048)
		return fmt.Errorf("provider at %s returned unexpected Content-Type %q (expected text/event-stream). Body preview: %s",
			g.url, contentType, preview)
	}

	needsRetry, parseErr := parseSSEStream(resp.Body, g.toolCallParsing, func(chunk StreamResponse) bool {
		return sendOrDone(streamCtx, ch, chunk)
	})
	if parseErr != nil {
		return parseErr
	}
	if needsRetry {
		return g.retryNonStreaming(streamCtx, body, ch)
	}
	return nil
}

func (g *OpenAIChatGenerator) retryNonStreaming(ctx context.Context, streamBody []byte, ch chan<- StreamResponse) error {
	retryBody, err := cloneRequestBodyForRetry(streamBody)
	if err != nil {
		return err
	}
	resp, err := postSSE(ctx, g.client, g.url, g.bearer, retryBody)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return mapOpenAIHTTPError(resp.StatusCode, readBodyPreview(resp.Body, 2048))
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read non-stream retry body: %w", err)
	}
	chunks, err := decodeNonStreamResponse(stripBOM(raw), g.toolCallParsing)
	if err != nil {
		return fmt.Errorf("decode non-stream retry: %w", err)
	}
	for _, chunk := range chunks {
		if !sendOrDone(ctx, ch, chunk) {
			return ctx.Err()
		}
	}
	return nil
}
