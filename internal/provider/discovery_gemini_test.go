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
