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

// Index provides "@path" completion candidates from a workspace, caching the
// file listing for a short interval so per-keystroke completion stays fast.
type Index struct {
	ws *tools.Workspace

	mu       sync.Mutex
	cached   []string
	cachedAt time.Time
}

// NewIndex builds a completion index over ws. It returns nil when ws is nil so
// callers can treat "no workspace" as "no completion".
func NewIndex(ws *tools.Workspace) *Index {
	if ws == nil {
		return nil
	}
	return &Index{ws: ws}
}

// Complete returns file-path suggestions for an active "@" token ending at the
// byte offset cursor within input. It returns no items when the cursor is not
// inside an "@" token. ReplaceFrom is the byte offset just after '@', so
// accepting a suggestion replaces the partial path with the chosen one.
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
	partial := unescape(string(b[contentStart:cursor]))
	matches := idx.match(partial)
	if len(matches) == 0 {
		return ui.Completions{}
	}

	items := make([]ui.Suggestion, 0, len(matches))
	for _, m := range matches {
		// AppendSpace clears the suggestion list and lets the user keep typing
		// the rest of the prompt after the path is inserted.
		items = append(items, ui.Suggestion{Label: m, Insert: escape(m), AppendSpace: true})
	}
	return ui.Completions{Items: items, ReplaceFrom: contentStart}
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

// files returns the cached workspace file listing, rewalking when the cache has
// expired.
func (idx *Index) files() []string {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if idx.cached != nil && time.Since(idx.cachedAt) < indexTTL {
		return idx.cached
	}
	idx.cached = walkFiles(idx.ws.Root())
	idx.cachedAt = time.Now()
	return idx.cached
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
