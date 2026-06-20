package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultDiscoveryTimeout = 10 * time.Second

// ModelInfo describes one model returned from GET /v1/models.
type ModelInfo struct {
	ID string
}

// DiscoverModels queries GET {baseUrl}/v1/models best-effort.
// All errors are swallowed; callers always receive a slice (possibly empty).
func DiscoverModels(
	ctx context.Context,
	baseURL string,
	bearer string,
	client *http.Client,
) []ModelInfo {
	if client == nil {
		client = &http.Client{Timeout: defaultDiscoveryTimeout}
	}
	url := ModelsURL(baseURL)

	reqCtx, cancel := context.WithTimeout(ctx, defaultDiscoveryTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Accept", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}
	var payload openAIModelsResponse
	if err := json.Unmarshal(stripBOM(raw), &payload); err != nil {
		return nil
	}

	out := make([]ModelInfo, 0, len(payload.Data))
	for _, entry := range payload.Data {
		id := strings.TrimSpace(entry.ID)
		if id == "" || !isChatModel(id) {
			continue
		}
		out = append(out, ModelInfo{ID: id})
	}
	return out
}

// ModelsURL returns the discovery URL for a provider base URL.
func ModelsURL(baseURL string) string {
	return ExtractServerRoot(baseURL) + "/v1/models"
}

func isChatModel(id string) bool {
	lower := strings.ToLower(id)
	patterns := []string{
		"dall-e", "whisper", "tts", "text-embedding", "embedding",
		"text-moderation", "text-search", "text-similarity",
		"text-davinci-edit", "code-search", "code-cushman",
		"babbage-002", "davinci-002",
	}
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return false
		}
	}
	return !strings.HasPrefix(lower, "tts")
}
