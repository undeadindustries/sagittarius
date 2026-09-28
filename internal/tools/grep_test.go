package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestGrepPatternStartingWithDash is the AD-094/benchmark failure: a pattern
// starting with "-" (a diff header, a negative look-around) used to die with
// "rg: unrecognized flag" because the pattern was appended without "--".
func TestGrepPatternStartingWithDash(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("ripgrep not installed")
	}

	root := t.TempDir()
	content := "alpha\n-beta\n--gamma\n"
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	tool := NewBuiltinRegistry(ws)
	g, ok := tool.Lookup(GrepToolName)
	if !ok {
		t.Fatal("grep_search not registered")
	}

	for _, pattern := range []string{"-beta", "--gamma", "^-"} {
		result, err := g.Execute(context.Background(), map[string]any{
			ParamPattern:       pattern,
			ParamDirPath:       ".",
			GrepParamNamesOnly: false,
		})
		if err != nil {
			t.Fatalf("pattern %q: %v", pattern, err)
		}
		matches, _ := result["matches"].(string)
		if strings.Contains(matches, "unrecognized flag") {
			t.Errorf("pattern %q hit the flag-parsing bug: %q", pattern, matches)
		}
		if pattern == "-beta" && !strings.Contains(matches, "-beta") {
			t.Errorf("pattern %q found no match in %q", pattern, matches)
		}
	}
}
