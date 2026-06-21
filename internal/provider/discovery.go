package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

const defaultDiscoveryTimeout = 10 * time.Second

// geminiModelsAPIRoot is the Google AI Gemini API base used for models.list.
// See https://ai.google.dev/api/models — GET /v1beta/models
const geminiModelsAPIRoot = "https://generativelanguage.googleapis.com"

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
	sortModelInfos(out)
	return out
}

type geminiModelsResponse struct {
	Models        []geminiModelEntry `json:"models"`
	NextPageToken string             `json:"nextPageToken"`
}

type geminiModelEntry struct {
	Name                       string   `json:"name"`
	SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
}

// DiscoverGeminiModels lists chat-capable models via the Gemini API models.list
// endpoint (GET /v1beta/models?key=…). The API key is passed as a query
// parameter per Google's REST documentation.
func DiscoverGeminiModels(ctx context.Context, apiKey string, client *http.Client) ([]ModelInfo, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("Gemini API key required to list models — set one in /providers first")
	}
	if client == nil {
		client = &http.Client{Timeout: defaultDiscoveryTimeout}
	}

	u := geminiModelsAPIRoot + "/v1beta/models?key=" + url.QueryEscape(apiKey)
	return fetchGeminiModels(ctx, u, client)
}

func fetchGeminiModels(ctx context.Context, listURL string, client *http.Client) ([]ModelInfo, error) {
	if client == nil {
		client = &http.Client{Timeout: defaultDiscoveryTimeout}
	}

	reqCtx, cancel := context.WithTimeout(ctx, defaultDiscoveryTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, listURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list Gemini models: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read Gemini models response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("list Gemini models: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var payload geminiModelsResponse
	if err := json.Unmarshal(stripBOM(raw), &payload); err != nil {
		return nil, fmt.Errorf("decode Gemini models: %w", err)
	}

	out := make([]ModelInfo, 0, len(payload.Models))
	for _, entry := range payload.Models {
		if !geminiSupportsGenerateContent(entry.SupportedGenerationMethods) {
			continue
		}
		id := geminiModelID(entry.Name)
		if id == "" {
			continue
		}
		out = append(out, ModelInfo{ID: id})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no generateContent models returned by Gemini API")
	}
	sortModelInfos(out)
	return out, nil
}

// SortModelIDs sorts model ids lexicographically in place.
func SortModelIDs(ids []string) {
	slices.Sort(ids)
}

func sortModelInfos(infos []ModelInfo) {
	slices.SortFunc(infos, func(a, b ModelInfo) int {
		return strings.Compare(a.ID, b.ID)
	})
}

func geminiSupportsGenerateContent(methods []string) bool {
	for _, m := range methods {
		if m == "generateContent" {
			return true
		}
	}
	return false
}

func geminiModelID(name string) string {
	name = strings.TrimSpace(name)
	name = strings.TrimPrefix(name, "models/")
	return name
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
