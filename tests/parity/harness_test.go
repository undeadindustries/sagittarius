// Package parity_test contains parity validation tests between Sagittarius and
// the frozen gemini-cli fork. Tests in this package run by default against
// Sagittarius only (using mock servers). Live-fork comparison tests are gated
// behind the SAGITTARIUS_PARITY_FORK=1 environment variable so they do not
// block the default `go test ./...` run.
//
// # Environment variables
//
//   - SAGITTARIUS_PARITY_FORK=1 — opt in to tests that invoke the fork binary
//     (requires Node + the fork at /home/rob/src/gemini-cli).
//   - SAGITTARIUS_BIN — path to the sagittarius binary (default: ../../bin/sagittarius).
//   - SAGITTARIUS_FORK_DIR — path to the fork source dir (default: /home/rob/src/gemini-cli).
package parity_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// defaultForkDir is the frozen fork location.
const defaultForkDir = "/home/rob/src/gemini-cli"

// defaultForkInvokeTimeout is generous due to npm/tsx transpile overhead (~4s+).
const defaultForkInvokeTimeout = 90 * time.Second

// defaultBinTimeout is the sagittarius binary invocation timeout.
const defaultBinTimeout = 30 * time.Second

// forkInvokeMu serializes fork `npm start` invocations. The parity tests run
// with t.Parallel(); concurrent npm/tsx processes against one fork checkout can
// contend on the transpile cache and produce flaky timings or empty output, so
// every fork invocation holds this lock for its duration.
var forkInvokeMu sync.Mutex

// forkParityEnabled reports whether live-fork comparison tests should run.
func forkParityEnabled() bool {
	return os.Getenv("SAGITTARIUS_PARITY_FORK") == "1"
}

// skipUnlessFork calls t.Skip when live-fork tests are not opted in.
func skipUnlessFork(t *testing.T) {
	t.Helper()
	if !forkParityEnabled() {
		t.Skip("live-fork parity tests disabled; set SAGITTARIUS_PARITY_FORK=1 to enable")
	}
}

// projectRoot returns the root of the sagittarius repo, derived from this
// file's location.  Tests in tests/parity/ are two directories below root.
func projectRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	// file = .../sagittarius/tests/parity/harness_test.go
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// builtBin caches the on-demand build so the binary is compiled at most once
// per `go test` invocation.
var (
	builtBinOnce sync.Once
	builtBinPath string
	builtBinErr  error
)

// sagittariusBin returns the path to the sagittarius binary. Resolution order:
//  1. $SAGITTARIUS_BIN, when set and present;
//  2. the prebuilt ./bin/sagittarius from `make build`, when present;
//  3. an on-demand `go build` into a temp dir (cached for the whole run).
//
// It never skips: a missing prebuilt binary is built so the headless/perf paths
// always run under `go test ./...`, even in CI that does not run `make build`.
// A build failure fails the test loudly rather than silently skipping.
func sagittariusBin(t *testing.T) string {
	t.Helper()
	if binEnv := os.Getenv("SAGITTARIUS_BIN"); binEnv != "" {
		if _, err := os.Stat(binEnv); err == nil {
			return binEnv
		}
	}
	root := projectRoot(t)
	if prebuilt := filepath.Join(root, "bin", "sagittarius"); fileExists(prebuilt) {
		return prebuilt
	}

	builtBinOnce.Do(func() {
		builtBinPath, builtBinErr = buildSagittarius(root)
	})
	if builtBinErr != nil {
		t.Fatalf("build sagittarius binary: %v", builtBinErr)
	}
	return builtBinPath
}

// buildSagittarius compiles the CLI into a stable per-run temp path and returns
// it. The output lives under os.TempDir so it survives across tests in the run.
func buildSagittarius(root string) (string, error) {
	out := filepath.Join(os.TempDir(), "sagittarius-parity-bin")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "build", "-o", out, "./cmd/sagittarius")
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", &buildError{cause: err, stderr: stderr.String()}
	}
	return out, nil
}

// buildError carries go build failure context (including compiler stderr).
type buildError struct {
	cause  error
	stderr string
}

func (e *buildError) Error() string {
	return e.cause.Error() + ": " + strings.TrimSpace(e.stderr)
}

func (e *buildError) Unwrap() error { return e.cause }

// fileExists reports whether path exists and is statable.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// forkDir returns the path to the fork source directory.
func forkDir() string {
	if d := os.Getenv("SAGITTARIUS_FORK_DIR"); d != "" {
		return d
	}
	return defaultForkDir
}

// setupTempHome creates an isolated temp HOME directory with a minimal
// settings.json and returns the home path. baseURL, if non-empty, configures
// the openai provider to point to a mock server.
func setupTempHome(t *testing.T, baseURL string) string {
	t.Helper()
	home := t.TempDir()
	geminiDir := filepath.Join(home, ".gemini")
	if err := os.MkdirAll(geminiDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	settings := buildSettings(baseURL)
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	settingsPath := filepath.Join(geminiDir, "settings.json")
	if err := os.WriteFile(settingsPath, data, 0o600); err != nil {
		t.Fatalf("WriteFile settings: %v", err)
	}
	return home
}

// buildSettings returns a settings map pointing to the mock OpenAI server.
func buildSettings(baseURL string) map[string]interface{} {
	openaiCfg := map[string]interface{}{
		"model": "gpt-mock",
	}
	if baseURL != "" {
		openaiCfg["baseUrl"] = baseURL + "/v1"
	}
	return map[string]interface{}{
		"providers": map[string]interface{}{
			"active": "openai",
			"openai": openaiCfg,
		},
	}
}

// invokeSagittarius runs the sagittarius binary with the given arguments,
// isolated HOME (GEMINI_CLI_HOME), and optional extra env vars.  Returns
// combined stdout output.  Fails the test if the process exits non-zero.
func invokeSagittarius(ctx context.Context, t *testing.T, bin, home string, extraEnv []string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = append(baseEnv(home), extraEnv...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("sagittarius %v: %v\nstdout=%q\nstderr=%q", args, err, stdout.String(), stderr.String())
	}
	return stdout.String()
}

// invokeSagittariusOutput is like invokeSagittarius but does not fail on
// non-zero exit; it returns stdout, stderr and exit code.
func invokeSagittariusOutput(ctx context.Context, t *testing.T, bin, home string, extraEnv []string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = append(baseEnv(home), extraEnv...)

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	exitCode = 0
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			exitCode = exit.ExitCode()
		} else {
			// Non-ExitError (e.g. context deadline exceeded, binary not
			// launchable): surface as a non-zero code so callers that gate on
			// exitCode == 0 do not misclassify a timed-out run as success.
			t.Logf("invokeSagittariusOutput exec error: %v", err)
			exitCode = -1
		}
	}
	return outBuf.String(), errBuf.String(), exitCode
}

// invokeFork runs the fork via `npm start -- <args>` with the given isolated
// HOME directory. Strips npm/tsx noise from stdout. Returns cleaned stdout.
// Skips the test if the fork directory is not accessible.
func invokeFork(ctx context.Context, t *testing.T, home string, args ...string) string {
	t.Helper()
	dir := forkDir()
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("fork directory %s not accessible: %v", dir, err)
	}

	forkInvokeMu.Lock()
	defer forkInvokeMu.Unlock()

	npmArgs := append([]string{"start", "--"}, args...)
	cmd := exec.CommandContext(ctx, "npm", npmArgs...)
	cmd.Dir = dir
	cmd.Env = append(baseEnv(home), "PATH="+os.Getenv("PATH"))

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Logf("fork stderr: %s", stderr.String())
		t.Skipf("fork invocation failed (not a blocker): %v", err)
	}
	return stripForkNoise(stdout.String())
}

// invokeForkLoose is like invokeFork but does not skip on non-zero exit;
// returns cleaned stdout regardless.
func invokeForkLoose(ctx context.Context, t *testing.T, home string, args ...string) (out string, ok bool) {
	t.Helper()
	dir := forkDir()
	if _, err := os.Stat(dir); err != nil {
		return "", false
	}

	forkInvokeMu.Lock()
	defer forkInvokeMu.Unlock()

	npmArgs := append([]string{"start", "--"}, args...)
	cmd := exec.CommandContext(ctx, "npm", npmArgs...)
	cmd.Dir = dir
	cmd.Env = append(baseEnv(home), "PATH="+os.Getenv("PATH"))

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	_ = cmd.Run()
	cleaned := stripForkNoise(stdout.String())
	return cleaned, true
}

// reForkNoise matches lines that are npm/tsx wrapper noise rather than CLI output.
// Patterns observed in the actual fork output (npm start -- <args>):
//   - "> @google/gemini-cli@x.y.z start" (npm script echo)
//   - "> cross-env NODE_ENV=development node scripts/start.js ..." (actual invocation echo)
//   - "Checking build status..." (tsx source-file check)
//   - "Source file ... modified since last build" warning
//   - "Run npm run build" suggestion
//   - "npm notice ..." / "npm warn ..." lines
var reForkNoise = regexp.MustCompile(`(?m)^(> .*|Checking build status.*|Source file.*|Run npm run build.*|npm notice.*|npm warn.*)\n?`)

// stripForkNoise removes npm and tsx wrapper lines from fork stdout, leaving
// only the CLI's actual output.
func stripForkNoise(raw string) string {
	cleaned := reForkNoise.ReplaceAllString(raw, "")
	return strings.TrimSpace(cleaned)
}

// baseEnv builds a minimal environment for subprocess invocations, pointing
// GEMINI_CLI_HOME / HOME at the temp directory and scrubbing live API keys.
func baseEnv(home string) []string {
	return []string{
		"GEMINI_CLI_HOME=" + home,
		"HOME=" + home,
		"PATH=" + os.Getenv("PATH"),
		"OPENAI_API_KEY=test-fake-key-parity",
		"GOOGLE_API_KEY=test-fake-key-parity",
		// suppress TUI requirements
		"TERM=dumb",
		// prevent loading user config from real home
		"XDG_CONFIG_HOME=" + home,
	}
}

// measureColdStart runs the binary with -v and returns the wall time.
func measureColdStart(t *testing.T, bin string, env []string) time.Duration {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), defaultBinTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "-v")
	cmd.Env = env
	start := time.Now()
	_ = cmd.Run()
	return time.Since(start)
}

// measureForkColdStart measures `npm start -- --version` wall time. The bool
// result reports whether the run actually succeeded; a timeout or npm failure
// returns false so the caller does not log a bogus measurement or speedup.
func measureForkColdStart(t *testing.T) (time.Duration, bool) {
	t.Helper()
	dir := forkDir()
	if _, err := os.Stat(dir); err != nil {
		return 0, false
	}

	forkInvokeMu.Lock()
	defer forkInvokeMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), defaultForkInvokeTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "npm", "start", "--", "--version")
	cmd.Dir = dir
	cmd.Env = append(baseEnv(t.TempDir()), "PATH="+os.Getenv("PATH"))
	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)
	if err != nil {
		t.Logf("fork cold-start measurement failed (not a blocker): %v", err)
		return 0, false
	}
	return elapsed, true
}

// recordPerfNumbers logs cold-start measurements for the checklist.
func recordPerfNumbers(t *testing.T, sagName string, sagDur time.Duration, forkDur time.Duration, forkOK bool) {
	t.Helper()
	t.Logf("PERF: %s cold-start = %s", sagName, sagDur.Round(time.Millisecond))
	if forkOK {
		t.Logf("PERF: fork cold-start (npm start) = %s (includes ~4s npm/tsx overhead)", forkDur.Round(time.Millisecond))
		t.Logf("PERF: speedup = %.1fx", float64(forkDur)/float64(sagDur))
	} else {
		t.Logf("PERF: fork not available; fork baseline is ~4s from known measurement")
	}
}

// forkCommandEntry is a known command name+description from the fork source.
// Used for static comparison without running the fork.
type forkCommandEntry struct {
	// name is the slash command name (without /).
	name string
	// description is the fork's description string.
	description string
	// parent is the parent command name, empty for top-level.
	parent string
}

// forkInScopeCommands is the statically-extracted table of commands from the
// fork TypeScript source that are in-scope for Sagittarius (Phase 09–13).
// Extracted from packages/cli/src/ui/commands/*.ts (read-only reference).
//
// This table is used by TestParityHelpOutput to verify Sagittarius implements
// every in-scope command. Fork extra commands (clear, resume, about, chat, etc.)
// that are in the fork superset are listed in PARITY_CHECKLIST.md.
var forkInScopeCommands = []forkCommandEntry{
	{name: "help", description: "For help on gemini-cli"},
	{name: "quit", description: "Exit the cli"},
	// Sagittarius renames the fork's `/provider` to `/providers` (plural) and
	// folds the fork's separate `/auth` command into the providers wizard's
	// "Set API key" screen. Both are intentional divergences documented in
	// PARITY_CHECKLIST.md (AD-025).
	{name: "providers", description: "", parent: ""},
	{name: "list", description: "List configured providers (built-in + custom) and active state", parent: "providers"},
	{name: "use", description: "", parent: "providers"},    // description varies
	{name: "set", description: "", parent: "providers"},    // description varies
	{name: "add", description: "", parent: "providers"},    // description varies
	{name: "remove", description: "", parent: "providers"}, // description varies
	{name: "model", description: "Manage model configuration"},
	{name: "memory", description: "Commands for interacting with memory"},
	{name: "reload", description: "Reload the memory from the source", parent: "memory"},
	{name: "skills", description: "", parent: ""},
	{name: "list", description: "", parent: "skills"},
	{name: "reload", description: "", parent: "skills"},
	{name: "mcp", description: "Manage configured Model Context Protocol (MCP) servers"},
	{name: "list", description: "List configured MCP servers and tools", parent: "mcp"},
	{name: "reload", description: "Reloads MCP servers", parent: "mcp"},
	{name: "agents", description: "Manage agents"},
	{name: "list", description: "List available local and remote agents", parent: "agents"},
	{name: "reload", description: "Reload the agent registry", parent: "agents"},
	{name: "resume", description: "Browse auto-saved conversations and manage chat checkpoints"},
	{name: "clear", description: "Clear the screen and start a new session"},
}

// inScopeTopLevelNames is the set of top-level command names Sagittarius must implement.
var inScopeTopLevelNames = []string{
	"help", "quit", "providers", "model", "memory",
	"skills", "mcp", "agents", "reasoning", "resume", "clear",
}
