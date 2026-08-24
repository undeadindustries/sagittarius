package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// MaxSearchHits is the maximum number of search results returned by backends.
const MaxSearchHits = 8

// SearchResult represents a single organic web search result.
type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// FormatOrganicResults formats a list of search results with indexed entries
// and backend identification.
func FormatOrganicResults(query, backend string, hits []SearchResult) string {
	if len(hits) == 0 {
		return fmt.Sprintf("Web search results for %q (%s):\n\nNo results found.", query, backend)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Web search results for %q (%s):\n\n", query, backend)
	for i, hit := range hits {
		fmt.Fprintf(&b, "[%d] %s\n", i+1, hit.Title)
		if hit.URL != "" {
			fmt.Fprintf(&b, "URL: %s\n", hit.URL)
		}
		if hit.Snippet != "" {
			fmt.Fprintf(&b, "%s\n", hit.Snippet)
		}
		if i < len(hits)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

type braveResponse struct {
	Web *struct {
		Results []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Description string `json:"description"`
		} `json:"results"`
	} `json:"web"`
}

// parseBraveResponse parses the JSON response from Brave Search API.
func parseBraveResponse(body []byte) ([]SearchResult, error) {
	var resp braveResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode brave response: %w", err)
	}
	if resp.Web == nil || len(resp.Web.Results) == 0 {
		return nil, nil
	}
	hits := make([]SearchResult, 0, len(resp.Web.Results))
	for _, r := range resp.Web.Results {
		title := strings.TrimSpace(r.Title)
		urlStr := strings.TrimSpace(r.URL)
		snippet := strings.TrimSpace(r.Description)
		if title == "" && urlStr == "" {
			continue
		}
		hits = append(hits, SearchResult{
			Title:   title,
			URL:     urlStr,
			Snippet: snippet,
		})
		if len(hits) >= MaxSearchHits {
			break
		}
	}
	return hits, nil
}

// SearchBrave performs an organic web search using the Brave Search API.
func SearchBrave(ctx context.Context, query, apiKey string) ([]SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query cannot be empty")
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("brave api key cannot be empty")
	}

	endpoint, err := url.Parse("https://api.search.brave.com/res/v1/web/search")
	if err != nil {
		return nil, err
	}
	q := endpoint.Query()
	q.Set("q", query)
	q.Set("count", fmt.Sprintf("%d", MaxSearchHits))
	endpoint.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", apiKey)
	req.Header.Set("User-Agent", "SagittariusBot/1.0 (+https://github.com/undeadindustries/sagittarius)")

	resp, err := safeHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("brave search request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("brave search returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB cap
	if err != nil {
		return nil, fmt.Errorf("read brave response: %w", err)
	}

	return parseBraveResponse(body)
}

// cleanDuckDuckGoURL resolves redirected uddg URLs to the target URL.
func cleanDuckDuckGoURL(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	if strings.HasPrefix(rawURL, "//") {
		rawURL = "https:" + rawURL
	} else if strings.HasPrefix(rawURL, "/") {
		rawURL = "https://duckduckgo.com" + rawURL
	}
	if u, err := url.Parse(rawURL); err == nil {
		if uddg := u.Query().Get("uddg"); uddg != "" {
			return uddg
		}
	}
	return rawURL
}

func nodeText(n *html.Node) string {
	var buf strings.Builder
	var extract func(*html.Node)
	extract = func(cn *html.Node) {
		if cn.Type == html.TextNode {
			buf.WriteString(cn.Data)
			buf.WriteString(" ")
		}
		for c := cn.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && (c.Data == "script" || c.Data == "style") {
				continue
			}
			extract(c)
		}
	}
	extract(n)
	return strings.Join(strings.Fields(buf.String()), " ")
}

func getAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasClass(n *html.Node, className string) bool {
	classAttr := getAttr(n, "class")
	for _, c := range strings.Fields(classAttr) {
		if c == className {
			return true
		}
	}
	return false
}

// parseDuckDuckGoHTML parses search results from DuckDuckGo HTML / HTML lite pages.
func parseDuckDuckGoHTML(r io.Reader) ([]SearchResult, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return nil, fmt.Errorf("parse duckduckgo html: %w", err)
	}

	var results []SearchResult

	// 1. Try finding by container blocks (.result__body or .result)
	var findResults func(*html.Node)
	findResults = func(n *html.Node) {
		if len(results) >= MaxSearchHits {
			return
		}
		if n.Type == html.ElementNode && (hasClass(n, "result__body") || hasClass(n, "result")) {
			var title, link, snippet string
			var searchInside func(*html.Node)
			searchInside = func(cn *html.Node) {
				if cn.Type == html.ElementNode {
					if hasClass(cn, "result__a") || hasClass(cn, "result-link") {
						if title == "" {
							title = nodeText(cn)
							link = cleanDuckDuckGoURL(getAttr(cn, "href"))
						}
					} else if hasClass(cn, "result__snippet") || hasClass(cn, "result-snippet") {
						if snippet == "" {
							snippet = nodeText(cn)
						}
					}
				}
				for c := cn.FirstChild; c != nil; c = c.NextSibling {
					searchInside(c)
				}
			}
			searchInside(n)
			if title != "" && link != "" {
				results = append(results, SearchResult{
					Title:   title,
					URL:     link,
					Snippet: snippet,
				})
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			findResults(c)
		}
	}
	findResults(doc)

	// 2. If no container matched, fall back to sequential result__a and result__snippet scanning
	if len(results) == 0 {
		var titles []string
		var urls []string
		var snippets []string

		var collect func(*html.Node)
		collect = func(n *html.Node) {
			if n.Type == html.ElementNode {
				if hasClass(n, "result__a") || hasClass(n, "result-link") {
					titles = append(titles, nodeText(n))
					urls = append(urls, cleanDuckDuckGoURL(getAttr(n, "href")))
				} else if hasClass(n, "result__snippet") || hasClass(n, "result-snippet") {
					snippets = append(snippets, nodeText(n))
				}
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				collect(c)
			}
		}
		collect(doc)

		for i := 0; i < len(titles) && i < len(urls); i++ {
			snip := ""
			if i < len(snippets) {
				snip = snippets[i]
			}
			if titles[i] != "" && urls[i] != "" {
				results = append(results, SearchResult{
					Title:   titles[i],
					URL:     urls[i],
					Snippet: snip,
				})
				if len(results) >= MaxSearchHits {
					break
				}
			}
		}
	}

	return results, nil
}

// SearchDuckDuckGo performs an organic web search by fetching DuckDuckGo HTML.
func SearchDuckDuckGo(ctx context.Context, query string) ([]SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query cannot be empty")
	}

	form := url.Values{}
	form.Set("q", query)
	form.Set("b", "")

	req, err := http.NewRequestWithContext(ctx, "POST", "https://html.duckduckgo.com/html/", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; SagittariusBot/1.0; +https://github.com/undeadindustries/sagittarius)")

	resp, err := safeHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("duckduckgo search request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("duckduckgo search returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB cap
	if err != nil {
		return nil, fmt.Errorf("read duckduckgo response: %w", err)
	}

	return parseDuckDuckGoHTML(bytes.NewReader(body))
}
