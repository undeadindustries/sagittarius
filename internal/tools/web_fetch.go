package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/web"
)

type webFetchTool struct {
	utilityClient  *provider.GeminiUtilityClient
	directWebFetch bool
	maxFetchBytes  int
}

func newWebFetchTool(client *provider.GeminiUtilityClient, directWebFetch bool, maxFetchBytes int) *webFetchTool {
	return &webFetchTool{
		utilityClient:  client,
		directWebFetch: directWebFetch,
		maxFetchBytes:  maxFetchBytes,
	}
}

func (w *webFetchTool) Name() string {
	return WebFetchToolName
}

func (w *webFetchTool) Description() string {
	return "Fetch content from a specified URL. Use this to retrieve information from webpages to answer user questions or provide context."
}

func (w *webFetchTool) RequiresConfirmation() bool {
	return true
}

func (w *webFetchTool) Declaration() provider.ToolDeclaration {
	if w.directWebFetch {
		return provider.ToolDeclaration{
			Name:        w.Name(),
			Description: w.Description(),
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					ParamURL: map[string]interface{}{
						"type":        "string",
						"description": "The URL to fetch. Must be a valid http or https URL.",
					},
				},
				"required": []string{ParamURL},
			},
		}
	}
	return provider.ToolDeclaration{
		Name:        w.Name(),
		Description: w.Description(),
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				ParamPrompt: map[string]interface{}{
					"type":        "string",
					"description": "The prompt containing URLs to fetch and instructions on what to extract. Must contain valid http/https URLs.",
				},
			},
			"required": []string{ParamPrompt},
		},
	}
}

func (w *webFetchTool) Execute(ctx context.Context, args map[string]interface{}) (map[string]interface{}, error) {
	if w.directWebFetch {
		return w.executeDirect(ctx, args)
	}
	return w.executeDefault(ctx, args)
}

func (w *webFetchTool) executeDirect(ctx context.Context, args map[string]interface{}) (map[string]interface{}, error) {
	rawURL, ok := args[ParamURL].(string)
	if !ok || rawURL == "" {
		return nil, fmt.Errorf("%s requires a non-empty string parameter %q", w.Name(), ParamURL)
	}

	validURLs, errs := web.ParsePrompt(rawURL)
	if len(validURLs) == 0 {
		return map[string]interface{}{
			"results": fmt.Sprintf("No valid URLs found in parameter. Errors: %s", strings.Join(errs, " ")),
		}, nil
	}

	targetURL := validURLs[0]
	targetURL = web.GitHubBlobToRaw(targetURL)

	data, err := web.FetchURL(ctx, targetURL, w.maxFetchBytes)
	if err != nil {
		return map[string]interface{}{
			"results": fmt.Sprintf("Error fetching %s: %v", targetURL, err),
		}, nil
	}

	text := web.HTMLToText(data, targetURL)
	return map[string]interface{}{
		"results": text,
	}, nil
}

func (w *webFetchTool) executeDefault(ctx context.Context, args map[string]interface{}) (map[string]interface{}, error) {
	prompt, ok := args[ParamPrompt].(string)
	if !ok || prompt == "" {
		return nil, fmt.Errorf("%s requires a non-empty string parameter %q", w.Name(), ParamPrompt)
	}

	validURLs, errs := web.ParsePrompt(prompt)
	if len(validURLs) == 0 {
		return map[string]interface{}{
			"results": fmt.Sprintf("No valid URLs found in prompt. Errors: %s", strings.Join(errs, " ")),
		}, nil
	}

	var fetchURLs []string
	for _, u := range validURLs {
		fetchURLs = append(fetchURLs, web.GitHubBlobToRaw(u))
	}

	var xmlBuilder strings.Builder
	xmlBuilder.WriteString("<user_instructions>\n")
	xmlBuilder.WriteString(prompt)
	xmlBuilder.WriteString("\n</user_instructions>\n<authorized_urls>\n")
	for _, u := range fetchURLs {
		xmlBuilder.WriteString(u)
		xmlBuilder.WriteString("\n")
	}
	xmlBuilder.WriteString("</authorized_urls>")

	if w.utilityClient != nil {
		text, _, err := w.utilityClient.FetchURLContext(ctx, xmlBuilder.String())
		if err == nil {
			return map[string]interface{}{
				"results": text,
			}, nil
		}
	}

	var fallbackTexts []string
	for _, u := range fetchURLs {
		data, err := web.FetchURL(ctx, u, w.maxFetchBytes)
		if err != nil {
			fallbackTexts = append(fallbackTexts, fmt.Sprintf("Error fetching %s: %v", u, err))
			continue
		}
		text := web.HTMLToText(data, u)
		fallbackTexts = append(fallbackTexts, fmt.Sprintf("Content from %s:\n%s", u, text))
	}

	combined := strings.Join(fallbackTexts, "\n\n")

	if w.utilityClient != nil {
		summaryPrompt := fmt.Sprintf("Based on the following fetched content, please answer this prompt:\n%s\n\nFetched Content:\n%s", prompt, combined)
		summary, err := w.utilityClient.Summarize(ctx, summaryPrompt)
		if err == nil {
			return map[string]interface{}{
				"results": summary,
			}, nil
		}
	}

	return map[string]interface{}{
		"results": combined,
	}, nil
}
