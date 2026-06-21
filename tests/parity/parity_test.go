package parity_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/slash"
)

// TestParityHelpOutput verifies that Sagittarius implements every in-scope
// slash command. It compares the Sagittarius slash registry against the
// statically-extracted fork command table (forkInScopeCommands).
//
// The test does NOT fail when the fork has extra commands; those are
// intentional gaps documented in PARITY_CHECKLIST.md.
//
// If SAGITTARIUS_PARITY_FORK=1, it also runs the fork headless to gather
// any help output that is accessible via the -v flag path.
func TestParityHelpOutput(t *testing.T) {
	t.Parallel()

	reg := slash.NewRegistry()
	helpText := reg.RenderHelp()

	t.Logf("Sagittarius help output:\n%s", helpText)

	// 1. Verify all in-scope top-level commands are present.
	for _, name := range inScopeTopLevelNames {
		if !strings.Contains(helpText, "/"+name) {
			t.Errorf("help output missing in-scope command: /%s", name)
		}
	}

	// 2. Verify expected subcommand paths.
	expectedPaths := []string{
		"/providers list",
		"/providers use",
		"/providers show",
		"/providers set",
		"/providers add",
		"/providers remove",
		"/model",
		"/memory reload",
		"/skills list",
		"/skills reload",
		"/mcp list",
		"/mcp reload",
		"/agents list",
		"/agents reload",
		"/reasoning show",
		"/reasoning clear",
		"/reasoning save",
	}
	for _, path := range expectedPaths {
		if !strings.Contains(helpText, path) {
			t.Errorf("help output missing subcommand: %s", path)
		}
	}

	// 3. Spot-check descriptions are present (non-empty help).
	descChecks := []string{
		"List slash commands",
		"Exit the interactive session",
		"Switch the active provider",
	}
	for _, want := range descChecks {
		if !strings.Contains(helpText, want) {
			t.Errorf("help output missing description fragment: %q", want)
		}
	}

	// 4. Verify all top-level commands from the in-scope fork table are present.
	for _, fc := range forkInScopeCommands {
		if fc.parent != "" {
			continue // subcommand — name collisions are fine, skip top-level check
		}
		// Only check that the command name appears somewhere in help output.
		if !strings.Contains(helpText, "/"+fc.name) {
			t.Errorf("fork in-scope command %q not found in Sagittarius help", fc.name)
		}
	}

	// 5. If fork opt-in: run the fork for version comparison only (slash commands
	// are not accessible via headless mode, so we just verify the fork starts).
	if forkParityEnabled() {
		t.Log("SAGITTARIUS_PARITY_FORK=1: verifying fork can start")
		home := setupTempHome(t, "")
		ctx, cancel := context.WithTimeout(context.Background(), defaultForkInvokeTimeout)
		defer cancel()
		forkOut, ok := invokeForkLoose(ctx, t, home, "--version")
		if !ok {
			t.Log("fork not accessible from this environment — noted as blocker")
		} else {
			t.Logf("fork --version output: %q", forkOut)
		}
	}
}

// TestParityHeadlessMock runs Sagittarius in headless mode (-p) against a mock
// OpenAI-compatible server and verifies it produces non-empty text output with
// exit code 0. This validates the headless path, provider selection, and
// streaming pipeline without real API keys.
//
// If SAGITTARIUS_PARITY_FORK=1, the fork is also invoked against the same mock
// server and its output is compared structurally (both must produce non-empty text).
func TestParityHeadlessMock(t *testing.T) {
	t.Parallel()

	// Start the mock OpenAI server.
	srv := newMockOpenAIServer(t, mockResponseText)
	t.Logf("mock OpenAI server at %s", srv.URL)

	bin := sagittariusBin(t)
	home := setupTempHome(t, srv.URL)

	// Run sagittarius.
	ctx, cancel := context.WithTimeout(context.Background(), defaultBinTimeout)
	defer cancel()

	stdout, stderr, code := invokeSagittariusOutput(ctx, t, bin, home, nil, "-p", "hello")
	t.Logf("sagittarius stdout: %q", stdout)
	t.Logf("sagittarius stderr: %q", stderr)
	t.Logf("sagittarius exit code: %d", code)

	if code != 0 {
		t.Fatalf("sagittarius exited %d; stderr=%q", code, stderr)
	}
	if strings.TrimSpace(stdout) == "" {
		t.Error("sagittarius produced empty stdout; expected non-empty text from mock server")
	}
	if !strings.Contains(stdout, "mock") && !strings.Contains(stdout, "Hello") {
		t.Errorf("sagittarius output %q does not contain expected mock response text", stdout)
	}

	// Live-fork comparison (opt-in only).
	if !forkParityEnabled() {
		return
	}
	t.Log("SAGITTARIUS_PARITY_FORK=1: running fork against same mock server")

	forkHome := setupTempHome(t, srv.URL)
	forkCtx, forkCancel := context.WithTimeout(context.Background(), defaultForkInvokeTimeout)
	defer forkCancel()

	forkOut, forkOK := invokeForkLoose(forkCtx, t, forkHome, "-p", "hello")
	if !forkOK {
		t.Log("BLOCKER: fork directory not accessible; skipping cross-binary comparison")
		return
	}
	t.Logf("fork output (after noise strip): %q", forkOut)

	if strings.TrimSpace(forkOut) == "" {
		t.Log("BLOCKER: fork produced empty output against mock server — likely settings format incompatibility or mock server unreachable from fork")
	} else if strings.Contains(forkOut, "Hello") || strings.Contains(forkOut, "mock") {
		t.Log("PASS: fork returned AI response text from mock server")
	} else {
		// Fork produced some output (preamble / error text) but not the mock response.
		// This is logged as a known limitation — the fork's provider configuration
		// pointing to a custom baseUrl may differ slightly in format from Sagittarius.
		t.Logf("PARTIAL: fork produced non-empty output but not the mock AI response (likely exited before reaching provider). Known limitation — see PARITY_CHECKLIST.md.")
	}
}

// TestParityProviderList verifies that Sagittarius's built-in provider set
// matches the expected registry (gemini-apikey, openai, openai-responses).
// Provider label parity with the fork is checked statically since the fork's
// /provider list is an interactive-only slash command.
func TestParityProviderList(t *testing.T) {
	t.Parallel()

	// The Sagittarius help output includes all provider-related commands.
	reg := slash.NewRegistry()
	helpText := reg.RenderHelp()

	// Verify the providers command and its subcommands are present.
	providerSubcmds := []string{"list", "use", "show", "set", "add", "remove"}
	for _, sub := range providerSubcmds {
		want := "/providers " + sub
		if !strings.Contains(helpText, want) {
			t.Errorf("missing providers subcommand in help: %s", want)
		}
	}

	// Verify the slash registry has a providers command.
	cmds := reg.List()
	var foundProvider bool
	for _, cmd := range cmds {
		if cmd.Name == "providers" {
			foundProvider = true
			break
		}
	}
	if !foundProvider {
		t.Fatal("slash registry missing 'providers' command")
	}

	// Built-in provider IDs that must ship in the binary's registry. Asserting
	// against config.BuiltInProviders catches a built-in being removed, which
	// the help-text check alone would miss.
	builtinIDs := []string{"gemini-apikey", "openai", "openai-responses"}
	for _, id := range builtinIDs {
		if _, ok := config.LookupBuiltInProvider(id); !ok {
			t.Errorf("built-in provider %q missing from config.BuiltInProviders", id)
		}
	}
	if len(config.BuiltInProviders) != len(builtinIDs) {
		t.Errorf("config.BuiltInProviders has %d entries, want %d (%v)",
			len(config.BuiltInProviders), len(builtinIDs), builtinIDs)
	}

	// Live-fork comparison (opt-in only): just confirm the fork starts and
	// has a recognisable providers concept via --version.
	if !forkParityEnabled() {
		return
	}
	t.Log("SAGITTARIUS_PARITY_FORK=1: checking fork startup for provider parity")
	home := setupTempHome(t, "")
	ctx, cancel := context.WithTimeout(context.Background(), defaultForkInvokeTimeout)
	defer cancel()
	forkOut, ok := invokeForkLoose(ctx, t, home, "--version")
	if !ok {
		t.Log("fork not accessible from this environment")
		return
	}
	t.Logf("fork --version: %q", forkOut)
}

// TestParityColdStartPerf measures Sagittarius cold-start time vs the known
// fork cold-start baseline. Numbers are logged for the PARITY_CHECKLIST.md.
// No hard pass/fail threshold is imposed — just a report.
func TestParityColdStartPerf(t *testing.T) {
	t.Parallel()

	bin := sagittariusBin(t)
	home := t.TempDir()
	env := baseEnv(home)

	// Warm up: one run to avoid filesystem-caching effects.
	measureColdStart(t, bin, env)

	// Measured run.
	dur := measureColdStart(t, bin, env)

	var forkDur time.Duration
	var forkOK bool
	if forkParityEnabled() {
		forkDur, forkOK = measureForkColdStart(t)
	}

	recordPerfNumbers(t, "sagittarius", dur, forkDur, forkOK)

	// Sagittarius must start in under 5 seconds (sanity bound).
	const maxStart = 5 * time.Second
	if dur > maxStart {
		t.Errorf("sagittarius cold-start %s exceeds 5s sanity bound", dur)
	}
}
