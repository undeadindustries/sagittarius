package toolkit

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

func TestScan(t *testing.T) {
	// Stub LookPath to return success only for specific commands.
	mockInstalled := map[string]bool{
		"rg":      true,
		"python3": true,
		"node":    true,
	}

	lookPath = func(file string) (string, error) {
		if mockInstalled[file] {
			return "/fake/" + file, nil
		}
		return "", fmt.Errorf("not found")
	}
	t.Cleanup(func() {
		lookPath = exec.LookPath
	})

	cfg := ScanConfig{
		GOOS: "linux",
	}

	rep := Scan(cfg)

	if len(rep.Groups) == 0 {
		t.Fatal("expected groups in report")
	}

	out := rep.RenderPlain()
	if !strings.Contains(out, "[x] ripgrep") {
		t.Errorf("expected ripgrep to be installed, got:\n%s", out)
	}
	if !strings.Contains(out, "[ ] git") {
		t.Errorf("expected git to be missing, got:\n%s", out)
	}
	if !strings.Contains(out, "[x] python") {
		t.Errorf("expected python to be installed, got:\n%s", out)
	}
	if !strings.Contains(out, "[x] node") {
		t.Errorf("expected node to be installed, got:\n%s", out)
	}
}
