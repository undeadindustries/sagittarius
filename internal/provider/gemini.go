package provider

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"google.golang.org/genai"

	"github.com/undeadindustries/sagittarius/internal/credentials"
)

// modelPartsAccumulator collects the complete model turn across stream chunks.
// Text deltas are concatenated into a single text part; each functionCall part
// is kept distinct with its thoughtSignature so Gemini 3 multi-step tool
// calling can replay signatures verbatim.
type modelPartsAccumulator struct {
	text    strings.Builder
	textSig []byte
	calls   []Part
}

func newModelPartsAccumulator() *modelPartsAccumulator {
	return &modelPartsAccumulator{}
}

func (a *modelPartsAccumulator) add(resp *genai.GenerateContentResponse) {
	if resp == nil || len(resp.Candidates) == 0 {
		return
	}
	cand := resp.Candidates[0]
	if cand == nil || cand.Content == nil {
		return
	}
	for _, p := range cand.Content.Parts {
		if p == nil {
			continue
		}
		switch {
		case p.FunctionCall != nil:
			part := Part{FunctionCall: functionCallFromGenai(p.FunctionCall)}
			if len(p.ThoughtSignature) > 0 {
				part.ThoughtSignature = p.ThoughtSignature
			}
			a.calls = append(a.calls, part)
		case p.Text != "":
			a.text.WriteString(p.Text)
			// Keep the most recent signature seen on a text part; for non-tool
			// responses Gemini attaches it to the final text part.
			if len(p.ThoughtSignature) > 0 {
				a.textSig = p.ThoughtSignature
			}
		}
	}
}

// parts returns the assembled model parts: the concatenated text part (if any)
// first, followed by the functionCall parts in arrival order. Returns nil when
// nothing was accumulated so callers can detect an empty turn.
func (a *modelPartsAccumulator) parts() []Part {
	var out []Part
	if text := a.text.String(); text != "" {
		p := Part{Text: text}
		if len(a.textSig) > 0 {
			p.ThoughtSignature = a.textSig
		}
		out = append(out, p)
	}
	out = append(out, a.calls...)
	return out
}

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
		var lastUsageMeta *genai.GenerateContentResponseUsageMetadata
		acc := newModelPartsAccumulator()
		for resp, err := range stream {
			if err != nil {
				ch <- StreamResponse{Error: MapAPIError(err)}
				return
			}
			if resp == nil {
				continue
			}

			if resp.UsageMetadata != nil {
				lastUsageMeta = resp.UsageMetadata
			}

			// Accumulate the full model turn (text + functionCall parts with
			// their thoughtSignatures) so the runner can store it verbatim for
			// Gemini 3 multi-step tool calling.
			acc.add(resp)

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
		// Emit provider-reported usage before Done (cost is not available from
		// the Gemini API natively so CostKnown remains false). The complete
		// model parts ride on the same chunk so the runner records the turn
		// with its thought signatures intact.
		final := StreamResponse{ModelParts: acc.parts()}
		if lastUsageMeta != nil {
			final.Usage = &Usage{
				InputTokens:  int(lastUsageMeta.PromptTokenCount),
				OutputTokens: int(lastUsageMeta.CandidatesTokenCount),
			}
		}
		if final.Usage != nil || len(final.ModelParts) > 0 {
			ch <- final
		}
		ch <- StreamResponse{Done: true}
	}()

	return ch, nil
}
