package prompteval

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type evalRunner struct {
	t         *testing.T
	bin       string
	workDir   string
	env       []string
	sessionID string
	logFile   string
	model     string
	turnCount int
}

func (r *evalRunner) runTurn(prompt string, extraArgs ...string) {
	r.t.Helper()
	args := []string{"--yolo", "--output-format", "text", "--log-verbose", "--model", r.model}
	if r.turnCount > 0 {
		args = append(args, "--resume", "latest")
	}
	args = append(args, extraArgs...)
	args = append(args, "-p", prompt)

	res := invoke(r.t, r.bin, r.workDir, r.env, args...)
	if res.exitCode != 0 {
		r.t.Fatalf("invoke failed (exit %d)\nstdout: %s\nstderr: %s", res.exitCode, res.stdout, res.stderr)
	}
	r.turnCount++
}

func (r *evalRunner) parseLog() *ParsedLog {
	r.t.Helper()
	f, err := os.Open(r.logFile)
	if err != nil {
		r.t.Fatalf("open log file: %v", err)
	}
	defer func() { _ = f.Close() }()
	plog, err := ParseVerboseLog(f)
	if err != nil {
		r.t.Fatalf("parse log: %v", err)
	}
	return plog
}

// mustMkdirAll and mustWriteFile fail the test on a fixture-setup error. A
// silently skipped fixture write would grade the model against a workspace it
// was never given, which is worse than no measurement at all.
func mustMkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
}

func mustWriteFile(t *testing.T, path string, data string, perm os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), perm); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func runEval(t *testing.T, taskName string, setup func(t *testing.T, workDir string), testFunc func(t *testing.T, r *evalRunner)) {
	t.Helper()
	if os.Getenv("SAGITTARIUS_PROMPT_EVAL") != "1" {
		t.Skip("skipping prompt evaluation test; set SAGITTARIUS_PROMPT_EVAL=1")
	}

	model := os.Getenv("SAGITTARIUS_PROMPT_EVAL_MODEL")
	if model == "" {
		model = "gemini-flash-lite-latest"
	}

	bins := []struct {
		name string
		path string
	}{
		{"current", sagittariusBin(t)},
	}

	if baseBin := os.Getenv("SAGITTARIUS_PROMPT_EVAL_BASELINE_BIN"); baseBin != "" {
		bins = append([]struct{ name, path string }{{"baseline", baseBin}}, bins...)
	}

	for _, b := range bins {
		t.Run(fmt.Sprintf("%s/%s", b.name, model), func(t *testing.T) {
			workDir := t.TempDir()
			if setup != nil {
				setup(t, workDir)
			}

			sessionID := fmt.Sprintf("eval-%s-%s", taskName, time.Now().Format("150405.000"))
			logFile := filepath.Join(os.Getenv("HOME"), ".sagittarius", "logs", fmt.Sprintf("chat-verbose-%s.log", sessionID))

			env := append(os.Environ(),
				"SAGITTARIUS_SESSION_ID="+sessionID,
				"SAGITTARIUS_MODEL="+model,
				"GEMINI_PROVIDER=gemini-apikey",
			)

			runner := &evalRunner{
				t:         t,
				bin:       b.path,
				workDir:   workDir,
				env:       env,
				sessionID: sessionID,
				logFile:   logFile,
				model:     model,
			}

			testFunc(t, runner)
		})
	}
}
