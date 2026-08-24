package tools

import (
	"context"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/web"
)

// TestWebDefaultFetchBudgetsAgree pins the settings-level default against the
// internal/web fallback. internal/web is a stdlib-only leaf and cannot import
// config, so nothing but this assertion keeps the two literals from drifting.
func TestWebDefaultFetchBudgetsAgree(t *testing.T) {
	if web.DefaultMaxBytes != config.DefaultMaxFetchBytes {
		t.Fatalf("web.DefaultMaxBytes = %d, config.DefaultMaxFetchBytes = %d; keep them equal",
			web.DefaultMaxBytes, config.DefaultMaxFetchBytes)
	}
}

// TestNewWebFetchToolNormalizesBudget guards the regression where the catalog
// passed maxFetchBytes=0, capping every HTTP fallback fetch at zero bytes.
func TestNewWebFetchToolNormalizesBudget(t *testing.T) {
	for _, tc := range []struct {
		name       string
		directMode bool
		maxBytes   int
		wantBudget int
	}{
		{name: "zero becomes the default", maxBytes: 0, wantBudget: config.DefaultMaxFetchBytes},
		{name: "negative becomes the default", maxBytes: -5, wantBudget: config.DefaultMaxFetchBytes},
		{name: "explicit budget is preserved", maxBytes: 1234, wantBudget: 1234},
		{
			name:       "zero in direct mode becomes the experimental default",
			directMode: true,
			maxBytes:   0,
			wantBudget: config.DefaultMaxExperimentalFetchBytes,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tool := newWebFetchTool(nil, tc.directMode, tc.maxBytes)
			if tool.maxFetchBytes != tc.wantBudget {
				t.Errorf("maxFetchBytes = %d; want %d", tool.maxFetchBytes, tc.wantBudget)
			}
		})
	}
}

// TestWebSearchRegistersWithoutClient asserts google_web_search registers
// even without a Gemini client, because it cascades to Brave and DuckDuckGo.
func TestWebSearchRegistersWithoutClient(t *testing.T) {
	ws := newTestWorkspace(t)
	reg := NewBuiltinRegistry(ws, WithWebTools(true, true, nil, false, 0))

	if _, ok := reg.Lookup(GoogleWebSearchToolName); !ok {
		t.Errorf("%s should register even without a Gemini utility client", GoogleWebSearchToolName)
	}
	if _, ok := reg.Lookup(WebFetchToolName); !ok {
		t.Errorf("%s should register: its Go HTTP fallback needs no key", WebFetchToolName)
	}
}

// TestWebSearchTool_Execute_Validation guards against empty or invalid queries.
func TestWebSearchTool_Execute_Validation(t *testing.T) {
	tool := newGoogleWebSearchTool(nil)

	if _, err := tool.Execute(context.Background(), map[string]interface{}{}); err == nil {
		t.Error("expected error for missing query")
	}
	if _, err := tool.Execute(context.Background(), map[string]interface{}{ParamQuery: ""}); err == nil {
		t.Error("expected error for empty query")
	}
}

// TestWebSearchTool_Execute_BraveFallback tests that a failing Brave key falls through to DDG.
func TestWebSearchTool_Execute_BraveFallback(t *testing.T) {
	t.Setenv("BRAVE_API_KEY", "invalid-brave-key-for-test")
	tool := newGoogleWebSearchTool(nil)

	res, err := tool.Execute(context.Background(), map[string]interface{}{ParamQuery: "golang testing"})
	if err != nil {
		t.Fatalf("Execute returned unexpected fatal error: %v", err)
	}
	resultsStr, ok := res["results"].(string)
	if !ok || resultsStr == "" {
		t.Errorf("expected string results from cascade, got %#v", res)
	}
}

func TestWebToolsDisabledByDefault(t *testing.T) {
	ws := newTestWorkspace(t)
	reg := NewBuiltinRegistry(ws)

	for _, name := range []string{GoogleWebSearchToolName, WebFetchToolName} {
		if _, ok := reg.Lookup(name); ok {
			t.Errorf("%s should stay unregistered unless WithWebTools enables it", name)
		}
	}
}
