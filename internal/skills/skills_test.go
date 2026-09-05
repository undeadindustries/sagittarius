package skills

import (
	"os"
	"path/filepath"
	"testing"
)

// writeSkill creates dir and a valid SKILL.md inside it named name.
func writeSkill(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", dir, err)
	}
	content := "---\nname: " + name + "\ndescription: A demo skill for tests\n---\nDo the demo thing.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write skill %q: %v", name, err)
	}
}

func TestSkillDiscovery(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSkill(t, filepath.Join(dir, "demo-skill"), "demo-skill")

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

// TestSkillDiscoveryFollowsDirectorySymlink is the regression for skills
// installed by linking a directory into the skills root, e.g.
// ~/.sagittarius/skills/golang -> ~/.cursor/skills/golang. os.ReadDir reports
// such an entry as ModeSymlink, so an entry.IsDir() filter dropped it.
func TestSkillDiscoveryFollowsDirectorySymlink(t *testing.T) {
	t.Parallel()

	target := filepath.Join(t.TempDir(), "linked-skill")
	writeSkill(t, target, "linked-skill")

	root := t.TempDir()
	if err := os.Symlink(target, filepath.Join(root, "linked-skill")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	defs, err := LoadFromDir(root)
	if err != nil {
		t.Fatalf("LoadFromDir() error = %v", err)
	}
	if len(defs) != 1 {
		t.Fatalf("LoadFromDir() len = %d, want 1 (symlinked skill dir was skipped)", len(defs))
	}
	if defs[0].Name != "linked-skill" {
		t.Fatalf("Name = %q, want linked-skill", defs[0].Name)
	}
}

// TestSkillDiscoverySkipsNonDirectoryEntries pins the invariant that the
// removed entry.IsDir() filter nominally provided: entries that cannot hold a
// SKILL.md are still ignored, because the os.Stat on the candidate path fails.
func TestSkillDiscoverySkipsNonDirectoryEntries(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "real-skill"), "real-skill")

	stray := filepath.Join(root, "notes.txt")
	if err := os.WriteFile(stray, []byte("not a skill"), 0o644); err != nil {
		t.Fatalf("write stray file: %v", err)
	}
	if err := os.Symlink(stray, filepath.Join(root, "notes-link")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "gone"), filepath.Join(root, "broken-link")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	defs, err := LoadFromDir(root)
	if err != nil {
		t.Fatalf("LoadFromDir() error = %v", err)
	}
	if len(defs) != 1 {
		t.Fatalf("LoadFromDir() len = %d, want 1", len(defs))
	}
	if defs[0].Name != "real-skill" {
		t.Fatalf("Name = %q, want real-skill", defs[0].Name)
	}
}
