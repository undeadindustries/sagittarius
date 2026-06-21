package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSkillDiscovery(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	skillDir := filepath.Join(dir, "demo-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := `---
name: demo-skill
description: A demo skill for tests
---
Do the demo thing.
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	defs, err := LoadFromDir(dir)
	if err != nil {
		t.Fatalf("LoadFromDir() error = %v", err)
	}
	if len(defs) != 1 {
		t.Fatalf("LoadFromDir() len = %d, want 1", len(defs))
	}
	if defs[0].Name != "demo-skill" {
		t.Fatalf("Name = %q, want demo-skill", defs[0].Name)
	}
	if defs[0].Body != "Do the demo thing." {
		t.Fatalf("Body = %q", defs[0].Body)
	}
}
