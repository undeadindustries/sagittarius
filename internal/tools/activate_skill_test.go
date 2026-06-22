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

// nameSchema extracts the "name" property schema from the declaration.
func nameSchema(t *testing.T, tool *ActivateSkillTool) map[string]any {
	t.Helper()
	decl := tool.Declaration()
	props, ok := decl.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties missing: %#v", decl.Parameters)
	}
	ns, ok := props["name"].(map[string]any)
	if !ok {
		t.Fatalf("name schema missing: %#v", props)
	}
	return ns
}

// TestActivateSkillDeclarationNoSkillsOmitsEnum guards against the Gemini/OpenRouter
// 400 ("enum[0]: cannot be empty"): with no skills the declaration must NOT emit an
// enum (matching the fork), rather than an enum containing an empty string.
func TestActivateSkillDeclarationNoSkillsOmitsEnum(t *testing.T) {
	t.Parallel()
	mgr := skills.NewManager(t.TempDir(), false)
	tool := NewActivateSkillTool(mgr)
	ns := nameSchema(t, tool)
	if _, ok := ns["enum"]; ok {
		t.Fatalf("no skills must omit enum, got %#v", ns)
	}
}

// TestActivateSkillDeclarationWithSkillsHasEnum verifies the enum lists the
// discovered skill names (no empty entries) when skills exist.
func TestActivateSkillDeclarationWithSkillsHasEnum(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	skillRoot := filepath.Join(dir, ".sagittarius", "skills", "writer")
	if err := os.MkdirAll(skillRoot, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "---\nname: writer\ndescription: Writing guidance\n---\nWrite clearly.\n"
	if err := os.WriteFile(filepath.Join(skillRoot, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	mgr := skills.NewManager(dir, true)
	if err := mgr.Discover(t.Context(), nil); err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	tool := NewActivateSkillTool(mgr)
	ns := nameSchema(t, tool)
	enum, ok := ns["enum"].([]string)
	if !ok {
		t.Fatalf("enum missing or wrong type: %#v", ns["enum"])
	}
	if len(enum) != 1 || enum[0] != "writer" {
		t.Fatalf("enum = %#v, want [writer]", enum)
	}
}
