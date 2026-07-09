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
    "defaultModels": {
      "openai": "gpt-4o-mini",
      "gemini-apikey": "gemini-2.5-flash"
    },
    "defaultMode": "plan",
    "modes": {
      "plan": { "model": "plan-model", "systemPromptSuffix": "Think step by step." },
      "ask": { "model": "ask-model" }
    },
    "subagents": {
      "default": { "model": "sub-default" },
      "investigator": { "model": "investigator-model" }
    },
    "compression": { "model": "compressor-model" },
    "tools": { "model": "tools-model" },
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
	if got := s.Sagittarius.DefaultModels["openai"]; got != "gpt-4o-mini" {
		t.Errorf("defaultModels[openai] = %q", got)
	}
	if got := s.Sagittarius.DefaultModels["gemini-apikey"]; got != "gemini-2.5-flash" {
		t.Errorf("defaultModels[gemini-apikey] = %q", got)
	}
	if got := s.Sagittarius.Modes.Plan.Model; got != "plan-model" {
		t.Errorf("plan model = %q", got)
	}
	if got := s.Sagittarius.Subagents.Named["investigator"].Model; got != "investigator-model" {
		t.Errorf("investigator model = %q", got)
	}
	if s.Sagittarius.Compression == nil || s.Sagittarius.Compression.Model != "compressor-model" {
		t.Errorf("compression model = %+v", s.Sagittarius.Compression)
	}
	if s.Sagittarius.Tools == nil || s.Sagittarius.Tools.Model != "tools-model" {
		t.Errorf("tools model = %+v", s.Sagittarius.Tools)
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
	if got := reloaded.Sagittarius.DefaultModels["openai"]; got != "gpt-4o-mini" {
		t.Errorf("reloaded defaultModels[openai] = %q", got)
	}
	if reloaded.Sagittarius.Compression == nil || reloaded.Sagittarius.Compression.Model != "compressor-model" {
		t.Errorf("reloaded compression model = %+v", reloaded.Sagittarius.Compression)
	}
	if reloaded.Sagittarius.Tools == nil || reloaded.Sagittarius.Tools.Model != "tools-model" {
		t.Errorf("reloaded tools model = %+v", reloaded.Sagittarius.Tools)
	}
}

// TestDefaultModelsOmittedWhenEmpty guards that an empty defaultModels map is not
// serialized, so settings.json stays clean for users who never set one.
func TestDefaultModelsOmittedWhenEmpty(t *testing.T) {
	t.Parallel()

	raw, err := marshalSagittarius(&SagittariusSettings{DefaultModel: "only-global"})
	if err != nil {
		t.Fatalf("marshalSagittarius: %v", err)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("unmarshal serialized sagittarius: %v", err)
	}
	if _, ok := obj["defaultModels"]; ok {
		t.Error("empty defaultModels should be omitted from serialized output")
	}
}

// TestGrillConfigRoundTrip guards against the grill config being defined but
// never wired into unmarshalSagittarius/marshalSagittarius, which would
// silently drop it into Extra instead of populating SagittariusGrillConfig.
func TestGrillConfigRoundTrip(t *testing.T) {
	t.Parallel()

	s, err := unmarshalSagittarius(json.RawMessage(`{
  "grill": {
    "specDir": "docs/interrogations",
    "maxQuestions": 12,
    "recommend": false
  }
}`))
	if err != nil {
		t.Fatalf("unmarshalSagittarius: %v", err)
	}
	if s.Grill == nil {
		t.Fatal("Grill is nil")
	}
	if got := s.Grill.SpecDir; got != "docs/interrogations" {
		t.Errorf("SpecDir = %q", got)
	}
	if s.Grill.MaxQuestions == nil || *s.Grill.MaxQuestions != 12 {
		t.Errorf("MaxQuestions = %+v", s.Grill.MaxQuestions)
	}
	if s.Grill.Recommend == nil || *s.Grill.Recommend != false {
		t.Errorf("Recommend = %+v", s.Grill.Recommend)
	}
	if _, ok := s.Extra["grill"]; ok {
		t.Error("grill leaked into Extra instead of being parsed into Grill")
	}

	raw, err := marshalSagittarius(s)
	if err != nil {
		t.Fatalf("marshalSagittarius: %v", err)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("unmarshal serialized sagittarius: %v", err)
	}
	grillRaw, ok := obj["grill"]
	if !ok {
		t.Fatal("grill missing from serialized output")
	}
	reparsed, err := unmarshalGrillConfig(grillRaw)
	if err != nil {
		t.Fatalf("unmarshalGrillConfig(reserialized): %v", err)
	}
	if reparsed.SpecDir != "docs/interrogations" {
		t.Errorf("reserialized SpecDir = %q", reparsed.SpecDir)
	}
	if reparsed.MaxQuestions == nil || *reparsed.MaxQuestions != 12 {
		t.Errorf("reserialized MaxQuestions = %+v", reparsed.MaxQuestions)
	}
	if reparsed.Recommend == nil || *reparsed.Recommend != false {
		t.Errorf("reserialized Recommend = %+v", reparsed.Recommend)
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
