package hooks_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/undeadindustries/sagittarius/internal/hooks"
)

func TestMain(m *testing.M) {
	if os.Getenv("SAGITTARIUS_HOOK_FAKE") == "1" {
		runFakeHook()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func runFakeHook() {
	mode := os.Getenv("HOOK_MODE")
	switch mode {
	case "json_allow":
		fmt.Print(`{"decision":"allow","systemMessage":"all good"}`)
	case "json_deny":
		fmt.Print(`{"decision":"deny","reason":"access denied"}`)
	case "json_context":
		fmt.Print(`{"hookSpecificOutput":{"additionalContext":"extra instructions"}}`)
	case "json_modify_tool":
		fmt.Print(`{"hookSpecificOutput":{"tool_input":{"file_path":"modified.txt"}}}`)
	case "plain_text":
		fmt.Print("Plain text output from hook")
	case "exit_1":
		fmt.Fprint(os.Stderr, "non-blocking warning")
		os.Exit(1)
	case "exit_2":
		fmt.Fprint(os.Stderr, "hard block system failure")
		os.Exit(2)
	case "sleep_timeout":
		time.Sleep(30 * time.Second)
		fmt.Print(`{"decision":"allow"}`)
	case "exit_2_json_allow":
		fmt.Print(`{"decision":"allow","systemMessage":"ignore me"}`)
		fmt.Fprint(os.Stderr, "policy violation")
		os.Exit(2)
	case "marker":
		fmt.Printf(`{"systemMessage":%q}`, "ran:"+os.Getenv("HOOK_MARKER"))
	case "echo_env":
		sagDir := os.Getenv("SAGITTARIUS_PROJECT_DIR")
		gemDir := os.Getenv("GEMINI_PROJECT_DIR")
		fmt.Printf(`{"systemMessage":"sag=%s,gem=%s"}`, sagDir, gemDir)
	default:
		fmt.Print(`{"decision":"allow"}`)
	}
}

func fakeHookCommand(mode string) string {
	return fmt.Sprintf("SAGITTARIUS_HOOK_FAKE=1 HOOK_MODE=%s %s", mode, os.Args[0])
}

func TestExecuteHook_JSONOutput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	runner := hooks.NewRunner(dir)
	cfg := hooks.HookConfig{
		Type:    hooks.TypeCommand,
		Command: fakeHookCommand("json_allow"),
		Name:    "test-allow",
	}
	input := hooks.NewHookInput("sess-1", filepath.Join(dir, "transcript.jsonl"), dir, hooks.EventBeforeAgent, 1)

	res := runner.ExecuteHook(context.Background(), cfg, hooks.EventBeforeAgent, input)
	if !res.Success {
		t.Fatalf("expected success, got err: %v", res.Error)
	}
	if res.Output == nil {
		t.Fatal("expected non-nil output")
	}
	if res.Output.Decision != hooks.DecisionAllow {
		t.Errorf("expected decision allow, got %q", res.Output.Decision)
	}
	if res.Output.SystemMessage != "all good" {
		t.Errorf("expected systemMessage 'all good', got %q", res.Output.SystemMessage)
	}
}

func TestExecuteHook_PlainTextOutput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	runner := hooks.NewRunner(dir)
	cfg := hooks.HookConfig{
		Type:    hooks.TypeCommand,
		Command: fakeHookCommand("plain_text"),
		Name:    "test-plain",
	}
	input := hooks.NewHookInput("sess-1", filepath.Join(dir, "transcript.jsonl"), dir, hooks.EventBeforeAgent, 1)

	res := runner.ExecuteHook(context.Background(), cfg, hooks.EventBeforeAgent, input)
	if !res.Success {
		t.Fatalf("expected success, got err: %v", res.Error)
	}
	if res.OutputFormat != "text" {
		t.Errorf("expected format text, got %q", res.OutputFormat)
	}
	if res.Output.SystemMessage != "Plain text output from hook" {
		t.Errorf("expected Plain text output from hook, got %q", res.Output.SystemMessage)
	}
}

func TestExecuteHook_ExitCodes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	runner := hooks.NewRunner(dir)
	input := hooks.NewHookInput("sess-1", filepath.Join(dir, "transcript.jsonl"), dir, hooks.EventBeforeAgent, 1)

	// Exit code 1
	cfg1 := hooks.HookConfig{
		Type:    hooks.TypeCommand,
		Command: fakeHookCommand("exit_1"),
	}
	res1 := runner.ExecuteHook(context.Background(), cfg1, hooks.EventBeforeAgent, input)
	if res1.ExitCode != 1 {
		t.Errorf("expected exit code 1, got %d", res1.ExitCode)
	}
	if res1.Output == nil || res1.Output.SystemMessage != "Warning: non-blocking warning" {
		sysMsg := ""
		if res1.Output != nil {
			sysMsg = res1.Output.SystemMessage
		}
		t.Errorf("expected Warning prefix, got %q", sysMsg)
	}

	// Exit code 2
	cfg2 := hooks.HookConfig{
		Type:    hooks.TypeCommand,
		Command: fakeHookCommand("exit_2"),
	}
	res2 := runner.ExecuteHook(context.Background(), cfg2, hooks.EventBeforeAgent, input)
	if res2.ExitCode != 2 {
		t.Errorf("expected exit code 2, got %d", res2.ExitCode)
	}
	if res2.Output == nil || !res2.Output.IsBlocking() {
		t.Error("expected exit code 2 to be blocking")
	}
	if res2.Output.Reason != "hard block system failure" {
		t.Errorf("expected reason 'hard block system failure', got %q", res2.Output.Reason)
	}
}

func TestExecuteHook_Timeout(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	runner := hooks.NewRunner(dir)
	cfg := hooks.HookConfig{
		Type:    hooks.TypeCommand,
		Command: fakeHookCommand("sleep_timeout"),
		Timeout: 1, // seconds, matching gemini-cli and Claude Code
	}
	input := hooks.NewHookInput("sess-1", filepath.Join(dir, "transcript.jsonl"), dir, hooks.EventBeforeAgent, 1)

	res := runner.ExecuteHook(context.Background(), cfg, hooks.EventBeforeAgent, input)
	if res.Success {
		t.Error("expected hook to time out and fail")
	}
	if res.Error == nil || !strings.Contains(res.Error.Error(), "timed out") {
		t.Errorf("expected timeout error, got %v", res.Error)
	}
}

func TestExecuteHook_EnvVariables(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	runner := hooks.NewRunner(dir)
	cfg := hooks.HookConfig{
		Type:    hooks.TypeCommand,
		Command: fakeHookCommand("echo_env"),
	}
	input := hooks.NewHookInput("sess-1", filepath.Join(dir, "transcript.jsonl"), dir, hooks.EventBeforeAgent, 1)

	res := runner.ExecuteHook(context.Background(), cfg, hooks.EventBeforeAgent, input)
	if !res.Success {
		t.Fatalf("expected success, got err: %v", res.Error)
	}
	expected := fmt.Sprintf("sag=%s,gem=%s", dir, dir)
	if !strings.Contains(res.Output.SystemMessage, expected) {
		t.Errorf("expected env vars injected correctly %q, got %q", expected, res.Output.SystemMessage)
	}
}

func TestExecuteHooksSequential_Threading(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	runner := hooks.NewRunner(dir)
	configs := []hooks.HookConfig{
		{
			Type:    hooks.TypeCommand,
			Command: fakeHookCommand("json_context"),
		},
		{
			Type:    hooks.TypeCommand,
			Command: fakeHookCommand("json_allow"),
		},
	}
	input := hooks.NewHookInput("sess-1", filepath.Join(dir, "transcript.jsonl"), dir, hooks.EventBeforeAgent, 1)
	input.Prompt = "original prompt"

	results := runner.ExecuteHooksSequential(context.Background(), configs, hooks.EventBeforeAgent, input)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if !results[0].Success || !results[1].Success {
		t.Error("expected both hooks to succeed")
	}
}

// A hook that exits 2 is a hard block regardless of what it printed, so a
// well-formed {"decision":"allow"} on stdout must not lift the block.
func TestExecuteHook_ExitTwoOutranksAllowJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	runner := hooks.NewRunner(dir)
	cfg := hooks.HookConfig{
		Type:    hooks.TypeCommand,
		Command: fakeHookCommand("exit_2_json_allow"),
	}
	input := hooks.NewHookInput("sess-1", filepath.Join(dir, "transcript.jsonl"), dir, hooks.EventBeforeTool, 1)

	res := runner.ExecuteHook(context.Background(), cfg, hooks.EventBeforeTool, input)
	if res.Output == nil {
		t.Fatal("expected output")
	}
	if !res.Output.IsBlocking() {
		t.Errorf("exit 2 must block, got decision %q", res.Output.Decision)
	}
	if res.Output.Reason != "policy violation" {
		t.Errorf("expected stderr as reason, got %q", res.Output.Reason)
	}
}

// A sequential group is a chain: once a hook blocks, the action is not going to
// happen, so the rest of the chain must not run.
func TestExecuteHooksSequential_StopsOnBlock(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	runner := hooks.NewRunner(dir)
	configs := []hooks.HookConfig{
		{Type: hooks.TypeCommand, Command: fakeHookCommand("json_deny"), Name: "denier"},
		{Type: hooks.TypeCommand, Command: fakeHookCommand("json_allow"), Name: "should-not-run"},
	}
	input := hooks.NewHookInput("sess-1", filepath.Join(dir, "transcript.jsonl"), dir, hooks.EventBeforeTool, 1)

	results := runner.ExecuteHooksSequential(context.Background(), configs, hooks.EventBeforeTool, input)
	if len(results) != 1 {
		t.Fatalf("expected the chain to stop after the blocking hook, got %d results", len(results))
	}
	if !results[0].Output.IsBlocking() {
		t.Error("expected the first result to be blocking")
	}
}

func TestMatcher(t *testing.T) {
	t.Parallel()

	// Wildcards
	if !hooks.Match(hooks.EventBeforeTool, "*", "run_shell_command") {
		t.Error("expected * to match")
	}
	if !hooks.Match(hooks.EventBeforeTool, "", "run_shell_command") {
		t.Error("expected empty matcher to match")
	}

	// Tool regex matchers
	if !hooks.Match(hooks.EventBeforeTool, "^run_.*", "run_shell_command") {
		t.Error("expected regex ^run_.* to match run_shell_command")
	}
	if hooks.Match(hooks.EventBeforeTool, "^write_.*", "run_shell_command") {
		t.Error("expected regex ^write_.* not to match run_shell_command")
	}

	// Lifecycle exact string matchers
	if !hooks.Match(hooks.EventSessionStart, "startup", "startup") {
		t.Error("expected exact match 'startup'")
	}
	if hooks.Match(hooks.EventSessionStart, "startup", "resume") {
		t.Error("expected exact match 'startup' not to match 'resume'")
	}
}
