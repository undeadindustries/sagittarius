package tools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/skills"
)

func TestActivateSkillTool(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	skillRoot := filepath.Join(dir, ".sagittarius", "skills", "writer")
	if err := os.MkdirAll(skillRoot, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := `---
name: writer
description: Writing guidance
---
Write clearly.
`
	if err := os.WriteFile(filepath.Join(skillRoot, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	mgr := skills.NewManager(dir, true)
	if err := mgr.Discover(t.Context(), nil); err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	tool := NewActivateSkillTool(mgr)
	result, err := tool.Execute(t.Context(), map[string]any{"name": "writer"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if _, ok := result["error"]; ok {
		t.Fatalf("Execute() error result = %v", result["error"])
	}
	text, ok := result["result"].(string)
	if !ok || text == "" {
		t.Fatalf("Execute() result = %#v, want activated skill XML", result)
	}
	if !mgr.IsActive("writer") {
		t.Fatal("expected writer skill to be active")
	}
}
