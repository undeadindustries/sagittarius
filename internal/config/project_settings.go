package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MergeProjectSystemPrompt overlays sagittarius.systemPrompt from the project
// settings file onto the in-memory global settings. Project values win over the
// global ~/.sagittarius default for the current workspace.
func MergeProjectSystemPrompt(global *Settings, workDir string) error {
	if global == nil {
		return nil
	}
	project, err := LoadProjectSettings(workDir)
	if err != nil {
		return err
	}
	if project == nil {
		return nil
	}
	overlayProjectSystemPrompt(global, project)
	return nil
}

func overlayProjectSystemPrompt(dst, src *Settings) {
	if dst == nil || src == nil || src.Sagittarius == nil || src.Sagittarius.SystemPrompt == nil {
		return
	}
	if dst.Sagittarius == nil {
		dst.Sagittarius = &SagittariusSettings{}
	}
	sp := *src.Sagittarius.SystemPrompt
	dst.Sagittarius.SystemPrompt = &sp
}

// SaveProjectSystemPrompt writes personality + variant to
// <workDir>/.sagittarius/settings.json, merging with any existing project
// settings on disk.
func SaveProjectSystemPrompt(workDir, personality, variant string) error {
	workDir = filepath.Clean(workDir)
	if workDir == "" {
		return fmt.Errorf("save project system prompt: working directory is required")
	}
	personality = CanonicalPersonalityID(personality)
	variant = CanonicalVariant(variant)

	existing, err := LoadProjectSettings(workDir)
	if err != nil {
		return err
	}
	s := existing
	if s == nil {
		s = &Settings{Sagittarius: &SagittariusSettings{}}
	}
	if s.Sagittarius == nil {
		s.Sagittarius = &SagittariusSettings{}
	}
	s.Sagittarius.SystemPrompt = &SagittariusSystemPromptConfig{
		Personality: personality,
		Variant:     variant,
	}
	if err := ValidateSagittariusSettings(s.Sagittarius); err != nil {
		return err
	}
	return writeProjectSettings(workDir, s)
}

// ProjectSystemPromptPresetID returns the preset id matching an explicitly
// configured sagittarius.systemPrompt, or "" when unset.
func ProjectSystemPromptPresetID(settings *Settings) string {
	gp := globalSystemPrompt(settings)
	if gp == nil || (strings.TrimSpace(gp.Personality) == "" && strings.TrimSpace(gp.Variant) == "") {
		return ""
	}
	personality := gp.Personality
	variant := gp.Variant
	if strings.TrimSpace(personality) == "" {
		personality = PersonalityProgrammer
	}
	if strings.TrimSpace(variant) == "" {
		variant = VariantFull
	}
	if p, ok := PresetForPersonalityVariant(personality, variant); ok {
		return p.ID
	}
	return ""
}

// CanonicalPersonalityID returns the canonical personality or programmer default.
func CanonicalPersonalityID(id string) string {
	if canon, ok := CanonicalPersonality(id); ok {
		return canon
	}
	return PersonalityProgrammer
}

func writeProjectSettings(workDir string, s *Settings) error {
	path := ResolveProjectSettingsPath(workDir)
	out, err := encodeSettingsDocument(s)
	if err != nil {
		return err
	}
	found, err := FindSecretFields(out)
	if err != nil {
		return err
	}
	if len(found) > 0 {
		return fmt.Errorf("%w: %v", ErrSecretsInSettings, found)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create project settings dir %q: %w", dir, err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return fmt.Errorf("write temp project settings %q: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename project settings file: %w", err)
	}
	return nil
}
