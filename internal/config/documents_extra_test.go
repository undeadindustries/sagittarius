package config

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// captureWarnings redirects slog to a buffer for the duration of the test.
func captureWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// TestMergeProvidersExtraOverlaysGlobalFields is the behavior AD-117 depends on:
// a project block that names only a model must not erase the global endpoint, or
// selecting a model in project scope breaks the provider it selected.
func TestMergeProvidersExtraOverlaysGlobalFields(t *testing.T) {
	global := map[string]json.RawMessage{
		"gx10": json.RawMessage(`{"baseUrl":"http://gx10:8000/v1","contextLimit":131072,"model":"qwen"}`),
	}
	project := map[string]json.RawMessage{
		"gx10": json.RawMessage(`{"model":"gemma"}`),
	}

	var got ProviderInstanceConfig
	if err := json.Unmarshal(mergeProvidersExtra(global, project)["gx10"], &got); err != nil {
		t.Fatalf("unmarshal merged block: %v", err)
	}
	if got.Model != "gemma" {
		t.Errorf("Model = %q, want the project override gemma", got.Model)
	}
	if got.BaseURL != "http://gx10:8000/v1" {
		t.Errorf("BaseURL = %q, want the global value preserved", got.BaseURL)
	}
}

// TestMergeProvidersExtraWarnsOnMalformedBlock covers the fallback path. Taking
// the project block wholesale is the safe choice, but doing it silently hid the
// data loss: the provider appears to forget fields it never restated.
func TestMergeProvidersExtraWarnsOnMalformedBlock(t *testing.T) {
	logs := captureWarnings(t)

	global := map[string]json.RawMessage{
		"gx10": json.RawMessage(`{"baseUrl":"http://gx10:8000/v1"}`),
	}
	project := map[string]json.RawMessage{
		"gx10": json.RawMessage(`{"model":`), // truncated: decode fails
	}

	merged := mergeProvidersExtra(global, project)
	if string(merged["gx10"]) != `{"model":` {
		t.Errorf("merged block = %s, want the project block verbatim", merged["gx10"])
	}

	out := logs.String()
	if !strings.Contains(out, "level=WARN") {
		t.Fatalf("no warning was logged for a malformed block:\n%s", out)
	}
	if !strings.Contains(out, "gx10") {
		t.Errorf("warning does not name the provider, so it is not actionable:\n%s", out)
	}
}

// TestMergeProvidersExtraQuietOnCleanMerge keeps the warning meaningful: an
// ordinary merge must not log, or the signal is lost in noise.
func TestMergeProvidersExtraQuietOnCleanMerge(t *testing.T) {
	logs := captureWarnings(t)

	mergeProvidersExtra(
		map[string]json.RawMessage{"gx10": json.RawMessage(`{"baseUrl":"http://gx10:8000/v1"}`)},
		map[string]json.RawMessage{"gx10": json.RawMessage(`{"model":"gemma"}`)},
	)

	if out := logs.String(); out != "" {
		t.Errorf("a clean merge logged output:\n%s", out)
	}
}
