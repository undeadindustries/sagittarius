package web

import (
	"context"
	"strings"
	"testing"
)

const sampleBraveJSON = `{
  "query": {
    "original": "golang concurrency"
  },
  "web": {
    "results": [
      {
        "title": "Go Concurrency Patterns",
        "url": "https://go.dev/blog/pipelines",
        "description": "Pipelines and cancellation in Go using channels and goroutines."
      },
      {
        "title": "Effective Go - Concurrency",
        "url": "https://go.dev/doc/effective_go#concurrency",
        "description": "Do not communicate by sharing memory; instead, share memory by communicating."
      }
    ]
  }
}`

const sampleDDGHTML = `<!DOCTYPE html>
<html>
<body>
<div class="results">
  <div class="result results_links results_links_deep web-result">
    <div class="links_main links_deep result__body">
      <h2 class="result__title">
        <a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgolang.org%2Fpkg%2Fsync%2F&rut=123">sync package - Go Packages</a>
      </h2>
      <a class="result__snippet" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgolang.org%2Fpkg%2Fsync%2F&rut=123">Package sync provides basic synchronization primitives such as mutual exclusion locks.</a>
    </div>
  </div>
  <div class="result results_links results_links_deep web-result">
    <div class="links_main links_deep result__body">
      <h2 class="result__title">
        <a class="result__a" href="https://example.com/direct-link">Direct Link Example</a>
      </h2>
      <a class="result__snippet">This snippet has no href attribute.</a>
    </div>
  </div>
</div>
</body>
</html>`

const sampleDDGLiteHTML = `<!DOCTYPE html>
<html>
<body>
<table>
  <tr>
    <td>
      <a class="result-link" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fpkg.go.dev%2F">Go Packages - pkg.go.dev</a>
    </td>
  </tr>
  <tr>
    <td class="result-snippet">Discover packages in the Go ecosystem.</td>
  </tr>
</table>
</body>
</html>`

func TestParseBraveResponse(t *testing.T) {
	hits, err := parseBraveResponse([]byte(sampleBraveJSON))
	if err != nil {
		t.Fatalf("parseBraveResponse failed: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits, got %d", len(hits))
	}
	if hits[0].Title != "Go Concurrency Patterns" || hits[0].URL != "https://go.dev/blog/pipelines" {
		t.Errorf("unexpected hit 0: %+v", hits[0])
	}
	if !strings.Contains(hits[0].Snippet, "Pipelines and cancellation") {
		t.Errorf("unexpected snippet 0: %q", hits[0].Snippet)
	}
	if hits[1].Title != "Effective Go - Concurrency" {
		t.Errorf("unexpected hit 1: %+v", hits[1])
	}
}

func TestParseBraveResponse_Empty(t *testing.T) {
	hits, err := parseBraveResponse([]byte(`{"web": {"results": []}}`))
	if err != nil {
		t.Fatalf("unexpected error on empty results: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("expected 0 hits, got %d", len(hits))
	}
}

func TestParseDuckDuckGoHTML(t *testing.T) {
	hits, err := parseDuckDuckGoHTML(strings.NewReader(sampleDDGHTML))
	if err != nil {
		t.Fatalf("parseDuckDuckGoHTML failed: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits, got %d", len(hits))
	}
	if hits[0].Title != "sync package - Go Packages" {
		t.Errorf("unexpected title: %q", hits[0].Title)
	}
	if hits[0].URL != "https://golang.org/pkg/sync/" {
		t.Errorf("expected decoded uddg url https://golang.org/pkg/sync/, got %q", hits[0].URL)
	}
	if !strings.Contains(hits[0].Snippet, "mutual exclusion locks") {
		t.Errorf("unexpected snippet: %q", hits[0].Snippet)
	}
	if hits[1].URL != "https://example.com/direct-link" {
		t.Errorf("expected direct url, got %q", hits[1].URL)
	}
}

func TestParseDuckDuckGoHTMLLite(t *testing.T) {
	hits, err := parseDuckDuckGoHTML(strings.NewReader(sampleDDGLiteHTML))
	if err != nil {
		t.Fatalf("parseDuckDuckGoHTML lite failed: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(hits))
	}
	if hits[0].Title != "Go Packages - pkg.go.dev" {
		t.Errorf("unexpected title: %q", hits[0].Title)
	}
	if hits[0].URL != "https://pkg.go.dev/" {
		t.Errorf("expected decoded uddg url https://pkg.go.dev/, got %q", hits[0].URL)
	}
	if hits[0].Snippet != "Discover packages in the Go ecosystem." {
		t.Errorf("unexpected snippet: %q", hits[0].Snippet)
	}
}

func TestCleanDuckDuckGoURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"https://example.com/page", "https://example.com/page"},
		{"//duckduckgo.com/l/?uddg=https%3A%2F%2Fgithub.com%2Ffoo", "https://github.com/foo"},
		{"/l/?uddg=https%3A%2F%2Fgolang.org", "https://golang.org"},
	}
	for _, tc := range tests {
		got := cleanDuckDuckGoURL(tc.input)
		if got != tc.want {
			t.Errorf("cleanDuckDuckGoURL(%q) = %q; want %q", tc.input, got, tc.want)
		}
	}
}

func TestFormatOrganicResults(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		got := FormatOrganicResults("quantum computing", "DuckDuckGo", nil)
		want := "Web search results for \"quantum computing\" (DuckDuckGo):\n\nNo results found."
		if got != want {
			t.Errorf("FormatOrganicResults empty = %q; want %q", got, want)
		}
	})

	t.Run("formatted with hits", func(t *testing.T) {
		hits := []SearchResult{
			{
				Title:   "First Result",
				URL:     "https://example.com/1",
				Snippet: "First snippet description.",
			},
			{
				Title:   "Second Result",
				URL:     "https://example.com/2",
				Snippet: "Second snippet description.",
			},
		}
		got := FormatOrganicResults("test query", "Brave Search", hits)
		if !strings.Contains(got, "Web search results for \"test query\" (Brave Search):") {
			t.Errorf("missing header in %q", got)
		}
		if !strings.Contains(got, "[1] First Result\nURL: https://example.com/1\nFirst snippet description.") {
			t.Errorf("missing hit 1 in %q", got)
		}
		if !strings.Contains(got, "[2] Second Result\nURL: https://example.com/2\nSecond snippet description.") {
			t.Errorf("missing hit 2 in %q", got)
		}
	})
}

func TestSearchValidation(t *testing.T) {
	ctx := context.Background()
	if _, err := SearchBrave(ctx, "", "token"); err == nil {
		t.Error("expected error on empty query for SearchBrave")
	}
	if _, err := SearchBrave(ctx, "query", ""); err == nil {
		t.Error("expected error on empty apiKey for SearchBrave")
	}
	if _, err := SearchDuckDuckGo(ctx, ""); err == nil {
		t.Error("expected error on empty query for SearchDuckDuckGo")
	}
}
