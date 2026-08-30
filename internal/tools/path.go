package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	maxPathLength      = 4096
	maxComponentLength = 255
)

var (
	invalidPathCharsRE = regexp.MustCompile(`[\n\r\0\t]`)
	logMarkerREs       = []*regexp.Regexp{
		regexp.MustCompile(`(^|[/\\])AssertionError:`),
		regexp.MustCompile(`(^|[/\\])FAIL `),
		regexp.MustCompile(`(^|[/\\])✓ `),
		regexp.MustCompile(`(^|[/\\])× `),
		regexp.MustCompile(`(^|[/\\])TestingLibraryElementError:`),
	}
)

// Workspace is the trusted root directory for path validation.
type Workspace struct {
	root string
}

// NewWorkspace resolves and validates workDir as the trusted workspace root.
func NewWorkspace(workDir string) (*Workspace, error) {
	workDir = strings.TrimSpace(workDir)
	if workDir == "" {
		return nil, fmt.Errorf("workspace: work directory is required")
	}
	abs, err := filepath.Abs(workDir)
	if err != nil {
		return nil, fmt.Errorf("workspace: resolve path: %w", err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("workspace: eval symlinks: %w", err)
		}
		real = abs
	}
	info, err := os.Stat(real)
	if err != nil {
		return nil, fmt.Errorf("workspace: stat root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("workspace: %s is not a directory", real)
	}
	return &Workspace{root: real}, nil
}

// Root returns the absolute trusted workspace path.
func (w *Workspace) Root() string {
	return w.root
}

// ResolvePath resolves a user-supplied path relative to the workspace root.
// Existing paths and existing ancestor directories of new paths are
// canonicalized with EvalSymlinks so symlink targets are verified against the
// workspace root and lease boundaries.
func (w *Workspace) ResolvePath(pathStr string) (string, error) {
	if err := validatePathString(pathStr); err != nil {
		return "", err
	}

	abs := pathStr
	if !filepath.IsAbs(pathStr) {
		abs = filepath.Join(w.root, pathStr)
	}
	abs = filepath.Clean(abs)

	if !w.isWithinRoot(abs) {
		return "", fmt.Errorf("path %q is outside the trusted workspace", pathStr)
	}

	real, err := resolveExistingPrefix(abs)
	if err != nil {
		return "", fmt.Errorf("resolve symlinks: %w", err)
	}

	if !w.isWithinRoot(real) {
		return "", fmt.Errorf("path %q resolves outside the trusted workspace", pathStr)
	}

	return real, nil
}

// resolveExistingPrefix canonicalizes all existing path components using EvalSymlinks.
// For paths where the target does not exist, it finds the deepest existing ancestor,
// resolves symlinks on that ancestor, appends the remaining non-existent components,
// and repeats until all existing symlink components have been fully resolved.
func resolveExistingPrefix(abs string) (string, error) {
	current := abs
	for range 255 {
		next, changed, err := resolveOneExistingPrefix(current)
		if err != nil {
			return "", err
		}
		if !changed {
			return next, nil
		}
		current = next
	}
	return "", fmt.Errorf("excessive symlink hops or path resolution loop in %q", abs)
}

func resolveOneExistingPrefix(target string) (string, bool, error) {
	cur := target
	var rest string
	for {
		if _, err := os.Lstat(cur); err == nil {
			realCur, err := filepath.EvalSymlinks(cur)
			if err != nil {
				return "", false, err
			}
			var next string
			if rest == "" {
				next = realCur
			} else {
				next = filepath.Clean(filepath.Join(realCur, rest))
			}
			return next, next != target, nil
		}
		parent := filepath.Dir(cur)
		base := filepath.Base(cur)
		if parent == cur {
			return target, false, nil
		}
		if rest == "" {
			rest = base
		} else {
			rest = filepath.Join(base, rest)
		}
		cur = parent
	}
}

// RelativePath resolves a user-supplied path and returns it relative to the
// workspace root with slash separators. Anything outside the workspace is an
// error, so a caller matching against workspace-relative patterns never has to
// reason about absolute or traversing input.
func (w *Workspace) RelativePath(pathStr string) (string, error) {
	abs, err := w.ResolvePath(pathStr)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(w.root, abs)
	if err != nil {
		return "", fmt.Errorf("relativize %q: %w", pathStr, err)
	}
	return filepath.ToSlash(rel), nil
}

func (w *Workspace) isWithinRoot(pathToCheck string) bool {
	rel, err := filepath.Rel(w.root, pathToCheck)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}

func validatePathString(pathStr string) error {
	pathStr = strings.TrimSpace(pathStr)
	if pathStr == "" {
		return fmt.Errorf("path must be a non-empty string")
	}
	if invalidPathCharsRE.MatchString(pathStr) {
		return fmt.Errorf("path contains invalid characters (newlines or control characters)")
	}
	for _, re := range logMarkerREs {
		if re.MatchString(pathStr) {
			return fmt.Errorf("path appears to be a misinterpreted log fragment")
		}
	}
	if strings.Contains(pathStr, "\"") || strings.Contains(pathStr, "...") {
		if len(pathStr) > 20 {
			return fmt.Errorf("path contains suspicious characters and is too long to be a simple filename")
		}
	}
	if len(pathStr) > maxPathLength {
		return fmt.Errorf("path is too long (maximum %d characters)", maxPathLength)
	}
	for _, component := range splitPathComponents(pathStr) {
		if len(component) > maxComponentLength {
			return fmt.Errorf("path component is too long (maximum %d characters)", maxComponentLength)
		}
	}
	return nil
}

func splitPathComponents(pathStr string) []string {
	normalized := strings.ReplaceAll(pathStr, "\\", "/")
	return strings.Split(normalized, "/")
}
