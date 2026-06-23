// Package checks detects a project's language stack from its root marker files
// and builds an ordered plan of external verification commands (lint, format
// check, type check, build, test). It is a leaf package: it knows command
// recipes and install hints but executes nothing, so it stays trivially
// testable and lets internal/tools own subprocess execution and gating.
package checks

import (
	"os"
	"path/filepath"
)

// Check is a single verification command in a stack's plan.
type Check struct {
	// Name is the logical step: "lint", "format", "typecheck", "build", "test".
	Name string
	// Command is the executable looked up on PATH (e.g. "go", "golangci-lint").
	Command string
	// Args are passed verbatim to Command.
	Args []string
	// Mutates is true when the command rewrites files (fix mode); such checks
	// are only emitted when Detect is called with fix=true.
	Mutates bool
	// FileScoped is true when Args end in a target list that may be narrowed to
	// caller-supplied paths instead of the default whole-tree target.
	FileScoped bool
	// FailOnOutput marks checks whose exit code is 0 even on findings (e.g.
	// `gofmt -l`); the runner treats non-empty output as a failure.
	FailOnOutput bool
}

// Plan is the ordered set of checks for a detected stack. Stack is empty when
// no known marker is found.
type Plan struct {
	Stack  string
	Checks []Check
}

// Detect inspects root for stack markers and returns the matching check plan.
// When fix is true, format/lint steps use their mutating (auto-fix) variants;
// build/test/typecheck steps are unaffected. The first matching marker wins, so
// polyglot repos resolve to a single primary stack (callers can run additional
// checks via the shell).
func Detect(root string, fix bool) Plan {
	switch {
	case exists(root, "go.mod"):
		return goPlan(fix)
	case exists(root, "package.json"):
		return nodePlan(root, fix)
	case exists(root, "pyproject.toml"), exists(root, "requirements.txt"), exists(root, "setup.py"), exists(root, "setup.cfg"):
		return pythonPlan(fix)
	case exists(root, "Cargo.toml"):
		return rustPlan(fix)
	default:
		return Plan{}
	}
}

// InstallHint returns a one-line install suggestion for a missing command, or
// "" when no hint is known.
func InstallHint(command string) string {
	switch command {
	case "go", "gofmt":
		return "install the Go toolchain from https://go.dev/dl/"
	case "golangci-lint":
		return "go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest"
	case "eslint":
		return "npm install --save-dev eslint"
	case "prettier":
		return "npm install --save-dev prettier"
	case "tsc":
		return "npm install --save-dev typescript"
	case "ruff":
		return "pip install ruff"
	case "mypy":
		return "pip install mypy"
	case "cargo":
		return "install Rust via https://rustup.rs"
	default:
		return ""
	}
}

func goPlan(fix bool) Plan {
	format := Check{Name: "format", Command: "gofmt", Args: []string{"-l", "."}, FileScoped: true, FailOnOutput: true}
	if fix {
		format = Check{Name: "format", Command: "gofmt", Args: []string{"-w", "."}, FileScoped: true, Mutates: true}
	}
	lint := Check{Name: "lint", Command: "golangci-lint", Args: []string{"run", "./..."}}
	if fix {
		lint = Check{Name: "lint", Command: "golangci-lint", Args: []string{"run", "--fix", "./..."}, Mutates: true}
	}
	return Plan{
		Stack: "go",
		Checks: []Check{
			format,
			{Name: "vet", Command: "go", Args: []string{"vet", "./..."}},
			lint,
			{Name: "build", Command: "go", Args: []string{"build", "./..."}},
			{Name: "test", Command: "go", Args: []string{"test", "./..."}},
		},
	}
}

func nodePlan(root string, fix bool) Plan {
	lint := Check{Name: "lint", Command: "eslint", Args: []string{"."}, FileScoped: true}
	if fix {
		lint = Check{Name: "lint", Command: "eslint", Args: []string{"--fix", "."}, FileScoped: true, Mutates: true}
	}
	format := Check{Name: "format", Command: "prettier", Args: []string{"--check", "."}, FileScoped: true}
	if fix {
		format = Check{Name: "format", Command: "prettier", Args: []string{"--write", "."}, FileScoped: true, Mutates: true}
	}
	plan := Plan{Stack: "node", Checks: []Check{lint, format}}
	// Type check only when the project is configured for TypeScript.
	if exists(root, "tsconfig.json") {
		plan.Checks = append(plan.Checks, Check{Name: "typecheck", Command: "tsc", Args: []string{"--noEmit"}})
	}
	return plan
}

func pythonPlan(fix bool) Plan {
	lint := Check{Name: "lint", Command: "ruff", Args: []string{"check", "."}, FileScoped: true}
	if fix {
		lint = Check{Name: "lint", Command: "ruff", Args: []string{"check", "--fix", "."}, FileScoped: true, Mutates: true}
	}
	format := Check{Name: "format", Command: "ruff", Args: []string{"format", "--check", "."}, FileScoped: true}
	if fix {
		format = Check{Name: "format", Command: "ruff", Args: []string{"format", "."}, FileScoped: true, Mutates: true}
	}
	return Plan{
		Stack: "python",
		Checks: []Check{
			lint,
			format,
			{Name: "typecheck", Command: "mypy", Args: []string{"."}, FileScoped: true},
		},
	}
}

func rustPlan(fix bool) Plan {
	format := Check{Name: "format", Command: "cargo", Args: []string{"fmt", "--check"}}
	if fix {
		format = Check{Name: "format", Command: "cargo", Args: []string{"fmt"}, Mutates: true}
	}
	return Plan{
		Stack: "rust",
		Checks: []Check{
			format,
			{Name: "lint", Command: "cargo", Args: []string{"clippy"}},
			{Name: "test", Command: "cargo", Args: []string{"test"}},
		},
	}
}

func exists(root, name string) bool {
	_, err := os.Stat(filepath.Join(root, name))
	return err == nil
}
