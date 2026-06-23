package snapshot

import "github.com/undeadindustries/sagittarius/internal/diff"

// UnifiedDiff renders a git-style unified diff of before -> after for path.
// It returns "" when the two inputs are identical. The implementation lives in
// the leaf internal/diff package so it can be shared with internal/tools
// without coupling the two packages; this wrapper preserves the existing
// snapshot API used by /diff and /undo.
func UnifiedDiff(before, after, path string) string {
	return diff.UnifiedDiff(before, after, path)
}
