package tools

import (
	"context"
	"fmt"

	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/web"
)

// webSearchTool implements the google_web_search tool using Gemini's native
// GoogleSearch grounding feature.
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

func (w *webSearchTool) Execute(ctx context.Context, args map[string]interface{}) (map[string]interface{}, error) {
	query, ok := args[ParamQuery].(string)
	if !ok || query == "" {
		return nil, fmt.Errorf("%s requires a non-empty string parameter %q", w.Name(), ParamQuery)
	}

	text, meta, err := w.utilityClient.Search(ctx, query)
	if err != nil {
		// Provide a helpful error that the agent can read.
		return map[string]interface{}{
			"results": fmt.Sprintf("Error performing web search: %v", err),
		}, nil
	}

	formatted := web.FormatSearchResult(query, text, meta)
	return map[string]interface{}{
		"results": formatted,
	}, nil
}
