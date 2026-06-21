package config

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestSagittariusSettingsRoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	raw := `{
  "providers": { "active": "openai", "openai": { "model": "gpt-4o" } },
  "sagittarius": {
    "defaultModel": "global-model",
    "defaultMode": "plan",
    "modes": {
      "plan": { "model": "plan-model", "systemPromptSuffix": "Think step by step." },
      "ask": { "model": "ask-model" }
    },
    "subagents": {
      "default": { "model": "sub-default" },
      "investigator": { "model": "investigator-model" }
    },
    "futureFeature": true
  },
  "ui": { "theme": "dark" }
}`
	writeFile(t, path, []byte(raw))

	loader, err := NewLoader(WithSettingsPath(path))
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}
	s, err := loader.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Sagittarius == nil {
		t.Fatal("Sagittarius is nil")
	}
	if got := s.Sagittarius.DefaultModel; got != "global-model" {
		t.Errorf("DefaultModel = %q", got)
	}
	if got := s.Sagittarius.Modes.Plan.Model; got != "plan-model" {
		t.Errorf("plan model = %q", got)
	}
	if got := s.Sagittarius.Subagents.Named["investigator"].Model; got != "investigator-model" {
		t.Errorf("investigator model = %q", got)
	}
	if _, ok := s.Sagittarius.Extra["futureFeature"]; !ok {
		t.Error("futureFeature passthrough missing")
	}

	if err := loader.Save(s); err != nil {
		t.Fatalf("Save: %v", err)
	}
	reloaded, err := loader.Load()
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if reloaded.Sagittarius.DefaultMode != "plan" {
		t.Errorf("DefaultMode = %q", reloaded.Sagittarius.DefaultMode)
	}
}

func TestValidateSagittariusSettingsRejectsBadMode(t *testing.T) {
	t.Parallel()

	_, err := unmarshalSagittarius(json.RawMessage(`{"defaultMode":"yolo"}`))
	if err == nil {
		t.Fatal("expected validation error for bad defaultMode")
	}
}

// TestValidateSuffixOnlyModeAccepted guards that a mode block with only
// systemPromptSuffix (no model) is valid: ResolveModel falls back to
// defaultModel / provider default while the suffix still applies.
func TestValidateSuffixOnlyModeAccepted(t *testing.T) {
	t.Parallel()

	s, err := unmarshalSagittarius(json.RawMessage(`{"modes":{"plan":{"systemPromptSuffix":"Plan carefully."}}}`))
	if err != nil {
		t.Fatalf("suffix-only mode rejected: %v", err)
	}
	if s == nil || s.Modes == nil || s.Modes.Plan == nil {
		t.Fatal("plan mode config not parsed")
	}
	if got := s.Modes.Plan.SystemPromptSuffix; got != "Plan carefully." {
		t.Errorf("systemPromptSuffix = %q", got)
	}
	if got := s.Modes.Plan.Model; got != "" {
		t.Errorf("plan model = %q, want empty", got)
	}
}
