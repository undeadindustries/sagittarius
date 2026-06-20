package main_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMainVersion(t *testing.T) {
	t.Parallel()

	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}

	bin := filepath.Join(t.TempDir(), "sagittarius")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/sagittarius")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build binary: %v\n%s", err, out)
	}

	run := exec.Command(bin, "--version")
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("run --version: %v\n%s", err, out)
	}

	version := strings.TrimSpace(string(out))
	if version == "" {
		t.Fatal("--version output is empty")
	}
}
