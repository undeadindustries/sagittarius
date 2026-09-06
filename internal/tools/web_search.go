package tools

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/credentials"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/web"
)

// newGoogleWebSearchTool implements the google_web_search tool using a cascade:
// Gemini native grounding first, then the Brave Search API when a key is
// configured, falling back to DuckDuckGo HTML search.
func newGoogleWebSearchTool(client *provider.GeminiUtilityClient) *webSearchTool {
	return &webSearchTool{utilityClient: client}
}

type webSearchTool struct {
	utilityClient *provider.GeminiUtilityClient
}

func (w *webSearchTool) Name() string {
	return GoogleWebSearchToolName
}

func (w *webSearchTool) Description() string {
	return "Search the web for up-to-date information on any topic."
}

func (w *webSearchTool) RequiresConfirmation() bool {
	return false // read-only
}

func (w *webSearchTool) Declaration() provider.ToolDeclaration {
	return provider.ToolDeclaration{
		Name:        w.Name(),
		Description: w.Description(),
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				ParamQuery: map[string]interface{}{
					"type":        "string",
					"description": "The search query. Be specific to get the best results.",
				},
			},
			"required": []string{ParamQuery},
		},
	}
}

// resolveBraveAPIKey reads BRAVE_API_KEY, then the secure store written by
// /settings. A lookup failure is reported so the caller can log it before
// falling through to the key-free backend.
func resolveBraveAPIKey(ctx context.Context) (string, error) {
	return credentials.ResolveBraveAPIKey(ctx)
}

func (w *webSearchTool) Execute(ctx context.Context, args map[string]interface{}) (map[string]interface{}, error) {
	query, ok := args[ParamQuery].(string)
	if !ok || strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("%s requires a non-empty string parameter %q", w.Name(), ParamQuery)
	}
	query = strings.TrimSpace(query)

	// 1. Gemini native grounding backend (best quality with citations)
	if w.utilityClient != nil {
		text, meta, err := w.utilityClient.Search(ctx, query)
		if err != nil {
			return map[string]interface{}{
				"results": fmt.Sprintf("Error performing web search: %v", err),
			}, nil
		}
		formatted := web.FormatSearchResult(query, text, meta)
		return map[string]interface{}{
			"results": formatted,
		}, nil
	}

	// 2. Brave Search API (BRAVE_API_KEY, or a key stored via /settings)
	braveKey, err := resolveBraveAPIKey(ctx)
	if err != nil {
		slog.Debug("web search: brave key lookup failed", "error", err)
	}
	if braveKey != "" {
		hits, err := web.SearchBrave(ctx, query, braveKey)
		if err == nil {
			return map[string]interface{}{
				"results": web.FormatOrganicResults(query, "Brave Search", hits),
			}, nil
		}
		slog.Debug("web search: brave search failed, falling back to duckduckgo", "error", err)
	}

	// 3. DuckDuckGo HTML fallback
	hits, err := web.SearchDuckDuckGo(ctx, query)
	if err != nil {
		return map[string]interface{}{
			"results": fmt.Sprintf("Error performing web search: %v", err),
		}, nil
	}

	return map[string]interface{}{
		"results": web.FormatOrganicResults(query, "DuckDuckGo", hits),
	}, nil
}
