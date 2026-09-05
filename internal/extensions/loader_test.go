package extensions

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
)

// writeExtension creates dir and a minimal gemini-extension.json inside it.
func writeExtension(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", dir, err)
	}
	manifest := `{"name":"` + name + `","version":"1.2.3"}`
	if err := os.WriteFile(filepath.Join(dir, "gemini-extension.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest %q: %v", name, err)
	}
}

// extensionsRoot points config.ResolveSagittariusDir at a temp home and returns
// the created <home>/.sagittarius/extensions directory.
func extensionsRoot(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SAGITTARIUS_HOME", home)
	root := filepath.Join(home, config.SagittariusDir, "extensions")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir extensions root: %v", err)
	}
	return root
}

// TestDiscoverInstalledExtensionsFollowsDirectorySymlink is the regression for
// an extension installed by linking its directory into
// ~/.sagittarius/extensions. os.ReadDir reports such an entry as ModeSymlink,
// so an entry.IsDir() filter dropped it.
func TestDiscoverInstalledExtensionsFollowsDirectorySymlink(t *testing.T) {
	target := filepath.Join(t.TempDir(), "demo-ext")
	writeExtension(t, target, "demo-ext")

	root := extensionsRoot(t)
	if err := os.Symlink(target, filepath.Join(root, "demo-ext")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	found, err := discoverInstalledExtensions()
	if err != nil {
		t.Fatalf("discoverInstalledExtensions() error = %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("len = %d, want 1 (symlinked extension dir was skipped)", len(found))
	}
	if found[0].Name != "demo-ext" {
		t.Fatalf("Name = %q, want demo-ext", found[0].Name)
	}
	if found[0].Version != "1.2.3" {
		t.Fatalf("Version = %q, want 1.2.3", found[0].Version)
	}
}

// TestDiscoverInstalledExtensionsSkipsNonDirectoryEntries pins the invariant
// that the removed entry.IsDir() filter nominally provided: an entry with no
// readable manifest under it is still ignored.
func TestDiscoverInstalledExtensionsSkipsNonDirectoryEntries(t *testing.T) {
	root := extensionsRoot(t)
	writeExtension(t, filepath.Join(root, "real-ext"), "real-ext")

	stray := filepath.Join(root, "notes.txt")
	if err := os.WriteFile(stray, []byte("not an extension"), 0o644); err != nil {
		t.Fatalf("write stray file: %v", err)
	}
	if err := os.Symlink(stray, filepath.Join(root, "notes-link")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "gone"), filepath.Join(root, "broken-link")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	found, err := discoverInstalledExtensions()
	if err != nil {
		t.Fatalf("discoverInstalledExtensions() error = %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("len = %d, want 1", len(found))
	}
	if found[0].Name != "real-ext" {
		t.Fatalf("Name = %q, want real-ext", found[0].Name)
	}
}

// TestDiscoverInstalledExtensionsFindsLinkedSkills covers the composed path: a
// symlinked extension whose own skills/ directory is discovered through it.
func TestDiscoverInstalledExtensionsFindsLinkedSkills(t *testing.T) {
	target := filepath.Join(t.TempDir(), "skilled-ext")
	writeExtension(t, target, "skilled-ext")

	skillDir := filepath.Join(target, "skills", "ext-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	skill := "---\nname: ext-skill\ndescription: A skill shipped by an extension\n---\nDo the extension thing.\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skill), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	root := extensionsRoot(t)
	if err := os.Symlink(target, filepath.Join(root, "skilled-ext")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	found, err := discoverInstalledExtensions()
	if err != nil {
		t.Fatalf("discoverInstalledExtensions() error = %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("len = %d, want 1", len(found))
	}
	if len(found[0].Skills) != 1 {
		t.Fatalf("Skills len = %d, want 1", len(found[0].Skills))
	}
	if found[0].Skills[0].Name != "ext-skill" {
		t.Fatalf("skill Name = %q, want ext-skill", found[0].Skills[0].Name)
	}
}
