package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

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
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir) // Windows fallback
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

// TestActivateSkillDeclarationCatalog verifies that the declaration description
// includes sorted skills with their descriptions, while enum remains names-only.
func TestActivateSkillDeclarationCatalog(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)

	for _, s := range []struct{ name, desc string }{
		{"writer", "Writing guidance for prose"},
		{"coder", "Coding standards and best practices"},
	} {
		skillDir := filepath.Join(dir, ".sagittarius", "skills", s.name)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			t.Fatal(err)
		}
		content := "---\nname: " + s.name + "\ndescription: " + s.desc + "\n---\nBody.\n"
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	mgr := skills.NewManager(dir, true)
	if err := mgr.Discover(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	tool := NewActivateSkillTool(mgr)
	decl := tool.Declaration()

	// Enum must be sorted names only
	ns := nameSchema(t, tool)
	enum, ok := ns["enum"].([]string)
	if !ok || len(enum) != 2 || enum[0] != "coder" || enum[1] != "writer" {
		t.Fatalf("enum = %#v, want [coder, writer]", ns["enum"])
	}

	// Description must contain sorted catalog
	wantCatalog := "Available skills:\n- coder: Coding standards and best practices\n- writer: Writing guidance for prose"
	if !strings.Contains(decl.Description, wantCatalog) {
		t.Errorf("decl.Description missing catalog:\n%s\nwant:\n%s", decl.Description, wantCatalog)
	}
}

// TestActivateSkillDeclarationDescriptionCapped: descriptions exceeding 200
// runes are capped on a rune boundary with an ellipsis (...).
func TestActivateSkillDeclarationDescriptionCapped(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)

	// Create a description with multi-byte unicode characters that exceeds 200 runes.
	longDesc := strings.Repeat("日本語テスト", 50) // 50 * 6 = 300 runes
	skillDir := filepath.Join(dir, ".sagittarius", "skills", "unicode_skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: unicode_skill\ndescription: " + longDesc + "\n---\nBody.\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	mgr := skills.NewManager(dir, true)
	if err := mgr.Discover(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	tool := NewActivateSkillTool(mgr)
	decl := tool.Declaration()

	prefix := "- unicode_skill: "
	idx := strings.Index(decl.Description, prefix)
	if idx < 0 {
		t.Fatalf("decl.Description missing prefix %q:\n%s", prefix, decl.Description)
	}
	line := decl.Description[idx+len(prefix):]
	if end := strings.IndexByte(line, '\n'); end >= 0 {
		line = line[:end]
	}

	if !strings.HasSuffix(line, "...") {
		t.Errorf("line does not end with ellipsis: %q", line)
	}
	runeCount := utf8.RuneCountInString(line)
	if runeCount != maxSkillDescriptionRunes {
		t.Errorf("rune count = %d, want %d", runeCount, maxSkillDescriptionRunes)
	}
}

// TestActivateSkillDeclarationLiveReload: adding a skill and calling mgr.Discover
// reflects in Declaration() without rebuilding the tool.
func TestActivateSkillDeclarationLiveReload(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)

	skillDir1 := filepath.Join(dir, ".sagittarius", "skills", "skill1")
	if err := os.MkdirAll(skillDir1, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir1, "SKILL.md"), []byte("---\nname: skill1\ndescription: First skill\n---\nBody.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	mgr := skills.NewManager(dir, true)
	if err := mgr.Discover(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	tool := NewActivateSkillTool(mgr)
	decl1 := tool.Declaration()
	if !strings.Contains(decl1.Description, "- skill1: First skill") {
		t.Fatalf("decl1 missing skill1:\n%s", decl1.Description)
	}

	// Add second skill and reload manager
	skillDir2 := filepath.Join(dir, ".sagittarius", "skills", "skill2")
	if err := os.MkdirAll(skillDir2, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir2, "SKILL.md"), []byte("---\nname: skill2\ndescription: Second skill\n---\nBody.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Discover(t.Context(), nil); err != nil {
		t.Fatal(err)
	}

	decl2 := tool.Declaration()
	if !strings.Contains(decl2.Description, "- skill1: First skill") || !strings.Contains(decl2.Description, "- skill2: Second skill") {
		t.Fatalf("decl2 missing newly discovered skills:\n%s", decl2.Description)
	}
	ns2 := nameSchema(t, tool)
	enum2, _ := ns2["enum"].([]string)
	if len(enum2) != 2 || enum2[0] != "skill1" || enum2[1] != "skill2" {
		t.Fatalf("enum2 = %#v, want [skill1, skill2]", enum2)
	}
}
