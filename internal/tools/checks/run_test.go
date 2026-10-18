package checks

import (
	"slices"
	"testing"
)

func TestArgvNarrowsFileScoped(t *testing.T) {
	t.Parallel()

	fileScoped := Check{Name: "format", Command: "gofmt", Args: []string{"-l", "."}, FileScoped: true}
	got := Argv(fileScoped, []string{"a.go", "b.go"})
	want := []string{"-l", "a.go", "b.go"}
	if len(got) != len(want) {
		t.Fatalf("argv = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("argv = %v, want %v", got, want)
		}
	}

	moduleScoped := Check{Name: "vet", Command: "go", Args: []string{"vet", "./..."}}
	got = Argv(moduleScoped, []string{"a.go"})
	if len(got) != 2 || got[1] != "./..." {
		t.Fatalf("module-scoped argv should be unchanged, got %v", got)
	}
}

func TestScopePaths(t *testing.T) {
	t.Parallel()
	mypy := pythonPlan(false).Checks[2]
	gofmt := goPlan(false).Checks[0]
	vet := goPlan(false).Checks[1]
	anyFile := Check{Name: "format", Command: "fmt", Args: []string{"."}, FileScoped: true}
	dirs := map[string]bool{"src": true, "pkg/sub": true}
	isDir := func(p string) bool { return dirs[p] }

	cases := []struct {
		name  string
		check Check
		paths []string
		want  []string
	}{
		{"mypy drops pyproject.toml", mypy, []string{"app.py", "pyproject.toml", "README.md"}, []string{"app.py"}},
		{"mypy keeps stubs and directories", mypy, []string{"types.pyi", "src"}, []string{"types.pyi", "src"}},
		{"extension match ignores case", mypy, []string{"Legacy.PY"}, []string{"Legacy.PY"}},
		{"nothing applies yields empty", mypy, []string{"pyproject.toml", "Makefile"}, []string{}},
		{"gofmt drops docs", gofmt, []string{"main.go", "docs/x.md", "pkg/sub"}, []string{"main.go", "pkg/sub"}},
		{"module check passes through", vet, []string{"docs/x.md"}, []string{"docs/x.md"}},
		{"no extension list accepts all", anyFile, []string{"a.toml", "b"}, []string{"a.toml", "b"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ScopePaths(tc.check, tc.paths, isDir); !slices.Equal(got, tc.want) {
				t.Errorf("ScopePaths = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestFileScopedChecksDeclareExtensions keeps new file-scoped checks honest:
// without an extension list they would receive every caller path again.
func TestFileScopedChecksDeclareExtensions(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, fix := range []bool{false, true} {
		for _, plan := range []Plan{goPlan(fix), nodePlan(root, fix), pythonPlan(fix), rustPlan(fix)} {
			for _, c := range plan.Checks {
				if c.FileScoped && len(c.Extensions) == 0 {
					t.Errorf("%s %s (fix=%v) is file-scoped but declares no extensions", plan.Stack, c.Name, fix)
				}
			}
		}
	}
}

func TestTruncateCaps(t *testing.T) {
	t.Parallel()

	short := "hello"
	if got := Truncate(short); got != short {
		t.Fatalf("Truncate(%q) = %q, want unchanged", short, got)
	}

	long := make([]byte, maxCheckOutputBytes+100)
	for i := range long {
		long[i] = 'x'
	}
	got := Truncate(string(long))
	if len(got) <= maxCheckOutputBytes {
		t.Fatalf("Truncate should still be capped-ish with suffix, got len %d", len(got))
	}
	if got[:maxCheckOutputBytes] != string(long[:maxCheckOutputBytes]) {
		t.Fatal("Truncate must preserve the leading maxCheckOutputBytes bytes")
	}
}
