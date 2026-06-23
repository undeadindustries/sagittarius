package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/credentials"
	"google.golang.org/genai"
)

// ErrGeminiRequired is returned when a Gemini-native web tool is invoked but no
// Gemini API key is configured.
var ErrGeminiRequired = errors.New("a Gemini API key is required to use this capability")

// GeminiUtilityClient is a dedicated, non-streaming client for internal web tools
// (google_web_search and web_fetch) that rely on Gemini-native grounding features
// (GoogleSearch and URLContext). It bypasses the main agent loop's Generator and
// constructs its own connection so web tools can use Gemini grounding even when
// the user's active chat provider is OpenRouter or OpenAI.
type GeminiUtilityClient struct {
	client *genai.Client
	model  string
}

// NewGeminiUtilityClient constructs a utility client using the globally resolved
// Gemini API key. If no key is found, it returns ErrGeminiRequired. model is the
// target model for utility tasks (e.g. "gemini-3.0-flash").
func NewGeminiUtilityClient(ctx context.Context, model string) (*GeminiUtilityClient, error) {
	apiKey, err := resolveAPIKey(ctx, string(config.BuiltInGeminiAPIKey))
	if err != nil {
		if errors.Is(err, credentials.ErrAPIKeyMissing) {
			return nil, ErrGeminiRequired
		}
		return nil, fmt.Errorf("resolve gemini utility key: %w", err)
	}

	if model == "" {
		model = "gemini-2.5-flash" // safe default, can be overridden by caller
	}

	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		Backend: genai.BackendGeminiAPI,
		APIKey:  apiKey,
	})
	if err != nil {
		return nil, fmt.Errorf("create gemini utility client: %w", err)
	}

	return &GeminiUtilityClient{
		client: client,
		model:  model,
	}, nil
}

// Search calls GenerateContent with the GoogleSearch tool enabled and returns the
// synthesized response plus its grounding metadata (citations).
func (c *GeminiUtilityClient) Search(ctx context.Context, query string) (string, *genai.GroundingMetadata, error) {
	temp := float32(0.0)
	cfg := &genai.GenerateContentConfig{
		Temperature: &temp,
		Tools:       []*genai.Tool{{GoogleSearch: &genai.GoogleSearch{}}},
	}
	resp, err := c.client.Models.GenerateContent(ctx, c.model, []*genai.Content{
		genai.NewContentFromText(query, genai.RoleUser),
	}, cfg)
	if err != nil {
		return "", nil, fmt.Errorf("gemini utility search: %w", err)
	}
	if len(resp.Candidates) == 0 {
		return "", nil, nil
	}
	return resp.Text(), resp.Candidates[0].GroundingMetadata, nil
}

// FetchURLContext calls GenerateContent with the URLContext tool enabled, allowing
// Gemini to fetch and summarize authorized URLs embedded in the prompt.
func (c *GeminiUtilityClient) FetchURLContext(ctx context.Context, prompt string) (string, *genai.GroundingMetadata, error) {
	temp := float32(0.0)
	cfg := &genai.GenerateContentConfig{
		Temperature: &temp,
		Tools:       []*genai.Tool{{URLContext: &genai.URLContext{}}},
	}
	resp, err := c.client.Models.GenerateContent(ctx, c.model, []*genai.Content{
		genai.NewContentFromText(prompt, genai.RoleUser),
	}, cfg)
	if err != nil {
		return "", nil, fmt.Errorf("gemini utility fetch: %w", err)
	}
	if len(resp.Candidates) == 0 {
		return "", nil, nil
	}
	return resp.Text(), resp.Candidates[0].GroundingMetadata, nil
}

// Summarize is a pure LLM call used by the HTTP fallback fetch path to summarize
// extracted HTML text.
func (c *GeminiUtilityClient) Summarize(ctx context.Context, prompt string) (string, error) {
	temp := float32(0.0)
	cfg := &genai.GenerateContentConfig{
		Temperature: &temp,
	}
	resp, err := c.client.Models.GenerateContent(ctx, c.model, []*genai.Content{
		genai.NewContentFromText(prompt, genai.RoleUser),
	}, cfg)
	if err != nil {
		return "", fmt.Errorf("gemini utility summarize: %w", err)
	}
	if len(resp.Candidates) == 0 {
		return "", nil
	}
	return resp.Text(), nil
}
