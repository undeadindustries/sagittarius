// Package archtest holds architecture-boundary tests. It has no production code;
// it exists so `go test ./...` enforces the import seams documented in AGENTS.md.
package archtest

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// charmPrefix is the import-path prefix for the charm UI libraries (bubbletea,
// bubbles, lipgloss, x/vt, x/ansi, …). The TUI seam keeps these out of the
// agent/core packages so the UI stays swappable.
const charmPrefix = "github.com/charmbracelet/"

// charmAllowedDirs are the module-relative directory prefixes permitted to import
// charm. Everything else (notably internal/agent and internal/slash) must stay
// charm-free so the agent core never depends on a concrete UI toolkit.
var charmAllowedDirs = []string{
	"internal/ui/",    // the Bubble Tea UI implementation and its dialog leaves
	"internal/tools/", // shell.go/ptyrun.go use x/vt + x/ansi for PTY emulation
}

// TestNoCharmOutsideUI walks every Go file in the module and fails if any file
// outside the allowed directories imports a charm package.
func TestNoCharmOutsideUI(t *testing.T) {
	root := moduleRoot(t)
	fset := token.NewFileSet()

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor", "testdata", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Errorf("parse %s: %v", rel, err)
			return nil
		}
		for _, imp := range f.Imports {
			ipath := strings.Trim(imp.Path.Value, `"`)
			if !strings.HasPrefix(ipath, charmPrefix) {
				continue
			}
			if !allowedCharmFile(rel) {
				t.Errorf("forbidden charm import %q in %s: charm is only allowed under %s", ipath, rel, strings.Join(charmAllowedDirs, ", "))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk module: %v", err)
	}
}

// TestAgentAndSlashHaveNoCharm is a focused assertion of the most important half
// of the rule: the agent core and slash layer must never import charm.
func TestAgentAndSlashHaveNoCharm(t *testing.T) {
	root := moduleRoot(t)
	fset := token.NewFileSet()
	for _, pkg := range []string{"internal/agent", "internal/slash"} {
		dir := filepath.Join(root, filepath.FromSlash(pkg))
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
			if err != nil {
				t.Errorf("parse %s: %v", path, err)
				return nil
			}
			for _, imp := range f.Imports {
				if strings.HasPrefix(strings.Trim(imp.Path.Value, `"`), charmPrefix) {
					rel, _ := filepath.Rel(root, path)
					t.Errorf("%s must not import charm (%s)", pkg, filepath.ToSlash(rel))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", pkg, err)
		}
	}
}

func allowedCharmFile(rel string) bool {
	for _, dir := range charmAllowedDirs {
		if strings.HasPrefix(rel, dir) {
			return true
		}
	}
	return false
}

// moduleRoot returns the repository root, derived from this test file's location
// (internal/archtest is two levels below the module root).
func moduleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine caller path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
