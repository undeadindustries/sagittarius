package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureGlobalHomeCreatesDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SAGITTARIUS_HOME", home)

	dir, err := EnsureGlobalHome()
	if err != nil {
		t.Fatalf("EnsureGlobalHome: %v", err)
	}
	want := filepath.Join(home, ".sagittarius")
	if dir != want {
		t.Fatalf("dir = %q, want %q", dir, want)
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Fatalf("expected directory at %q, stat err=%v", dir, err)
	}

	// Idempotent: a second call must not error.
	if _, err := EnsureGlobalHome(); err != nil {
		t.Fatalf("second EnsureGlobalHome: %v", err)
	}
}

func TestProjectSlugCreatesMarkers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SAGITTARIUS_HOME", home)
	if _, err := EnsureGlobalHome(); err != nil {
		t.Fatalf("EnsureGlobalHome: %v", err)
	}

	projectRoot := filepath.Join(home, "work", "my-cool-project")
	slug, err := ProjectSlug(projectRoot)
	if err != nil {
		t.Fatalf("ProjectSlug: %v", err)
	}
	if slug != "my-cool-project" {
		t.Fatalf("slug = %q, want my-cool-project", slug)
	}

	root := filepath.Join(home, ".sagittarius")
	for _, base := range []string{"tmp", "history"} {
		marker := filepath.Join(root, base, slug, ".project_root")
		raw, err := os.ReadFile(marker)
		if err != nil {
			t.Fatalf("read marker %q: %v", marker, err)
		}
		abs, _ := filepath.Abs(projectRoot)
		if string(raw) != abs {
			t.Errorf("marker %q = %q, want %q", marker, string(raw), abs)
		}
	}

	// projects.json records the mapping.
	if _, err := os.Stat(filepath.Join(root, "projects.json")); err != nil {
		t.Fatalf("projects.json missing: %v", err)
	}
}

func TestProjectSlugStableAndIdempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SAGITTARIUS_HOME", home)

	projectRoot := filepath.Join(home, "repo")
	first, err := ProjectSlug(projectRoot)
	if err != nil {
		t.Fatalf("ProjectSlug first: %v", err)
	}
	second, err := ProjectSlug(projectRoot)
	if err != nil {
		t.Fatalf("ProjectSlug second: %v", err)
	}
	if first != second {
		t.Fatalf("slug not stable: %q vs %q", first, second)
	}
}

func TestProjectSlugCollisionGetsSuffix(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SAGITTARIUS_HOME", home)

	a := filepath.Join(home, "a", "shared")
	b := filepath.Join(home, "b", "shared")
	slugA, err := ProjectSlug(a)
	if err != nil {
		t.Fatalf("ProjectSlug a: %v", err)
	}
	slugB, err := ProjectSlug(b)
	if err != nil {
		t.Fatalf("ProjectSlug b: %v", err)
	}
	if slugA == slugB {
		t.Fatalf("expected distinct slugs for distinct roots with same basename, got %q twice", slugA)
	}
	if slugA != "shared" || slugB != "shared-1" {
		t.Fatalf("slugs = %q, %q; want shared, shared-1", slugA, slugB)
	}
}
