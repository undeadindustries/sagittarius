package tools

import (
	"context"
	"strings"
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

// TestWebSearchWithoutClientErrors guards against the nil-pointer dereference
// that crashed the agent when search was enabled without a resolvable key.
func TestWebSearchWithoutClientErrors(t *testing.T) {
	tool := newGoogleWebSearchTool(nil)

	_, err := tool.Execute(context.Background(), map[string]interface{}{ParamQuery: "golang"})
	if err == nil {
		t.Fatal("expected an error when no utility client is configured")
	}
	if !strings.Contains(err.Error(), "no Gemini API key") {
		t.Errorf("error %q should explain the missing key", err)
	}
}

// TestWebSearchSkippedWithoutClient asserts the registry hides a tool that could
// never succeed, while web_fetch (which has a key-free fallback) still registers.
func TestWebSearchSkippedWithoutClient(t *testing.T) {
	ws := newTestWorkspace(t)
	reg := NewBuiltinRegistry(ws, WithWebTools(true, true, nil, false, 0))

	if _, ok := reg.Lookup(GoogleWebSearchToolName); ok {
		t.Errorf("%s should not be registered without a Gemini utility client", GoogleWebSearchToolName)
	}
	if _, ok := reg.Lookup(WebFetchToolName); !ok {
		t.Errorf("%s should register regardless: its Go HTTP fallback needs no key", WebFetchToolName)
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
