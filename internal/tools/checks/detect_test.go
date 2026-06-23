package checks

import (
	"os"
	"path/filepath"
	"testing"
)

func writeMarkers(t *testing.T, names ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("write marker %q: %v", name, err)
		}
	}
	return root
}

func hasCheck(checks []Check, name string) (Check, bool) {
	for _, c := range checks {
		if c.Name == name {
			return c, true
		}
	}
	return Check{}, false
}

func TestDetectStack(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		markers   []string
		wantStack string
		wantCheck string // a check name expected in the plan
	}{
		{"go", []string{"go.mod"}, "go", "vet"},
		{"node", []string{"package.json"}, "node", "lint"},
		{"node typescript", []string{"package.json", "tsconfig.json"}, "node", "typecheck"},
		{"python pyproject", []string{"pyproject.toml"}, "python", "lint"},
		{"python requirements", []string{"requirements.txt"}, "python", "format"},
		{"rust", []string{"Cargo.toml"}, "rust", "lint"},
		{"none", []string{"README.md"}, "", ""},
		{"go wins over node", []string{"go.mod", "package.json"}, "go", "build"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := writeMarkers(t, tt.markers...)
			plan := Detect(root, false)
			if plan.Stack != tt.wantStack {
				t.Fatalf("stack = %q, want %q", plan.Stack, tt.wantStack)
			}
			if tt.wantStack == "" {
				if len(plan.Checks) != 0 {
					t.Fatalf("expected no checks for unknown stack, got %d", len(plan.Checks))
				}
				return
			}
			if _, ok := hasCheck(plan.Checks, tt.wantCheck); !ok {
				t.Fatalf("expected check %q in %s plan", tt.wantCheck, tt.wantStack)
			}
		})
	}
}

func TestDetectNodeTypecheckOnlyWithTsconfig(t *testing.T) {
	t.Parallel()

	root := writeMarkers(t, "package.json")
	if _, ok := hasCheck(Detect(root, false).Checks, "typecheck"); ok {
		t.Fatal("typecheck should not be present without tsconfig.json")
	}
}

func TestDetectFixSwitchesToMutatingVariants(t *testing.T) {
	t.Parallel()

	root := writeMarkers(t, "go.mod")

	checkOnly, _ := hasCheck(Detect(root, false).Checks, "format")
	if checkOnly.Mutates {
		t.Fatal("check-only format should not be mutating")
	}
	if got := checkOnly.Args[0]; got != "-l" {
		t.Fatalf("check-only gofmt arg = %q, want -l", got)
	}

	fixed, _ := hasCheck(Detect(root, true).Checks, "format")
	if !fixed.Mutates {
		t.Fatal("fix format should be mutating")
	}
	if got := fixed.Args[0]; got != "-w" {
		t.Fatalf("fix gofmt arg = %q, want -w", got)
	}
}

func TestInstallHint(t *testing.T) {
	t.Parallel()

	if InstallHint("ruff") == "" {
		t.Fatal("expected install hint for ruff")
	}
	if InstallHint("golangci-lint") == "" {
		t.Fatal("expected install hint for golangci-lint")
	}
	if InstallHint("totally-unknown-tool") != "" {
		t.Fatal("expected empty hint for unknown tool")
	}
}
