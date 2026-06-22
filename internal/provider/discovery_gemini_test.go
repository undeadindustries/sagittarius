package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDiscoverGeminiModels(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models" {
			t.Fatalf("path = %q, want /v1beta/models", r.URL.Path)
		}
		if !strings.Contains(r.URL.RawQuery, "key=test-key") {
			t.Fatalf("missing key query param: %q", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(geminiModelsResponse{
			Models: []geminiModelEntry{
				{Name: "models/gemini-2.5-pro", SupportedGenerationMethods: []string{"generateContent"}},
				{Name: "models/text-embedding-004", SupportedGenerationMethods: []string{"embedContent"}},
			},
		})
	}))
	t.Cleanup(srv.Close)

	infos, err := fetchGeminiModels(context.Background(), srv.URL+"/v1beta/models?key=test-key", srv.Client())
	if err != nil {
		t.Fatalf("fetchGeminiModels: %v", err)
	}
	if len(infos) != 1 || infos[0].ID != "gemini-2.5-pro" {
		t.Fatalf("models = %v, want [gemini-2.5-pro]", infos)
	}
}

func TestDiscoverGeminiModelsRequiresKey(t *testing.T) {
	t.Parallel()
	_, err := DiscoverGeminiModels(context.Background(), "", nil)
	if err == nil {
		t.Fatal("expected error for missing API key")
	}
}

func TestGeminiModelIDStripsPrefix(t *testing.T) {
	t.Parallel()
	if got := geminiModelID("models/gemini-2.5-flash"); got != "gemini-2.5-flash" {
		t.Fatalf("geminiModelID = %q", got)
	}
}

func TestDiscoverGeminiModelsPagination(t *testing.T) {
	t.Parallel()

	pages := []geminiModelsResponse{
		{
			Models: []geminiModelEntry{
				{Name: "models/gemini-2.5-pro", SupportedGenerationMethods: []string{"generateContent"}},
			},
			NextPageToken: "page2token",
		},
		{
			Models: []geminiModelEntry{
				{Name: "models/gemini-2.5-flash", SupportedGenerationMethods: []string{"generateContent"}},
				// Non-gemini model slipping through generateContent — should be filtered.
				{Name: "models/learnlm-2.0-flash-experimental", SupportedGenerationMethods: []string{"generateContent"}},
			},
			NextPageToken: "",
		},
	}
	call := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := call
		call++
		if idx >= len(pages) {
			t.Fatalf("unexpected page request %d", idx)
		}
		_ = json.NewEncoder(w).Encode(pages[idx])
	}))
	t.Cleanup(srv.Close)

	infos, err := fetchGeminiModels(context.Background(), srv.URL+"/v1beta/models?key=k", srv.Client())
	if err != nil {
		t.Fatalf("fetchGeminiModels: %v", err)
	}
	if call != 2 {
		t.Fatalf("expected 2 page requests, got %d", call)
	}
	wantIDs := []string{"gemini-2.5-flash", "gemini-2.5-pro"}
	if len(infos) != len(wantIDs) {
		t.Fatalf("models = %v, want %v", infos, wantIDs)
	}
	for i, info := range infos {
		if info.ID != wantIDs[i] {
			t.Fatalf("infos[%d].ID = %q, want %q", i, info.ID, wantIDs[i])
		}
	}
}

// TestDiscoverGeminiModelsPageTokenNeverAccumulates is the regression test for
// the Bugbot finding: prior code appended pageToken to the previous URL each
// iteration, so from page 3 onward the URL contained duplicate pageToken params
// (e.g. pageToken=p2&pageToken=p3). The fix parses the base URL once and uses
// url.Values.Set so each request carries exactly one pageToken parameter.
func TestDiscoverGeminiModelsPageTokenNeverAccumulates(t *testing.T) {
	t.Parallel()

	// tokens[i] is the pageToken the handler expects to receive for page i.
	// Page 0 has no token; page 1 carries the token from page 0's response; etc.
	tokens := []string{"", "tok1", "tok2"}
	pages := []geminiModelsResponse{
		{
			Models:        []geminiModelEntry{{Name: "models/gemini-a", SupportedGenerationMethods: []string{"generateContent"}}},
			NextPageToken: "tok1",
		},
		{
			Models:        []geminiModelEntry{{Name: "models/gemini-b", SupportedGenerationMethods: []string{"generateContent"}}},
			NextPageToken: "tok2",
		},
		{
			Models: []geminiModelEntry{{Name: "models/gemini-c", SupportedGenerationMethods: []string{"generateContent"}}},
		},
	}
	call := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := call
		call++
		if idx >= len(pages) {
			t.Fatalf("unexpected page request %d", idx)
		}
		q := r.URL.Query()
		// Page 0: no pageToken expected; pages 1+ expect exactly one token value.
		gotTokens := q["pageToken"]
		wantToken := tokens[idx]
		if wantToken == "" {
			if len(gotTokens) != 0 {
				t.Errorf("page %d: unexpected pageToken params %v", idx, gotTokens)
			}
		} else {
			if len(gotTokens) != 1 {
				t.Errorf("page %d: want exactly 1 pageToken, got %v", idx, gotTokens)
			} else if gotTokens[0] != wantToken {
				t.Errorf("page %d: pageToken = %q, want %q", idx, gotTokens[0], wantToken)
			}
		}
		_ = json.NewEncoder(w).Encode(pages[idx])
	}))
	t.Cleanup(srv.Close)

	infos, err := fetchGeminiModels(context.Background(), srv.URL+"/v1beta/models?key=k", srv.Client())
	if err != nil {
		t.Fatalf("fetchGeminiModels: %v", err)
	}
	if call != 3 {
		t.Fatalf("expected 3 page requests, got %d", call)
	}
	if len(infos) != 3 {
		t.Fatalf("expected 3 models, got %d: %v", len(infos), infos)
	}
}

func TestDiscoverGeminiModelsFiltersNonGeminiPrefix(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(geminiModelsResponse{
			Models: []geminiModelEntry{
				{Name: "models/gemini-2.5-pro", SupportedGenerationMethods: []string{"generateContent"}},
				{Name: "models/learnlm-2.0-flash-experimental", SupportedGenerationMethods: []string{"generateContent"}},
				{Name: "models/aqa", SupportedGenerationMethods: []string{"generateContent"}},
			},
		})
	}))
	t.Cleanup(srv.Close)

	infos, err := fetchGeminiModels(context.Background(), srv.URL+"/v1beta/models?key=k", srv.Client())
	if err != nil {
		t.Fatalf("fetchGeminiModels: %v", err)
	}
	if len(infos) != 1 || infos[0].ID != "gemini-2.5-pro" {
		t.Fatalf("models = %v, want only gemini-2.5-pro", infos)
	}
}
