package session

import (
	"os"
	"path/filepath"
	"strings"
)

// CurrentBranch resolves the git branch for dir by reading .git/HEAD directly.
// It never shells out to git and never returns an error: any failure mode
// (not a repo, unreadable file, unexpected contents) yields "" so a non-git
// workspace is a silent first-class state rather than an error path. The value
// is display-only; callers must not validate or resolve anything through it.
//
// Handled cases:
//   - not a repo: no .git found walking up from dir -> "".
//   - .git is a file (worktree / submodule): contains "gitdir: <path>", follow one level.
//   - detached HEAD: HEAD holds a raw commit SHA -> return the short SHA.
//   - normal branch: HEAD holds "ref: refs/heads/<branch>" -> return <branch>.
//
// The upward walk is bounded by the user's home directory, matching the
// AGENTS.md memory walk's existing boundary rule, so launching in a repo
// subdirectory resolves the parent repo's branch.
func CurrentBranch(dir string) string {
	home, _ := os.UserHomeDir()
	return currentBranchBound(dir, home)
}

// currentBranchBound walks up from dir looking for .git, stopping at (and not
// descending below) home. Separated from CurrentBranch so tests can pin the
// boundary without touching the real home directory.
func currentBranchBound(dir, home string) string {
	if dir == "" {
		return ""
	}
	dir = filepath.Clean(dir)
	for {
		if gitDir, ok := resolveGitDir(filepath.Join(dir, ".git")); ok {
			if branch := readHeadBranch(filepath.Join(gitDir, "HEAD")); branch != "" {
				return branch
			}
		}
		if dir == home {
			return ""
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// resolveGitDir returns the real git directory for a .git path. When .git is a
// directory it is returned as-is; when it is a file (worktree / submodule) its
// "gitdir: <target>" pointer is followed one level. ok is false when neither
// form is present.
func resolveGitDir(dotGit string) (string, bool) {
	info, err := os.Stat(dotGit)
	if err != nil {
		return "", false
	}
	if info.IsDir() {
		return dotGit, true
	}
	// .git file form: "gitdir: <path>".
	raw, err := os.ReadFile(dotGit)
	if err != nil {
		return "", false
	}
	target := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(raw)), "gitdir:"))
	if target == "" {
		return "", false
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(dotGit), target)
	}
	if st, err := os.Stat(target); err == nil && st.IsDir() {
		return filepath.Clean(target), true
	}
	return "", false
}

// readHeadBranch parses a HEAD file's contents into a branch name, a short SHA
// for a detached HEAD, or "" when neither can be determined.
func readHeadBranch(headPath string) string {
	raw, err := os.ReadFile(headPath)
	if err != nil {
		return ""
	}
	head := strings.TrimSpace(string(raw))
	if head == "" {
		return ""
	}
	const refPrefix = "ref: refs/heads/"
	if strings.HasPrefix(head, refPrefix) {
		branch := strings.TrimPrefix(head, refPrefix)
		return strings.TrimSpace(branch)
	}
	// Detached HEAD: a bare commit object name (typically a 40-char hex SHA).
	// Show the short form; anything that is not a plausible SHA yields "".
	if isHex(head) {
		if len(head) > 7 {
			return head[:7]
		}
		return head
	}
	return ""
}

// isHex reports whether s consists solely of lowercase hex digits, as a commit
// SHA does. It intentionally rejects empty strings.
func isHex(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		isDigit := c >= '0' && c <= '9'
		isLower := c >= 'a' && c <= 'f'
		if !isDigit && !isLower {
			return false
		}
	}
	return true
}
