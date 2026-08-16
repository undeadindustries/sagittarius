package atmention

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/undeadindustries/sagittarius/internal/tools"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

const (
	// maxSuggestions caps how many completion candidates are returned.
	maxSuggestions = 20
	// maxIndexEntries bounds the workspace walk so completion stays responsive in
	// very large trees.
	maxIndexEntries = 20000
	// indexTTL is how long a workspace file listing is reused before rewalking.
	indexTTL = 3 * time.Second
)

// skipDirs are directory names excluded from the completion index.
var skipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	"dist":         true,
	"build":        true,
	".sagittarius": true,
}

// Index provides "@path" and "@skill:name" completion candidates, caching the
// workspace file listing for a short interval so per-keystroke completion stays
// fast.
type Index struct {
	ws         *tools.Workspace
	skillNames func() []string

	mu         sync.Mutex
	cached     []string
	cachedAt   time.Time
	refreshing bool
}

// NewIndex builds a completion index over ws, with skillNames supplying the
// installed skill names for "@skill:" completion. Either source may be nil;
// NewIndex returns nil only when both are, so callers can treat "nothing to
// complete against" as "no completion". skillNames is called on the UI thread
// per keystroke and must not block.
func NewIndex(ws *tools.Workspace, skillNames func() []string) *Index {
	if ws == nil && skillNames == nil {
		return nil
	}
	return &Index{ws: ws, skillNames: skillNames}
}

// Complete returns file-path and skill suggestions for an active "@" token
// ending at the byte offset cursor within input. It returns no items when the
// cursor is not inside an "@" token. ReplaceFrom is the byte offset just after
// '@' (or just after "@skill:" when completing a skill name), so accepting a
// suggestion replaces the partial token with the chosen one.
func (idx *Index) Complete(input string, cursor int) ui.Completions {
	if idx == nil {
		return ui.Completions{}
	}
	b := []byte(input)
	if cursor < 0 || cursor > len(b) {
		cursor = len(b)
	}

	// Walk back to the start of the whitespace-delimited token at the cursor.
	start := cursor
	for start > 0 && !isSpaceByte(b[start-1]) {
		start--
	}
	if start >= cursor || b[start] != '@' {
		return ui.Completions{}
	}

	contentStart := start + 1
	// The raw token drives the "skill:" test and the replace offset; unescaping
	// can change the byte length, which would misplace ReplaceFrom.
	raw := string(b[contentStart:cursor])
	if hasSkillPrefix(raw) {
		return idx.completeSkills(raw[len(skillPrefix):], contentStart+len(skillPrefix))
	}

	partial := unescape(raw)
	items := make([]ui.Suggestion, 0, maxSuggestions+1)
	// Surface "skill:" while the user is still typing it (and on a bare '@'),
	// so skill mentions are discoverable without reading the docs.
	if isSkillPrefixTyped(partial) {
		items = append(items, ui.Suggestion{Label: skillPrefix, Description: "load a skill for this message", Insert: skillPrefix})
	}
	for _, m := range idx.match(partial) {
		// AppendSpace clears the suggestion list and lets the user keep typing
		// the rest of the prompt after the path is inserted.
		items = append(items, ui.Suggestion{Label: m, Insert: escape(m), AppendSpace: true})
	}
	if len(items) == 0 {
		return ui.Completions{}
	}
	return ui.Completions{Items: items, ReplaceFrom: contentStart}
}

// completeSkills matches installed skill names against the text typed after
// "@skill:". replaceFrom points just past the prefix so only the name is
// replaced.
func (idx *Index) completeSkills(partial string, replaceFrom int) ui.Completions {
	if idx.skillNames == nil {
		return ui.Completions{}
	}
	matches := matchNames(idx.skillNames(), partial)
	if len(matches) == 0 {
		return ui.Completions{}
	}
	items := make([]ui.Suggestion, 0, len(matches))
	for _, m := range matches {
		items = append(items, ui.Suggestion{Label: m, Insert: escape(m), AppendSpace: true})
	}
	return ui.Completions{Items: items, ReplaceFrom: replaceFrom}
}

// hasSkillPrefix reports whether a raw '@' token is a skill reference.
func hasSkillPrefix(raw string) bool {
	return len(raw) >= len(skillPrefix) && strings.EqualFold(raw[:len(skillPrefix)], skillPrefix)
}

// isSkillPrefixTyped reports whether partial is a (possibly empty) prefix of
// "skill:", i.e. the user could still be typing the skill marker.
func isSkillPrefixTyped(partial string) bool {
	return len(partial) < len(skillPrefix) && strings.EqualFold(skillPrefix[:len(partial)], partial)
}

// matchNames ranks names against partial: prefix matches first, then substring
// matches, each alphabetically (case-insensitively, so "6502-assembly" sorts
// before "Bash"). The comparison is case-insensitive
// because the matching is too — a user who typed lowercase expects lowercase
// results, and byte-order sorting would strand capitalized names at the top.
// All matching skills are returned so the user can scroll through the entire
// installed skill catalog in the TUI.
func matchNames(names []string, partial string) []string {
	partial = strings.ToLower(partial)
	var prefix, contains []string
	for _, n := range names {
		ln := strings.ToLower(n)
		switch {
		case partial == "" || strings.HasPrefix(ln, partial):
			prefix = append(prefix, n)
		case strings.Contains(ln, partial):
			contains = append(contains, n)
		}
	}
	sortCaseInsensitive(prefix)
	sortCaseInsensitive(contains)
	return append(prefix, contains...)
}

// sortCaseInsensitive orders strings by lowercase value, breaking ties
// byte-wise so the result is deterministic for names that differ only in case.
func sortCaseInsensitive(names []string) {
	sort.Slice(names, func(i, j int) bool {
		li, lj := strings.ToLower(names[i]), strings.ToLower(names[j])
		if li != lj {
			return li < lj
		}
		return names[i] < names[j]
	})
}

// match returns workspace-relative file paths matching partial: prefix matches
// first, then substring matches, capped at maxSuggestions.
func (idx *Index) match(partial string) []string {
	files := idx.files()
	partial = strings.ToLower(partial)

	var prefix, contains []string
	for _, f := range files {
		lf := strings.ToLower(f)
		switch {
		case partial == "":
			prefix = append(prefix, f)
		case strings.HasPrefix(lf, partial):
			prefix = append(prefix, f)
		case strings.Contains(lf, partial):
			contains = append(contains, f)
		}
		if len(prefix) >= maxSuggestions && partial == "" {
			break
		}
	}
	sortPaths(prefix)
	sortPaths(contains)
	out := append(prefix, contains...)
	if len(out) > maxSuggestions {
		out = out[:maxSuggestions]
	}
	return out
}

// files returns the current cache without blocking, kicking off a background
// refresh when the cache is stale. Callers (including the per-keystroke TUI
// completer on the Bubble Tea Update goroutine) always get an immediate, possibly
// stale, answer; the next keystroke after a refresh completes sees fresh data.
// The expensive walk runs off-lock — only the cache swap holds the lock — so a
// large tree never freezes input. First call returns nil (one frame of no
// suggestions) until the initial walk lands.
func (idx *Index) files() []string {
	if idx.ws == nil {
		return nil
	}
	idx.mu.Lock()
	cached := idx.cached
	stale := cached == nil || time.Since(idx.cachedAt) >= indexTTL
	startRefresh := stale && !idx.refreshing
	if startRefresh {
		idx.refreshing = true
	}
	idx.mu.Unlock()

	if startRefresh {
		go func() {
			files := walkFiles(idx.ws.Root())
			idx.mu.Lock()
			idx.cached = files
			idx.cachedAt = time.Now()
			idx.refreshing = false
			idx.mu.Unlock()
		}()
	}
	return cached
}

// walkFiles lists workspace-relative file paths (forward-slashed), skipping
// well-known noise directories and stopping after maxIndexEntries.
func walkFiles(root string) []string {
	var out []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != root && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		out = append(out, filepath.ToSlash(rel))
		if len(out) >= maxIndexEntries {
			return filepath.SkipAll
		}
		return nil
	})
	return out
}

// sortPaths orders paths by ascending length then lexically, so the shortest
// (usually most relevant) matches surface first.
func sortPaths(paths []string) {
	sort.Slice(paths, func(i, j int) bool {
		if len(paths[i]) != len(paths[j]) {
			return len(paths[i]) < len(paths[j])
		}
		return paths[i] < paths[j]
	})
}

func isSpaceByte(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}
