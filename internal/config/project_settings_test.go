package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDocumentsSaveProjectSystemPrompt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SAGITTARIUS_HOME", home)
	workDir := t.TempDir()

	docs, err := LoadDocuments(workDir)
	if err != nil {
		t.Fatalf("LoadDocuments: %v", err)
	}
	docs.Global.Sagittarius = &SagittariusSettings{
		SystemPrompt: &SagittariusSystemPromptConfig{
			Personality: PersonalityProgrammer,
			Variant:     VariantFull,
		},
	}
	docs.ReloadMerged()

	s := docs.TargetSettings(ScopeProject)
	s.Sagittarius = &SagittariusSettings{
		SystemPrompt: &SagittariusSystemPromptConfig{
			Personality: PersonalitySysadmin,
			Variant:     VariantLite,
		},
	}
	if err := docs.SaveProject(); err != nil {
		t.Fatalf("SaveProject: %v", err)
	}

	path := ResolveProjectSettingsPath(workDir)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("project settings file missing: %v", err)
	}

	// Project wins in the merged view.
	if got := docs.Merged.Sagittarius.SystemPrompt.Personality; got != PersonalitySysadmin {
		t.Errorf("personality = %q, want %q", got, PersonalitySysadmin)
	}
	if got := docs.Merged.Sagittarius.SystemPrompt.Variant; got != VariantLite {
		t.Errorf("variant = %q, want %q", got, VariantLite)
	}
	if got := ProjectSystemPromptPresetID(docs.Merged); got != "sysadmin-lite" {
		t.Errorf("preset id = %q, want sysadmin-lite", got)
	}
}

func TestDocumentsNoProjectFileIsNoOp(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SAGITTARIUS_HOME", home)

	docs, err := LoadDocuments(t.TempDir())
	if err != nil {
		t.Fatalf("LoadDocuments: %v", err)
	}
	if docs.Project != nil {
		t.Fatal("expected nil project when no project file is present")
	}
	if docs.Merged.Sagittarius != nil && docs.Merged.Sagittarius.SystemPrompt != nil {
		t.Fatal("expected no system prompt overlay when project file is absent")
	}
}

func TestDocumentsSaveProjectSystemPromptPreservesExisting(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SAGITTARIUS_HOME", home)
	workDir := t.TempDir()

	dir := filepath.Join(workDir, ".sagittarius")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	existing := `{"security":{"projectBoundary":{"enforce":true}}}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(existing), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	docs, err := LoadDocuments(workDir)
	if err != nil {
		t.Fatalf("LoadDocuments: %v", err)
	}
	s := docs.TargetSettings(ScopeProject)
	if s.Sagittarius == nil {
		s.Sagittarius = &SagittariusSettings{}
	}
	s.Sagittarius.SystemPrompt = &SagittariusSystemPromptConfig{
		Personality: PersonalityCreativeAssistant,
		Variant:     VariantFull,
	}
	if err := docs.SaveProject(); err != nil {
		t.Fatalf("SaveProject: %v", err)
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
