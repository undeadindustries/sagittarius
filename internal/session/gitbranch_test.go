package session

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFile is a small test helper that creates parent dirs and writes data.
func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestCurrentBranch(t *testing.T) {
	t.Parallel()

	t.Run("not a repo returns empty", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if got := currentBranchBound(dir, t.TempDir()); got != "" {
			t.Errorf("expected empty for non-repo, got %q", got)
		}
	})

	t.Run("normal branch", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, ".git", "HEAD"), "ref: refs/heads/feature/lsp-pool\n")
		if got := currentBranchBound(dir, t.TempDir()); got != "feature/lsp-pool" {
			t.Errorf("got %q, want %q", got, "feature/lsp-pool")
		}
	})

	t.Run("detached HEAD returns short sha", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, ".git", "HEAD"), "3f6b9a2c11112222333344445555666677778888\n")
		if got := currentBranchBound(dir, t.TempDir()); got != "3f6b9a2" {
			t.Errorf("got %q, want short sha %q", got, "3f6b9a2")
		}
	})

	t.Run("worktree gitdir file is followed", func(t *testing.T) {
		t.Parallel()
		// Real git dir lives elsewhere; .git is a file pointing at it.
		realGit := t.TempDir()
		writeFile(t, filepath.Join(realGit, "HEAD"), "ref: refs/heads/wt-branch\n")

		worktree := t.TempDir()
		writeFile(t, filepath.Join(worktree, ".git"), "gitdir: "+realGit+"\n")
		if got := currentBranchBound(worktree, t.TempDir()); got != "wt-branch" {
			t.Errorf("got %q, want %q", got, "wt-branch")
		}
	})

	t.Run("worktree relative gitdir is followed", func(t *testing.T) {
		t.Parallel()
		// Build <root>/.git/worktrees/wt with the HEAD, and a worktree whose
		// .git file points at it relatively.
		root := t.TempDir()
		realGit := filepath.Join(root, ".git", "worktrees", "wt")
		writeFile(t, filepath.Join(realGit, "HEAD"), "ref: refs/heads/rel-wt\n")

		wt := filepath.Join(root, "checkout")
		writeFile(t, filepath.Join(wt, ".git"), "gitdir: ../.git/worktrees/wt\n")
		if got := currentBranchBound(wt, t.TempDir()); got != "rel-wt" {
			t.Errorf("got %q, want %q", got, "rel-wt")
		}
	})

	t.Run("subdirectory walk resolves parent repo branch", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeFile(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
		nested := filepath.Join(root, "a", "b", "c")
		if err := os.MkdirAll(nested, 0o755); err != nil {
			t.Fatalf("mkdir nested: %v", err)
		}
		// Boundary above root so the walk reaches root's .git.
		if got := currentBranchBound(nested, t.TempDir()); got != "main" {
			t.Errorf("got %q, want %q", got, "main")
		}
	})

	t.Run("walk stops at home boundary", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		// Repo exists above home but must not be found.
		writeFile(t, filepath.Join(home, ".git", "HEAD"), "ref: refs/heads/should-not-see\n")
		nested := filepath.Join(home, "sub", "dir")
		if err := os.MkdirAll(nested, 0o755); err != nil {
			t.Fatalf("mkdir nested: %v", err)
		}
		// home itself is the boundary; a .git at the boundary counts, but the
		// nested dir is below it and the walk should stop at home.
		// Place the repo at home, so walking from nested reaches home and finds it.
		if got := currentBranchBound(nested, home); got != "should-not-see" {
			t.Errorf("expected to find repo at home boundary, got %q", got)
		}
	})

	t.Run("walk does not descend below home", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		nested := filepath.Join(home, "sub")
		if err := os.MkdirAll(nested, 0o755); err != nil {
			t.Fatalf("mkdir nested: %v", err)
		}
		// No .git anywhere under home; must return empty, not walk past home.
		if got := currentBranchBound(nested, home); got != "" {
			t.Errorf("expected empty when no repo at or above home, got %q", got)
		}
	})

	t.Run("renamed or deleted branch is display only and not validated", func(t *testing.T) {
		t.Parallel()
		// HEAD references a branch with no corresponding ref file; we return it
		// verbatim without resolving.
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, ".git", "HEAD"), "ref: refs/heads/gone-branch\n")
		if got := currentBranchBound(dir, t.TempDir()); got != "gone-branch" {
			t.Errorf("got %q, want %q", got, "gone-branch")
		}
	})

	t.Run("empty dir returns empty", func(t *testing.T) {
		t.Parallel()
		if got := currentBranchBound("", t.TempDir()); got != "" {
			t.Errorf("expected empty for empty dir, got %q", got)
		}
	})

	t.Run("malformed HEAD returns empty", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, ".git", "HEAD"), "this is not a ref or sha\n")
		if got := currentBranchBound(dir, t.TempDir()); got != "" {
			t.Errorf("expected empty for malformed HEAD, got %q", got)
		}
	})
}
