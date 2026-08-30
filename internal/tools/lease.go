package tools

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

const (
	// maxLeasePatterns bounds a single lease so a model cannot submit a
	// pattern set large enough to make overlap checking expensive.
	maxLeasePatterns = 64
	// maxLeaseSegments bounds one pattern's depth. Overlap checking walks
	// both patterns recursively and "**" branches, so unbounded depth would
	// be a denial-of-service vector.
	maxLeaseSegments = 32
	// recursiveSegment matches zero or more path segments.
	recursiveSegment = "**"
)

// WriteLease is the set of workspace-relative path patterns a subagent may
// modify. It is immutable once parsed and every method is pure.
//
// Patterns use slash separators and support the shell globbing that
// path.Match provides per segment, plus "**" to match zero or more segments.
// A pattern ending in "/" is expanded to "<pattern>/**", so "tests/" and
// "tests/**" are equivalent; a bare "tests" matches only a file named tests.
type WriteLease struct {
	Patterns []string
}

// ParseWriteLease validates and normalizes raw lease patterns coming off a
// tool call. It accepts []any (the shape JSON decoding produces) or []string.
//
// An empty lease is rejected: a coding subagent with nothing to write has no
// reason to exist, and an accidental empty list would otherwise read as
// "denies everything" only after the child had already been launched.
func ParseWriteLease(raw any) (WriteLease, error) {
	items, err := leaseStrings(raw)
	if err != nil {
		return WriteLease{}, err
	}
	if len(items) == 0 {
		return WriteLease{}, fmt.Errorf("write lease is empty: list the paths this subagent may modify")
	}
	if len(items) > maxLeasePatterns {
		return WriteLease{}, fmt.Errorf("write lease has %d patterns, limit is %d", len(items), maxLeasePatterns)
	}
	seen := make(map[string]struct{}, len(items))
	patterns := make([]string, 0, len(items))
	for _, item := range items {
		p, err := normalizeLeasePattern(item)
		if err != nil {
			return WriteLease{}, err
		}
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		patterns = append(patterns, p)
	}
	return WriteLease{Patterns: patterns}, nil
}

// Allows reports whether relPath, a slash-separated workspace-relative path,
// falls inside the lease.
func (l WriteLease) Allows(relPath string) bool {
	target := path.Clean(filepath.ToSlash(strings.TrimSpace(relPath)))
	if target == "" || target == "." {
		return false
	}
	target = strings.TrimPrefix(target, "./")
	segs := strings.Split(target, "/")
	for _, p := range l.Patterns {
		if matchSegments(strings.Split(p, "/"), segs) {
			return true
		}
	}
	return false
}

// String renders the lease for an error message or a confirmation card.
func (l WriteLease) String() string { return strings.Join(l.Patterns, ", ") }

// LeasesOverlap reports whether any path could be matched by both leases,
// returning the first colliding pattern pair for the error message.
//
// The check is conservative: when both sides of a segment are globs their
// intersection is not computed and they are treated as overlapping. Failing
// closed costs a rejected subagent batch; failing open costs two agents
// writing the same file.
func LeasesOverlap(a, b WriteLease) (string, bool) {
	for _, pa := range a.Patterns {
		for _, pb := range b.Patterns {
			if patternsOverlap(strings.Split(pa, "/"), strings.Split(pb, "/")) {
				return fmt.Sprintf("%s and %s", pa, pb), true
			}
		}
	}
	return "", false
}

func leaseStrings(raw any) ([]string, error) {
	switch v := raw.(type) {
	case nil:
		return nil, fmt.Errorf("write lease is missing")
	case []string:
		return v, nil
	case []any:
		out := make([]string, 0, len(v))
		for i, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("write lease entry %d is %T, want string", i, item)
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("write lease is %T, want a list of strings", raw)
	}
}

func normalizeLeasePattern(raw string) (string, error) {
	p := filepath.ToSlash(strings.TrimSpace(raw))
	if p == "" {
		return "", fmt.Errorf("write lease contains an empty pattern")
	}
	if strings.HasPrefix(p, "/") || filepath.IsAbs(raw) {
		return "", fmt.Errorf("write lease pattern %q is absolute: use workspace-relative paths", raw)
	}
	dir := strings.HasSuffix(p, "/")
	p = strings.TrimSuffix(p, "/")
	p = strings.TrimPrefix(p, "./")
	if p == "" || p == "." {
		return "", fmt.Errorf("write lease pattern %q does not name anything", raw)
	}
	segs := strings.Split(p, "/")
	if len(segs) > maxLeaseSegments {
		return "", fmt.Errorf("write lease pattern %q is %d segments deep, limit is %d", raw, len(segs), maxLeaseSegments)
	}
	for _, seg := range segs {
		if seg == ".." {
			return "", fmt.Errorf("write lease pattern %q escapes the workspace", raw)
		}
		if seg == "" {
			return "", fmt.Errorf("write lease pattern %q has an empty segment", raw)
		}
		if seg == recursiveSegment {
			continue
		}
		if _, err := path.Match(seg, ""); err != nil {
			return "", fmt.Errorf("write lease pattern %q is malformed: %w", raw, err)
		}
	}
	if dir {
		return p + "/" + recursiveSegment, nil
	}
	return p, nil
}

// matchSegments reports whether a pattern's segments match a concrete path's
// segments, with "**" consuming zero or more.
func matchSegments(pattern, target []string) bool {
	if len(pattern) == 0 {
		return len(target) == 0
	}
	if pattern[0] == recursiveSegment {
		if matchSegments(pattern[1:], target) {
			return true
		}
		if len(target) == 0 {
			return false
		}
		return matchSegments(pattern, target[1:])
	}
	if len(target) == 0 {
		return false
	}
	ok, err := path.Match(pattern[0], target[0])
	if err != nil || !ok {
		return false
	}
	return matchSegments(pattern[1:], target[1:])
}

// patternsOverlap reports whether some concrete path could match both patterns.
func patternsOverlap(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	if len(a) == 0 {
		return allRecursive(b)
	}
	if len(b) == 0 {
		return allRecursive(a)
	}
	if a[0] == recursiveSegment {
		return patternsOverlap(a[1:], b) || patternsOverlap(a, b[1:])
	}
	if b[0] == recursiveSegment {
		return patternsOverlap(a, b[1:]) || patternsOverlap(a[1:], b)
	}
	if !segmentsOverlap(a[0], b[0]) {
		return false
	}
	return patternsOverlap(a[1:], b[1:])
}

func allRecursive(segs []string) bool {
	for _, s := range segs {
		if s != recursiveSegment {
			return false
		}
	}
	return true
}

func segmentsOverlap(a, b string) bool {
	if a == b {
		return true
	}
	ag, bg := isGlobSegment(a), isGlobSegment(b)
	switch {
	case ag && bg:
		// Two globs: intersection is not computed, so fail closed.
		return true
	case ag:
		ok, err := path.Match(a, b)
		return err != nil || ok
	case bg:
		ok, err := path.Match(b, a)
		return err != nil || ok
	default:
		return false
	}
}

func isGlobSegment(s string) bool {
	return strings.ContainsAny(s, "*?[")
}
