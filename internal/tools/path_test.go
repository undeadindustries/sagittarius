package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolvePathSymlinks(t *testing.T) {
	root := t.TempDir()
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}

	outsideDir := t.TempDir()

	// Setup directories inside workspace
	subDir := filepath.Join(root, "sub")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	targetDir := filepath.Join(root, "target")
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		t.Fatal(err)
	}

	// 1. Symlink directory to another directory inside workspace
	linkInside := filepath.Join(root, "link_inside")
	if err := os.Symlink(targetDir, linkInside); err != nil {
		t.Fatal(err)
	}

	// 2. Symlink directory to outside workspace
	linkOutside := filepath.Join(root, "link_outside")
	if err := os.Symlink(outsideDir, linkOutside); err != nil {
		t.Fatal(err)
	}

	// 3. Broken symlink
	brokenLink := filepath.Join(root, "broken_link")
	if err := os.Symlink(filepath.Join(root, "does_not_exist"), brokenLink); err != nil {
		t.Fatal(err)
	}

	// 4. Nested chained symlinks: link1 -> link2 -> target
	linkChained := filepath.Join(root, "link_chained")
	if err := os.Symlink(linkInside, linkChained); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		input       string
		wantRel     string
		expectError bool
		errContains string
	}{
		{
			name:    "regular nonexistent file in existing dir",
			input:   "sub/newfile.txt",
			wantRel: "sub/newfile.txt",
		},
		{
			name:    "nonexistent file in deeply nested nonexistent dir",
			input:   "sub/a/b/c/newfile.txt",
			wantRel: "sub/a/b/c/newfile.txt",
		},
		{
			name:    "nonexistent file through inside symlink resolves to target dir",
			input:   "link_inside/newfile.txt",
			wantRel: "target/newfile.txt",
		},
		{
			name:    "nonexistent file through chained symlink resolves to target dir",
			input:   "link_chained/nested/newfile.txt",
			wantRel: "target/nested/newfile.txt",
		},
		{
			name:        "nonexistent file through outside symlink fails with outside error",
			input:       "link_outside/escape.txt",
			expectError: true,
			errContains: "resolves outside the trusted workspace",
		},
		{
			name:        "nonexistent file through broken symlink fails with resolve error",
			input:       "broken_link/newfile.txt",
			expectError: true,
			errContains: "resolve symlinks",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			resolved, err := ws.ResolvePath(tc.input)
			if tc.expectError {
				if err == nil {
					t.Fatalf("ResolvePath(%q) = %q, want error containing %q", tc.input, resolved, tc.errContains)
				}
				if tc.errContains != "" && !strings.Contains(err.Error(), tc.errContains) {
					t.Fatalf("ResolvePath(%q) error = %q, want error containing %q", tc.input, err.Error(), tc.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolvePath(%q) unexpected error: %v", tc.input, err)
			}

			rel, err := ws.RelativePath(tc.input)
			if err != nil {
				t.Fatalf("RelativePath(%q) unexpected error: %v", tc.input, err)
			}
			if rel != tc.wantRel {
				t.Fatalf("RelativePath(%q) = %q, want %q", tc.input, rel, tc.wantRel)
			}
		})
	}
}
