package provider

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"google.golang.org/genai"

	"github.com/undeadindustries/sagittarius/internal/credentials"
)

// GeminiGenerator streams content from the Gemini API (BackendGeminiAPI).
type GeminiGenerator struct {
	streamer geminiStreamer
	model    string
	timeout  time.Duration
}

// GeminiConfig holds runtime options for a GeminiGenerator.
type GeminiConfig struct {
	APIKey   string
	Model    string
	Timeout  time.Duration
	Streamer geminiStreamer
}

// NewGeminiGenerator constructs a GeminiGenerator backed by the official genai SDK.
func NewGeminiGenerator(ctx context.Context, cfg GeminiConfig) (*GeminiGenerator, error) {
	if cfg.Streamer == nil {
		if cfg.APIKey == "" {
			return nil, fmt.Errorf("gemini generator: api key is required")
		}
		client, err := genai.NewClient(ctx, &genai.ClientConfig{
			Backend: genai.BackendGeminiAPI,
			APIKey:  cfg.APIKey,
		})
		if err != nil {
			return nil, fmt.Errorf("create gemini client: %w", err)
		}
		cfg.Streamer = newGeminiStreamerFromClient(client)
		slog.Debug("gemini client created", "api_key", credentials.Redact(cfg.APIKey), "model", cfg.Model)
	}

	if cfg.Model == "" {
		return nil, fmt.Errorf("gemini generator: model is required")
	}

	return &GeminiGenerator{
		streamer: cfg.Streamer,
		model:    cfg.Model,
		timeout:  cfg.Timeout,
	}, nil
}

// GenerateContentStream implements ContentGenerator.
func (g *GeminiGenerator) GenerateContentStream(
	ctx context.Context,
	req *GenerateRequest,
) (<-chan StreamResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("generate content stream: request is required")
	}

	model := g.model
	if req.Model != "" {
		model = req.Model
	}

	contents := MessagesToGenaiContents(req.Messages)
	if len(contents) == 0 {
		return nil, fmt.Errorf("generate content stream: at least one message is required")
	}

	cfg := BuildGenerateContentConfig(req)
	ch := make(chan StreamResponse)

	go func() {
		defer close(ch)

		streamCtx := ctx
		if g.timeout > 0 {
			var cancel context.CancelFunc
			streamCtx, cancel = context.WithTimeout(ctx, g.timeout)
			defer cancel()
		}

		stream := g.streamer.GenerateContentStream(streamCtx, model, contents, cfg)
		for resp, err := range stream {
			if err != nil {
				ch <- StreamResponse{Error: MapAPIError(err)}
				return
			}
			if resp == nil {
				continue
			}

			chunk := StreamResponse{}
			if text := resp.Text(); text != "" {
				chunk.TextDelta = text
			}
			if calls := ToolCallsFromGenaiResponse(resp); len(calls) > 0 {
				chunk.ToolCalls = calls
			}
			if chunk.TextDelta != "" || len(chunk.ToolCalls) > 0 {
				ch <- chunk
			}
		}
		ch <- StreamResponse{Done: true}
	}()

	return ch, nil
}
