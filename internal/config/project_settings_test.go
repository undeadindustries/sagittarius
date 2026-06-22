package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndMergeProjectSystemPrompt(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	if err := SaveProjectSystemPrompt(workDir, PersonalitySysadmin, VariantLite); err != nil {
		t.Fatalf("SaveProjectSystemPrompt: %v", err)
	}

	path := ResolveProjectSettingsPath(workDir)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("project settings file missing: %v", err)
	}

	global := &Settings{Sagittarius: &SagittariusSettings{
		SystemPrompt: &SagittariusSystemPromptConfig{
			Personality: PersonalityProgrammer,
			Variant:     VariantFull,
		},
	}}
	if err := MergeProjectSystemPrompt(global, workDir); err != nil {
		t.Fatalf("MergeProjectSystemPrompt: %v", err)
	}
	if global.Sagittarius.SystemPrompt.Personality != PersonalitySysadmin {
		t.Errorf("personality = %q, want %q", global.Sagittarius.SystemPrompt.Personality, PersonalitySysadmin)
	}
	if global.Sagittarius.SystemPrompt.Variant != VariantLite {
		t.Errorf("variant = %q, want %q", global.Sagittarius.SystemPrompt.Variant, VariantLite)
	}
	if got := ProjectSystemPromptPresetID(global); got != "sysadmin-lite" {
		t.Errorf("preset id = %q, want sysadmin-lite", got)
	}
}

func TestMergeProjectSystemPromptNoFileIsNoOp(t *testing.T) {
	t.Parallel()

	global := &Settings{}
	if err := MergeProjectSystemPrompt(global, t.TempDir()); err != nil {
		t.Fatalf("MergeProjectSystemPrompt: %v", err)
	}
	if global.Sagittarius != nil && global.Sagittarius.SystemPrompt != nil {
		t.Fatal("expected no system prompt overlay when project file is absent")
	}
}

func TestSaveProjectSystemPromptMergesExistingProjectFile(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	dir := filepath.Join(workDir, ".sagittarius")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	existing := `{"security":{"projectBoundary":{"enforce":true}}}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(existing), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := SaveProjectSystemPrompt(workDir, PersonalityCreativeAssistant, VariantFull); err != nil {
		t.Fatalf("SaveProjectSystemPrompt: %v", err)
	}
	ps, err := LoadProjectSettings(workDir)
	if err != nil {
		t.Fatalf("LoadProjectSettings: %v", err)
	}
	if !ProjectBoundaryEnforced(nil, ps) {
		t.Fatal("expected existing project security settings to be preserved")
	}
	if ps.Sagittarius == nil || ps.Sagittarius.SystemPrompt == nil {
		t.Fatal("expected system prompt to be written")
	}
	if ps.Sagittarius.SystemPrompt.Personality != PersonalityCreativeAssistant {
		t.Errorf("personality = %q", ps.Sagittarius.SystemPrompt.Personality)
	}
}
