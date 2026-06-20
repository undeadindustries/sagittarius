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
// Existing paths are canonicalized with EvalSymlinks; new paths are checked
// against the root without requiring the target to exist.
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

	if _, err := os.Stat(abs); err == nil {
		real, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return "", fmt.Errorf("resolve symlinks: %w", err)
		}
		if !w.isWithinRoot(real) {
			return "", fmt.Errorf("path %q resolves outside the trusted workspace", pathStr)
		}
		return real, nil
	}

	return abs, nil
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
