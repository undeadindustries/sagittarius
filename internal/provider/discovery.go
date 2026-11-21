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
	// ContextLimit is the model's context window in tokens, when the provider
	// reports it (OpenRouter context_length, vLLM max_model_len, Gemini
	// inputTokenLimit, Anthropic max_input_tokens). 0 means unknown.
	ContextLimit int
	// MaxOutputTokens is the model's advertised max output token cap (e.g.
	// Anthropic max_tokens). 0 means unknown.
	MaxOutputTokens int
	// ThinkingTypes lists Anthropic's capabilities.thinking.types values when
	// discovered from /v1/models.
	ThinkingTypes []string
	// Reasoning is the model's discovered reasoning capability, populated by
	// OpenRouter's `reasoning` object or Anthropic's `capabilities` object.
	// Nil when the provider doesn't report it.
	Reasoning *ModelReasoningInfo
}

// ModelReasoningInfo captures a model's discovered reasoning capability from
// a provider's models list (see openAIModelReasoning for the OpenRouter wire
// shape this is parsed from).
type ModelReasoningInfo struct {
	SupportedEfforts []string
	DefaultEffort    string
	DefaultEnabled   bool
	Mandatory        bool
}

// DiscoverModels queries GET {baseUrl}/v1/models best-effort.
// All errors are swallowed; callers always receive a slice (possibly empty).
func DiscoverModels(
	ctx context.Context,
	baseURL string,
	bearer string,
	client *http.Client,
) []ModelInfo {
	infos, err := DiscoverModelsWithError(ctx, baseURL, bearer, client)
	if err != nil {
		return nil
	}
	return infos
}

// DiscoverModelsWithError queries GET {baseUrl}/v1/models and returns an
// actionable error on transport failure, non-2xx HTTP status, or bad JSON.
func DiscoverModelsWithError(
	ctx context.Context,
	baseURL string,
	bearer string,
	client *http.Client,
) ([]ModelInfo, error) {
	if client == nil {
		client = &http.Client{Timeout: defaultDiscoveryTimeout}
	}
	modelsURL := ModelsURL(baseURL)

	reqCtx, cancel := context.WithTimeout(ctx, defaultDiscoveryTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create models request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, mapHTTPTransportError(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		preview := readBodyPreview(resp.Body, 512)
		msg := extractHTTPErrorMessage(preview)
		if msg != "" {
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, msg)
		}
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read models response: %w", err)
	}
	var payload openAIModelsResponse
	if err := json.Unmarshal(stripBOM(raw), &payload); err != nil {
		return nil, fmt.Errorf("decode models response: %w", err)
	}

	out := make([]ModelInfo, 0, len(payload.Data))
	for _, entry := range payload.Data {
		id := strings.TrimSpace(entry.ID)
		if id == "" || !isChatModel(id) {
			continue
		}
		limit := entry.ContextLength
		if limit <= 0 {
			limit = entry.MaxModelLen
		}
		out = append(out, ModelInfo{ID: id, ContextLimit: limit, Reasoning: reasoningInfoFromWire(entry.Reasoning)})
	}
	sortModelInfos(out)
	return out, nil
}

// reasoningInfoFromWire converts the OpenRouter wire reasoning object into the
// provider-neutral ModelReasoningInfo, or nil when the provider omitted it.
func reasoningInfoFromWire(w *openAIModelReasoning) *ModelReasoningInfo {
	if w == nil {
		return nil
	}
	return &ModelReasoningInfo{
		SupportedEfforts: append([]string(nil), w.SupportedEfforts...),
		DefaultEffort:    w.DefaultEffort,
		DefaultEnabled:   w.DefaultEnabled,
		Mandatory:        w.Mandatory,
	}
}

// ReasoningInfoForModel returns the discovered reasoning capability for
// modelID from a discovery list, or nil when absent/unknown.
func ReasoningInfoForModel(models []ModelInfo, modelID string) *ModelReasoningInfo {
	modelID = strings.TrimSpace(modelID)
	for _, m := range models {
		if m.ID == modelID {
			return m.Reasoning
		}
	}
	return nil
}

type geminiModelsResponse struct {
	Models        []geminiModelEntry `json:"models"`
	NextPageToken string             `json:"nextPageToken"`
}

type geminiModelEntry struct {
	Name                       string   `json:"name"`
	SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
	InputTokenLimit            int      `json:"inputTokenLimit"`
}

type anthropicModelsResponse struct {
	Data    []anthropicModelEntry `json:"data"`
	HasMore bool                  `json:"has_more"`
	LastID  string                `json:"last_id"`
}

type anthropicModelEntry struct {
	ID             string                      `json:"id"`
	MaxInputTokens int                         `json:"max_input_tokens"`
	MaxTokens      int                         `json:"max_tokens"`
	Capabilities   *anthropicModelCapabilities `json:"capabilities"`
}

type anthropicModelCapabilities struct {
	Effort   *anthropicEffortCap   `json:"effort"`
	Thinking *anthropicThinkingCap `json:"thinking"`
}

type anthropicCapabilitySupport struct {
	Supported bool `json:"supported"`
}

type anthropicEffortCap struct {
	Supported bool                       `json:"supported"`
	Low       anthropicCapabilitySupport `json:"low"`
	Medium    anthropicCapabilitySupport `json:"medium"`
	High      anthropicCapabilitySupport `json:"high"`
	XHigh     anthropicCapabilitySupport `json:"xhigh"`
	Max       anthropicCapabilitySupport `json:"max"`
}

type anthropicThinkingCap struct {
	Supported bool                       `json:"supported"`
	Types     *anthropicThinkingTypesCap `json:"types"`
}

type anthropicThinkingTypesCap struct {
	Adaptive     anthropicCapabilitySupport `json:"adaptive"`
	BetweenTools anthropicCapabilitySupport `json:"between_tools"`
	Disabled     anthropicCapabilitySupport `json:"disabled"`
	Enabled      anthropicCapabilitySupport `json:"enabled"`
}

// DiscoverAnthropicModels paginates GET {serverRoot}/v1/models using the
// Anthropic version and optional workspace headers, returning an error on
// HTTP failure or invalid JSON.
func DiscoverAnthropicModels(
	ctx context.Context,
	baseURL string,
	bearer string,
	workspaceID string,
	client *http.Client,
) ([]ModelInfo, error) {
	if client == nil {
		client = &http.Client{Timeout: defaultDiscoveryTimeout}
	}
	base, err := url.Parse(ModelsURL(baseURL))
	if err != nil {
		return nil, fmt.Errorf("parse Anthropic models URL: %w", err)
	}

	var all []ModelInfo
	afterID := ""
	for {
		u := *base
		q := u.Query()
		q.Set("limit", "1000")
		if afterID != "" {
			q.Set("after_id", afterID)
		} else {
			q.Del("after_id")
		}
		u.RawQuery = q.Encode()

		reqCtx, cancel := context.WithTimeout(ctx, defaultDiscoveryTimeout)
		req, reqErr := http.NewRequestWithContext(reqCtx, http.MethodGet, u.String(), nil)
		if reqErr != nil {
			cancel()
			return nil, fmt.Errorf("create Anthropic models request: %w", reqErr)
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("anthropic-version", AnthropicAPIVersion)
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		if ws := strings.TrimSpace(workspaceID); ws != "" {
			req.Header.Set("anthropic-workspace-id", ws)
		}

		resp, doErr := client.Do(req)
		if doErr != nil {
			cancel()
			return nil, mapHTTPTransportError(doErr)
		}
		raw, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		cancel()
		if readErr != nil {
			return nil, fmt.Errorf("read Anthropic models response: %w", readErr)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			preview := strings.TrimSpace(string(raw))
			if len(preview) > 512 {
				preview = preview[:512]
			}
			msg := extractHTTPErrorMessage(preview)
			if msg != "" {
				return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, msg)
			}
			return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
		}

		var payload anthropicModelsResponse
		if err := json.Unmarshal(stripBOM(raw), &payload); err != nil {
			return nil, fmt.Errorf("decode Anthropic models: %w", err)
		}

		for _, entry := range payload.Data {
			id := strings.TrimSpace(entry.ID)
			if id == "" {
				continue
			}
			info := ModelInfo{
				ID:              id,
				ContextLimit:    entry.MaxInputTokens,
				MaxOutputTokens: entry.MaxTokens,
			}
			if entry.Capabilities != nil {
				thinkingTypes := extractAnthropicThinkingTypes(entry.Capabilities.Thinking)
				efforts := extractAnthropicEfforts(entry.Capabilities.Effort)
				info.ThinkingTypes = thinkingTypes
				if len(efforts) > 0 || len(thinkingTypes) > 0 {
					defaultEnabled := (entry.Capabilities.Thinking != nil && entry.Capabilities.Thinking.Supported) ||
						slices.Contains(thinkingTypes, "adaptive")
					mandatory := len(thinkingTypes) > 0 && !slices.Contains(thinkingTypes, "disabled")
					info.Reasoning = &ModelReasoningInfo{
						SupportedEfforts: efforts,
						DefaultEnabled:   defaultEnabled,
						Mandatory:        mandatory,
					}
				}
			}
			all = append(all, info)
		}

		if !payload.HasMore || payload.LastID == "" || payload.LastID == afterID {
			break
		}
		afterID = payload.LastID
	}

	sortModelInfos(all)
	return all, nil
}

func extractAnthropicEfforts(cap *anthropicEffortCap) []string {
	if cap == nil {
		return nil
	}
	var out []string
	if cap.Low.Supported {
		out = append(out, "low")
	}
	if cap.Medium.Supported {
		out = append(out, "medium")
	}
	if cap.High.Supported {
		out = append(out, "high")
	}
	if cap.XHigh.Supported {
		out = append(out, "xhigh")
	}
	if cap.Max.Supported {
		out = append(out, "max")
	}
	return out
}

func extractAnthropicThinkingTypes(cap *anthropicThinkingCap) []string {
	if cap == nil || cap.Types == nil {
		return nil
	}
	var out []string
	if cap.Types.Adaptive.Supported {
		out = append(out, "adaptive")
	}
	if cap.Types.Enabled.Supported {
		out = append(out, "enabled")
	}
	if cap.Types.Disabled.Supported {
		out = append(out, "disabled")
	}
	if cap.Types.BetweenTools.Supported {
		out = append(out, "between_tools")
	}
	return out
}

// DiscoverGeminiModels lists chat-capable models via the Gemini API models.list
// endpoint (GET /v1beta/models?key=…). The API key is passed as a query
// parameter per Google's REST documentation.
func DiscoverGeminiModels(ctx context.Context, apiKey string, client *http.Client) ([]ModelInfo, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("no Gemini API key configured — set one in /providers first")
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

	// Parse the base URL once so we can Set (not append) pageToken on each
	// iteration. Appending caused duplicate pageToken params from page 3 on.
	base, err := url.Parse(listURL)
	if err != nil {
		return nil, fmt.Errorf("parse Gemini models URL: %w", err)
	}

	var all []ModelInfo
	token := ""
	for {
		// Build the page URL by cloning the base and setting the token cleanly.
		u := *base
		q := u.Query()
		if token != "" {
			q.Set("pageToken", token)
		} else {
			q.Del("pageToken")
		}
		u.RawQuery = q.Encode()
		pageURL := u.String()

		reqCtx, cancel := context.WithTimeout(ctx, defaultDiscoveryTimeout)
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, pageURL, nil)
		if err != nil {
			cancel()
			return nil, err
		}
		req.Header.Set("Accept", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("list Gemini models: %w", err)
		}
		raw, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		cancel()
		if readErr != nil {
			return nil, fmt.Errorf("read Gemini models response: %w", readErr)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("list Gemini models: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
		}

		var payload geminiModelsResponse
		if err := json.Unmarshal(stripBOM(raw), &payload); err != nil {
			return nil, fmt.Errorf("decode Gemini models: %w", err)
		}

		for _, entry := range payload.Models {
			if !geminiSupportsGenerateContent(entry.SupportedGenerationMethods) {
				continue
			}
			id := geminiModelID(entry.Name)
			if id == "" || !strings.HasPrefix(id, "gemini-") {
				continue
			}
			all = append(all, ModelInfo{ID: id, ContextLimit: entry.InputTokenLimit})
		}

		if payload.NextPageToken == "" {
			break
		}
		token = payload.NextPageToken
	}

	if len(all) == 0 {
		return nil, fmt.Errorf("no generateContent models returned by Gemini API")
	}
	sortModelInfos(all)
	return all, nil
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

// staticModelContextLimits covers common OpenAI-direct ids whose /v1/models
// response omits a context field. Keys are matched exact-first, then by the
// ordered prefix list below for versioned ids (e.g. gpt-4o-2024-08-06).
var staticModelContextLimits = map[string]int{
	"gpt-4o":        128_000,
	"gpt-4o-mini":   128_000,
	"gpt-4.1":       1_047_576,
	"gpt-4.1-mini":  1_047_576,
	"gpt-4-turbo":   128_000,
	"gpt-4":         8_192,
	"gpt-3.5-turbo": 16_385,
	"gpt-5":         400_000,
	"gpt-5-codex":   400_000,
	"o1":            200_000,
	"o3":            200_000,
	"o3-mini":       200_000,
	"o4-mini":       200_000,
	// Anthropic Claude (verified against platform.claude.com/docs/en/models/overview)
	"claude-opus-5-5":   1_000_000,
	"claude-sonnet-5-5": 1_000_000,
	"claude-haiku-5-5":  1_000_000,
	"claude-fable-5-2":  1_000_000,
	"claude-fable-5-1":  1_000_000,
	"claude-fable-5":    1_000_000,
	"claude-mythos-5-1": 1_000_000,
	"claude-mythos-5":   1_000_000,
	"claude-opus-5":     1_000_000,
	"claude-sonnet-5":   1_000_000,
	"claude-opus-4-8":   1_000_000,
	"claude-opus-4-7":   1_000_000,
	"claude-opus-4-6":   1_000_000,
	"claude-sonnet-4-6": 1_000_000,
	"claude-opus-4-5":   200_000,
	"claude-sonnet-4-5": 200_000,
	"claude-haiku-4-5":  200_000,
}

// staticContextPrefixes is the deterministic prefix fallback for versioned ids.
// Longer prefixes come first so gpt-4o-mini wins over gpt-4o.
var staticContextPrefixes = []struct {
	prefix string
	limit  int
}{
	{"gpt-4o-mini", 128_000},
	{"gpt-4o", 128_000},
	{"gpt-4.1-mini", 1_047_576},
	{"gpt-4.1", 1_047_576},
	{"gpt-4-turbo", 128_000},
	{"gpt-5", 400_000},
	{"o4-mini", 200_000},
	{"o3-mini", 200_000},
	{"o3", 200_000},
	{"claude-opus-5", 1_000_000},
	{"claude-sonnet-5", 1_000_000},
	{"claude-haiku-5", 1_000_000},
	{"claude-fable-5", 1_000_000},
	{"claude-mythos-5", 1_000_000},
	{"claude-opus-4-8", 1_000_000},
	{"claude-opus-4-7", 1_000_000},
	{"claude-opus-4-6", 1_000_000},
	{"claude-sonnet-4-6", 1_000_000},
	{"claude-opus-4-5", 200_000},
	{"claude-sonnet-4-5", 200_000},
	{"claude-haiku-4-5", 200_000},
}

// StaticContextLimit returns a known context window for an OpenAI-direct model
// id, or 0 when unknown. Provider-prefixed ids ("openai/gpt-4o") are normalized.
func StaticContextLimit(modelID string) int {
	m := strings.ToLower(strings.TrimSpace(modelID))
	if i := strings.LastIndex(m, "/"); i >= 0 {
		m = m[i+1:]
	}
	if v, ok := staticModelContextLimits[m]; ok {
		return v
	}
	for _, p := range staticContextPrefixes {
		if strings.HasPrefix(m, p.prefix) {
			return p.limit
		}
	}
	return 0
}

// ContextLimitForModel returns the context window for modelID: the discovered
// value from the models list when present, otherwise the static OpenAI table.
// 0 means unknown.
func ContextLimitForModel(models []ModelInfo, modelID string) int {
	modelID = strings.TrimSpace(modelID)
	for _, m := range models {
		if m.ID == modelID && m.ContextLimit > 0 {
			return m.ContextLimit
		}
	}
	return StaticContextLimit(modelID)
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
