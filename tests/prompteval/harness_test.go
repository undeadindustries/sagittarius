package prompteval

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

const defaultBinTimeout = 3 * time.Minute

var (
	builtBinOnce sync.Once
	builtBinPath string
	builtBinErr  error
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func sagittariusBin(t *testing.T) string {
	t.Helper()
	if bin := os.Getenv("SAGITTARIUS_BIN"); bin != "" {
		if _, err := os.Stat(bin); err == nil {
			return bin
		}
	}
	root := repoRoot(t)
	builtBinOnce.Do(func() {
		out := filepath.Join(os.TempDir(), "sagittarius-prompteval-bin")
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "go", "build", "-o", out, "./cmd/sagittarius")
		cmd.Dir = root
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			builtBinErr = fmt.Errorf("%v: %s", err, stderr.String())
			return
		}
		builtBinPath = out
	})
	if builtBinErr != nil {
		t.Fatalf("build sagittarius binary: %v", builtBinErr)
	}
	return builtBinPath
}

type runResult struct {
	stdout   string
	stderr   string
	exitCode int
}

func invoke(t *testing.T, bin, workDir string, env []string, args ...string) runResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), defaultBinTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = workDir
	cmd.Env = env

	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb

	code := 0
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		} else {
			t.Logf("invoke exec error: %v", err)
			code = -1
		}
	}
	return runResult{stdout: out.String(), stderr: errb.String(), exitCode: code}
}
