package customprompt

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNormalizeName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "reviewer", want: "reviewer"},
		{in: "  Code Reviewer ", want: "code-reviewer"},
		{in: "ops_runbook", want: "ops-runbook"},
		{in: "a1-b2", want: "a1-b2"},
		{in: "", wantErr: true},
		{in: "   ", wantErr: true},
		{in: "-leading", wantErr: true},
		{in: "dots.not.allowed", wantErr: true},
		{in: "../escape", wantErr: true},
		{in: "slash/name", wantErr: true},
		{in: strings.Repeat("a", MaxNameLen+1), wantErr: true},
		{in: "programmer", wantErr: true},
		{in: "Sysadmin-Lite", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizeName(tt.in)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidName) {
					t.Fatalf("NormalizeName(%q) err = %v, want ErrInvalidName", tt.in, err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("NormalizeName(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
			}
		})
	}
}

func TestValidateBody(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		body    string
		want    string
		wantErr bool
	}{
		{name: "trims", body: "\n  You review code.\n\n", want: "You review code."},
		{name: "empty", body: "", wantErr: true},
		{name: "whitespace", body: " \n\t ", wantErr: true},
		{name: "nul", body: "a\x00b", wantErr: true},
		{name: "invalid utf8", body: "a\xffb", wantErr: true},
		{name: "at limit", body: strings.Repeat("x", MaxPromptBytes), want: strings.Repeat("x", MaxPromptBytes)},
		{name: "over limit", body: strings.Repeat("x", MaxPromptBytes+1), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ValidateBody(tt.body)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidBody) {
					t.Fatalf("err = %v, want ErrInvalidBody", err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("got %q, %v", got, err)
			}
		})
	}
}

func TestStoreLifecycle(t *testing.T) {
	t.Parallel()
	s := NewStore(filepath.Join(t.TempDir(), DirName))

	if got, err := s.List(); err != nil || len(got) != 0 {
		t.Fatalf("List on missing dir = %v, %v; want empty", got, err)
	}

	name, err := s.Create("Code Reviewer", "Review diffs strictly.")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if name != "code-reviewer" {
		t.Fatalf("Create name = %q", name)
	}
	if _, err := s.Create("code-reviewer", "again"); !errors.Is(err, ErrExists) {
		t.Fatalf("second Create err = %v, want ErrExists", err)
	}

	body, err := s.Load("code-reviewer")
	if err != nil || body != "Review diffs strictly." {
		t.Fatalf("Load = %q, %v", body, err)
	}

	if _, err := s.Save("code-reviewer", "Replaced."); err != nil {
		t.Fatalf("Save replace: %v", err)
	}
	if body, _ := s.Load("code-reviewer"); body != "Replaced." {
		t.Fatalf("after replace Load = %q", body)
	}

	if _, err := s.Create("other", "Other."); err != nil {
		t.Fatalf("Create other: %v", err)
	}
	if _, err := s.Rename("code-reviewer", "other"); !errors.Is(err, ErrExists) {
		t.Fatalf("Rename onto existing err = %v, want ErrExists", err)
	}
	renamed, err := s.Rename("code-reviewer", "reviewer")
	if err != nil || renamed != "reviewer" {
		t.Fatalf("Rename = %q, %v", renamed, err)
	}
	if s.Exists("code-reviewer") || !s.Exists("reviewer") {
		t.Fatal("rename did not move the file")
	}

	entries, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 2 || entries[0].Name != "other" || entries[1].Name != "reviewer" {
		t.Fatalf("List = %+v", entries)
	}

	if err := s.Delete("reviewer"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := s.Delete("reviewer"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Delete err = %v, want ErrNotFound", err)
	}
	if _, err := s.Load("reviewer"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Load deleted err = %v, want ErrNotFound", err)
	}
}

func TestStorePermissions(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	dir := filepath.Join(t.TempDir(), DirName)
	s := NewStore(dir)
	if _, err := s.Save("secret", "internal hosts"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := di.Mode().Perm(); got != dirPerm {
		t.Errorf("dir perm = %o, want %o", got, dirPerm)
	}
	fi, err := os.Stat(filepath.Join(dir, "secret"+FileExt))
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != filePerm {
		t.Errorf("file perm = %o, want %o", got, filePerm)
	}
	leftovers, _ := filepath.Glob(filepath.Join(dir, ".*.tmp"))
	if len(leftovers) != 0 {
		t.Errorf("temp files left behind: %v", leftovers)
	}
}

func TestListSkipsForeignFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := NewStore(dir)
	if _, err := s.Save("good", "ok"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"notes.txt", "Bad Name.md", "programmer.md", ".hidden-x.tmp"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "sub.md"), 0o700); err != nil {
		t.Fatal(err)
	}
	entries, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "good" {
		t.Fatalf("List = %+v, want only good", entries)
	}
}

func TestReadImportFile(t *testing.T) {
	t.Parallel()
	work := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(work, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	abs := write("prompt.md", "  # Role\nYou are a reviewer.\n")
	write("relative.txt", "relative body")
	write("big.md", strings.Repeat("x", MaxPromptBytes+1))
	write("empty.md", "   ")
	if err := os.Mkdir(filepath.Join(work, "adir"), 0o700); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		path    string
		want    string
		wantErr bool
	}{
		{name: "absolute", path: abs, want: "# Role\nYou are a reviewer."},
		{name: "relative", path: "relative.txt", want: "relative body"},
		{name: "missing", path: "nope.md", wantErr: true},
		{name: "directory", path: "adir", wantErr: true},
		{name: "oversize", path: "big.md", wantErr: true},
		{name: "empty", path: "empty.md", wantErr: true},
		{name: "blank path", path: "  ", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ReadImportFile(tt.path, work)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ReadImportFile(%q) = %q, want error", tt.path, got)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("ReadImportFile(%q) = %q, %v; want %q", tt.path, got, err, tt.want)
			}
		})
	}
}

func TestDefaultStoreHonorsSagittariusHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SAGITTARIUS_HOME", home)
	s, err := DefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(s.Dir(), home) || filepath.Base(s.Dir()) != DirName {
		t.Fatalf("DefaultStore dir = %q, want under %q ending in %q", s.Dir(), home, DirName)
	}
}
