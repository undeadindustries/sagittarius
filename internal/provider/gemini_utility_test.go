package provider

import (
	"context"
	"testing"

	"google.golang.org/genai"
)

func TestGeminiUtilityClientConfigsOmitTemperatureAndThinking(t *testing.T) {
	t.Parallel()

	dummyResp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{
				Content: &genai.Content{
					Parts: []*genai.Part{genai.NewPartFromText("dummy result")},
				},
			},
		},
	}

	t.Run("Search omits temperature and thinkingConfig", func(t *testing.T) {
		t.Parallel()
		var capturedCfg *genai.GenerateContentConfig
		client := &GeminiUtilityClient{
			model: "gemini-3.8-flash",
			generateContentOverride: func(ctx context.Context, model string, contents []*genai.Content, cfg *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
				capturedCfg = cfg
				return dummyResp, nil
			},
		}

		_, _, err := client.Search(context.Background(), "test query")
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if capturedCfg == nil {
			t.Fatal("capturedCfg is nil")
		}
		if capturedCfg.Temperature != nil {
			t.Errorf("Search Temperature = %v, want nil", *capturedCfg.Temperature)
		}
		if capturedCfg.ThinkingConfig != nil {
			t.Errorf("Search ThinkingConfig = %+v, want nil", capturedCfg.ThinkingConfig)
		}
	})

	t.Run("FetchURLContext omits temperature and thinkingConfig", func(t *testing.T) {
		t.Parallel()
		var capturedCfg *genai.GenerateContentConfig
		client := &GeminiUtilityClient{
			model: "gemini-3.8-flash",
			generateContentOverride: func(ctx context.Context, model string, contents []*genai.Content, cfg *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
				capturedCfg = cfg
				return dummyResp, nil
			},
		}

		_, _, err := client.FetchURLContext(context.Background(), "https://example.com")
		if err != nil {
			t.Fatalf("FetchURLContext failed: %v", err)
		}
		if capturedCfg == nil {
			t.Fatal("capturedCfg is nil")
		}
		if capturedCfg.Temperature != nil {
			t.Errorf("FetchURLContext Temperature = %v, want nil", *capturedCfg.Temperature)
		}
		if capturedCfg.ThinkingConfig != nil {
			t.Errorf("FetchURLContext ThinkingConfig = %+v, want nil", capturedCfg.ThinkingConfig)
		}
	})

	t.Run("Summarize omits temperature and thinkingConfig", func(t *testing.T) {
		t.Parallel()
		var capturedCfg *genai.GenerateContentConfig
		client := &GeminiUtilityClient{
			model: "gemini-3.8-flash",
			generateContentOverride: func(ctx context.Context, model string, contents []*genai.Content, cfg *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
				capturedCfg = cfg
				return dummyResp, nil
			},
		}

		_, err := client.Summarize(context.Background(), "sample content")
		if err != nil {
			t.Fatalf("Summarize failed: %v", err)
		}
		if capturedCfg == nil {
			t.Fatal("capturedCfg is nil")
		}
		if capturedCfg.Temperature != nil {
			t.Errorf("Summarize Temperature = %v, want nil", *capturedCfg.Temperature)
		}
		if capturedCfg.ThinkingConfig != nil {
			t.Errorf("Summarize ThinkingConfig = %+v, want nil", capturedCfg.ThinkingConfig)
		}
	})
}
