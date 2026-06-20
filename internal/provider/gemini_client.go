package provider

import (
	"context"
	"iter"

	"google.golang.org/genai"
)

// geminiStreamer abstracts GenerateContentStream for production and tests.
type geminiStreamer interface {
	GenerateContentStream(
		ctx context.Context,
		model string,
		contents []*genai.Content,
		config *genai.GenerateContentConfig,
	) iter.Seq2[*genai.GenerateContentResponse, error]
}

type geminiModels struct {
	models *genai.Models
}

func (m geminiModels) GenerateContentStream(
	ctx context.Context,
	model string,
	contents []*genai.Content,
	config *genai.GenerateContentConfig,
) iter.Seq2[*genai.GenerateContentResponse, error] {
	return m.models.GenerateContentStream(ctx, model, contents, config)
}

// newGeminiStreamerFromClient wraps a genai client for streaming.
func newGeminiStreamerFromClient(client *genai.Client) geminiStreamer {
	return geminiModels{models: client.Models}
}
