package diff

import (
	"strings"
)

// LooksLikeUnifiedDiff reports whether text appears to be a unified diff or a
// pseudo-diff edit (interleaved +/- lines). Models sometimes send diff hunks or
// confirmation previews instead of complete file contents to write_file.
func LooksLikeUnifiedDiff(text string) bool {
	if strings.HasPrefix(text, "--- ") || strings.Contains(text, "\n--- a/") {
		return true
	}
	if strings.HasPrefix(text, "@@") || strings.Contains(text, "\n@@ ") {
		return true
	}
	adds, dels := countDiffPrefixLines(text)
	if adds >= 1 && dels >= 1 {
		return true
	}
	// Many single-sided +/- lines (common pseudo-diff edits).
	if adds+dels >= 4 {
		return true
	}
	return false
}

// LooksLikeEjectionMarker reports content that matches the context-management
// write_file ejection tag models sometimes copy from history into write_file.
func LooksLikeEjectionMarker(text string) bool {
	trimmed := strings.TrimSpace(text)
	return strings.HasPrefix(trimmed, "[sagittarius omitted write_file content") ||
		strings.HasPrefix(trimmed, "<file_written ") ||
		strings.HasPrefix(trimmed, "<file_written>")
}

// LooksLikePlaceholderContent reports common placeholder elisions models use
// instead of supplying the full file body to write_file.
func LooksLikePlaceholderContent(text string) bool {
	lower := strings.ToLower(text)
	for _, p := range []string{
		"... existing code ...",
		"...existing code...",
		"// ... existing",
		"# ... existing",
		"/* ... existing",
		"<!-- ... existing",
	} {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

func countDiffPrefixLines(text string) (adds, dels int) {
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if len(trimmed) < 2 {
			continue
		}
		switch trimmed[0] {
		case '+':
			adds++
		case '-':
			dels++
		}
	}
	return adds, dels
}
