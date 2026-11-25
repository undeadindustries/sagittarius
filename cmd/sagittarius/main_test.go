package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/agent"
	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/credentials"
	"github.com/undeadindustries/sagittarius/internal/modes"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/session"
	"github.com/undeadindustries/sagittarius/internal/tools"
)

func withoutEnv(t *testing.T, key string) {
	t.Helper()
	prev, had := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("Unsetenv(%q): %v", key, err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, prev)
		}
	})
}

func withEmptyCredentials(t *testing.T) {
	t.Helper()
	credentials.SetStoreFactoryForTesting(func(string) credentials.Store {
		return emptyCredentialStore{}
	})
	t.Cleanup(credentials.ResetForTesting)
}

type emptyCredentialStore struct{}

func (emptyCredentialStore) Get(context.Context, string) (string, error) {
	return "", nil
}

func (emptyCredentialStore) Set(context.Context, string, string) error {
	return nil
}

func (emptyCredentialStore) Delete(context.Context, string) error {
	return nil
}

func (emptyCredentialStore) Available(context.Context) bool {
	return true
}

func TestRunVersionFlag(t *testing.T) {
	t.Parallel()
	if code := run([]string{"-v"}); code != 0 {
		t.Fatalf("run(-v) = %d, want 0", code)
	}
}

func TestNormalizeResumeArgs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"bare long at end", []string{"--resume"}, []string{"--resume=latest"}},
		{"bare short at end", []string{"-r"}, []string{"-r=latest"}},
		{"bare followed by flag", []string{"--resume", "-p", "hi"}, []string{"--resume=latest", "-p", "hi"}},
		{"value space form preserved", []string{"--resume", "1"}, []string{"--resume", "1"}},
		{"short value space form preserved", []string{"-r", "latest", "query"}, []string{"-r", "latest", "query"}},
		{"equals form untouched", []string{"--resume=abc"}, []string{"--resume=abc"}},
		{"unrelated args untouched", []string{"-p", "hello"}, []string{"-p", "hello"}},
		{"terminator stops rewrite", []string{"--", "--resume"}, []string{"--", "--resume"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := normalizeResumeArgs(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("normalizeResumeArgs(%v) = %v, want %v", tc.in, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("normalizeResumeArgs(%v) = %v, want %v", tc.in, got, tc.want)
				}
			}
		})
	}
}

func TestRunRejectsYoloWithApprovalMode(t *testing.T) {
	t.Setenv("SAGITTARIUS_HOME", t.TempDir())
	stderr := captureStderr(t, func() {
		if code := run([]string{"--yolo", "--approval-mode=default", "-p", "hi"}); code != 2 {
			t.Fatalf("run = %d, want 2", code)
		}
	})
	if !strings.Contains(stderr, "cannot use both --yolo and --approval-mode") {
		t.Fatalf("stderr = %q, want yolo/approval-mode conflict message", stderr)
	}
}

func TestRunRejectsApprovalModePlan(t *testing.T) {
	t.Setenv("SAGITTARIUS_HOME", t.TempDir())
	stderr := captureStderr(t, func() {
		if code := run([]string{"--approval-mode=plan", "-p", "hi"}); code != 2 {
			t.Fatalf("run = %d, want 2", code)
		}
	})
	if !strings.Contains(stderr, "--mode plan") {
		t.Fatalf("stderr = %q, want pointer to --mode plan", stderr)
	}
}

func TestRunRejectsUnknownMode(t *testing.T) {
	t.Setenv("SAGITTARIUS_HOME", t.TempDir())
	stderr := captureStderr(t, func() {
		if code := run([]string{"--mode=bogus", "-p", "hi"}); code != 2 {
			t.Fatalf("run = %d, want 2", code)
		}
	})
	if !strings.Contains(stderr, "unknown interaction mode") {
		t.Fatalf("stderr = %q, want unknown mode message", stderr)
	}
}

func TestRunSlashHelp(t *testing.T) {
	t.Setenv("SAGITTARIUS_HOME", t.TempDir())
	t.Setenv("GEMINI_PROVIDER", "gemini-apikey")
	t.Setenv("GEMINI_API_KEY", "test-key")
	withEmptyCredentials(t)

	out := captureStdout(t, func() {
		if code := runSlash("/help", runnerOptions{}); code != 0 {
			t.Fatalf("runSlash(/help) = %d, want 0", code)
		}
	})
	if !strings.Contains(out, "/help") {
		t.Fatalf("/help output missing command list:\n%s", out)
	}
}

func TestRunSlashModeShow(t *testing.T) {
	t.Setenv("SAGITTARIUS_HOME", t.TempDir())
	t.Setenv("GEMINI_PROVIDER", "gemini-apikey")
	t.Setenv("GEMINI_API_KEY", "test-key")
	withEmptyCredentials(t)

	out := captureStdout(t, func() {
		if code := runSlash("/mode show", runnerOptions{}); code != 0 {
			t.Fatalf("runSlash(/mode show) = %d, want 0", code)
		}
	})
	if !strings.Contains(strings.ToLower(out), "agent") {
		t.Fatalf("/mode show output missing mode:\n%s", out)
	}
}

// TestRunRefusesNestedInteractiveSession guards against the agent relaunching
// its own TUI. The shell tool runs commands in a PTY, so terminal detection says
// "interactive" inside a tool call and a bare `sagittarius` used to start a
// second full-screen session that blocked the parent turn until the
// auto-background threshold and then lingered as an orphan.
func TestRunRefusesNestedInteractiveSession(t *testing.T) {
	t.Setenv("SAGITTARIUS_HOME", t.TempDir())
	t.Setenv(tools.NestedAgentEnvVar, "1")

	stderr := captureStderr(t, func() {
		if code := run(nil); code != 2 {
			t.Fatalf("run = %d, want 2", code)
		}
	})
	if !strings.Contains(stderr, "inside a sagittarius tool call") {
		t.Fatalf("stderr = %q, want the nested-tool-call refusal", stderr)
	}
	if !strings.Contains(stderr, "-p") {
		t.Fatalf("stderr = %q, want it to point at the headless flag", stderr)
	}
}

// TestRunAllowsHeadlessInsideToolCall pins the other half of the guard: a tool
// call may still run sagittarius headlessly, which is the supported way for an
// agent to invoke its own binary.
func TestRunAllowsHeadlessInsideToolCall(t *testing.T) {
	t.Setenv("SAGITTARIUS_HOME", t.TempDir())
	t.Setenv(tools.NestedAgentEnvVar, "1")

	if code := run([]string{"-v"}); code != 0 {
		t.Fatalf("run(-v) = %d, want 0 with the nested marker set", code)
	}
}

func TestRunRejectsSlashWithPrompt(t *testing.T) {
	t.Setenv("SAGITTARIUS_HOME", t.TempDir())
	stderr := captureStderr(t, func() {
		if code := run([]string{"--slash", "/help", "-p", "hi"}); code != 2 {
			t.Fatalf("run = %d, want 2", code)
		}
	})
	if !strings.Contains(stderr, "--slash cannot be combined with a prompt") {
		t.Fatalf("stderr = %q, want slash/prompt conflict message", stderr)
	}
}

func TestBuildRunnerAllowsMissingProviderWhenInteractive(t *testing.T) {
	home := t.TempDir()
	sagDir := filepath.Join(home, ".sagittarius")
	if err := os.MkdirAll(sagDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	t.Setenv("SAGITTARIUS_HOME", home)
	withEmptyCredentials(t)

	runner, docs, runtime, sessID, _, err := buildRunner(context.Background(), runnerOptions{interactive: true})
	if err != nil {
		t.Fatalf("buildRunner: %v", err)
	}
	defer func() { _ = runtime.Close() }()

	if !agent.NeedsProviderSetup(context.Background(), docs.Merged()) {
		t.Fatal("expected setup to be needed with empty settings")
	}
	if runner.Model() != agent.PlaceholderModel() {
		t.Fatalf("model = %q, want placeholder %q", runner.Model(), agent.PlaceholderModel())
	}
	if sessID == "" {
		t.Fatal("expected non-empty session ID")
	}
}

func TestBuildRunnerResumeUsesResumedSessionID(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Chdir(project)
	t.Setenv("SAGITTARIUS_HOME", home)
	withoutEnv(t, "SAGITTARIUS_SESSION_ID")
	withEmptyCredentials(t)

	resumedID := "resumed-session-abc12345"
	chatsDir, err := session.ChatsDir(project)
	if err != nil {
		t.Fatalf("ChatsDir: %v", err)
	}
	rec := session.NewRecorder(chatsDir, resumedID, session.ProjectHash(project), "main")
	rec.RecordUserMessage("hello from prior session")

	runner, _, runtime, sessID, _, err := buildRunner(context.Background(), runnerOptions{
		interactive: true,
		resume:      session.ResumeLatest,
	})
	if err != nil {
		t.Fatalf("buildRunner: %v", err)
	}
	defer func() { _ = runtime.Close() }()

	if sessID != resumedID {
		t.Fatalf("session ID = %q, want resumed %q", sessID, resumedID)
	}
	if !runner.SnapshotEnabled() {
		t.Fatal("expected snapshots enabled for resumed session")
	}
}

func TestRunHeadlessMissingAPIKey(t *testing.T) {
	home := t.TempDir()
	sagDir := filepath.Join(home, ".sagittarius")
	if err := os.MkdirAll(sagDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	settings := filepath.Join(sagDir, "settings.json")
	if err := os.WriteFile(settings, []byte(`{"providers":{"active":"gemini-apikey"}}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Setenv("SAGITTARIUS_HOME", home)
	withoutEnv(t, "GOOGLE_API_KEY")
	withoutEnv(t, "GEMINI_API_KEY")
	withEmptyCredentials(t)

	stderr := captureStderr(t, func() {
		code := run([]string{"-p", "hello"})
		if code != 1 {
			t.Fatalf("run(-p) = %d, want 1", code)
		}
	})
	if !strings.Contains(stderr, credentials.ErrAPIKeyMissing.Error()) {
		t.Fatalf("stderr = %q, want api key missing message", stderr)
	}
}

func TestRunHeadlessPromptFlag(t *testing.T) {
	t.Parallel()

	gen := &headlessFakeGenerator{
		responses: []provider.StreamResponse{
			{TextDelta: "headless "},
			{TextDelta: "text"},
			{Done: true},
		},
	}

	runner, err := agent.NewRunner(agent.RunnerConfig{
		Generator: gen,
		Model:     "test-model",
		WorkDir:   t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	var buf bytes.Buffer
	if err := runner.RunHeadless(context.Background(), "hello", &buf); err != nil {
		t.Fatalf("RunHeadless: %v", err)
	}
	if buf.String() != "headless text" {
		t.Fatalf("output = %q", buf.String())
	}
}

type headlessFakeGenerator struct {
	responses []provider.StreamResponse
}

func (f *headlessFakeGenerator) GenerateContentStream(_ context.Context, _ *provider.GenerateRequest) (<-chan provider.StreamResponse, error) {
	ch := make(chan provider.StreamResponse, len(f.responses))
	for _, resp := range f.responses {
		ch <- resp
	}
	close(ch)
	return ch, nil
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	return captureFD(t, &os.Stderr, fn)
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	return captureFD(t, &os.Stdout, fn)
}

func captureFD(t *testing.T, fd **os.File, fn func()) string {
	t.Helper()
	old := *fd
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	*fd = w
	t.Cleanup(func() {
		*fd = old
	})

	done := make(chan struct{})
	var buf bytes.Buffer
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()

	fn()

	_ = w.Close()
	<-done
	return buf.String()
}

// stubGenerator returns canned StreamResponse batches, one per generate call.
type stubGenerator struct {
	batches [][]provider.StreamResponse
	call    int
}

func (g *stubGenerator) GenerateContentStream(_ context.Context, _ *provider.GenerateRequest) (<-chan provider.StreamResponse, error) {
	ch := make(chan provider.StreamResponse, 8)
	batch := g.batches[g.call]
	g.call++
	go func() {
		defer close(ch)
		for _, r := range batch {
			ch <- r
		}
	}()
	return ch, nil
}

func TestRunHeadlessJSONEmitsToolEvents(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "data.txt"), []byte("payload"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	gen := &stubGenerator{batches: [][]provider.StreamResponse{
		{
			{ToolCalls: []provider.ToolCall{{
				Name: tools.ReadFileToolName,
				Args: map[string]any{tools.ParamFilePath: "data.txt"},
			}}},
			{Done: true},
		},
		{
			{TextDelta: "read ok"},
			{Done: true},
		},
	}}

	runner, err := agent.NewRunner(agent.RunnerConfig{
		Generator:    gen,
		Model:        "test-model",
		WorkDir:      root,
		ApprovalMode: agent.ApprovalYolo,
		Interactive:  false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	out := captureStdout(t, func() {
		if code := runHeadlessJSON(context.Background(), runner, "read it", true); code != 0 {
			t.Fatalf("runHeadlessJSON = %d, want 0", code)
		}
	})

	for _, want := range []string{`"type":"tool_start"`, `"type":"tool_result"`, `"type":"text"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("stream-json output missing %s:\n%s", want, out)
		}
	}
}

func TestGoogleChatCLIGuards(t *testing.T) {
	// 1. Refuse yolo approval when chat is enabled, in both spellings
	if code := run([]string{"--google-chat", "--yolo"}); code != 2 {
		t.Errorf("run(--google-chat, --yolo) = %d, want 2", code)
	}
	if code := run([]string{"--google-chat-only", "-y"}); code != 2 {
		t.Errorf("run(--google-chat-only, -y) = %d, want 2", code)
	}
	if code := run([]string{"--google-chat", "--approval-mode=yolo"}); code != 2 {
		t.Errorf("run(--google-chat, --approval-mode=yolo) = %d, want 2", code)
	}
	if code := run([]string{"--google-chat-only", "--approval-mode=yolo"}); code != 2 {
		t.Errorf("run(--google-chat-only, --approval-mode=yolo) = %d, want 2", code)
	}

	// 2. Refuse nested agent child invocation
	t.Setenv(tools.NestedAgentEnvVar, "1")
	if code := run([]string{"--google-chat"}); code != 2 {
		t.Errorf("run(--google-chat) inside child = %d, want 2", code)
	}
	if code := run([]string{"--google-chat-only"}); code != 2 {
		t.Errorf("run(--google-chat-only) inside child = %d, want 2", code)
	}
}

func TestResolveCLIModelOverride(t *testing.T) {
	t.Parallel()

	s := &config.Settings{
		Providers: &config.ProvidersSettings{
			Active: string(config.BuiltInGeminiAPIKey),
			GeminiAPIKey: &config.ProviderInstanceConfig{
				ActiveModels: []string{"gemini-4-carbon"},
			},
			Custom: map[string]config.CustomProviderDefinition{
				"anthropic":  {BaseURL: "https://api.anthropic.com/v1/messages", WireFormat: config.WireFormatAnthropic, DefaultModel: "claude-opus-5-5"},
				"openrouter": {BaseURL: "https://openrouter.ai/api/v1/chat/completions", WireFormat: config.WireFormatOpenAIChat, DefaultModel: "anthropic/claude-opus-5.5"},
			},
			Extra: map[string]json.RawMessage{
				"anthropic":  json.RawMessage(`{"activeModels":["claude-opus-5-5","claude-sonnet-4-6"]}`),
				"openrouter": json.RawMessage(`{"activeModels":["anthropic/claude-opus-5.5","qwen/qwen3-coder"]}`),
			},
		},
	}

	cases := []struct {
		name     string
		base     string
		raw      string
		wantProv string
		wantMod  string
	}{
		{"plain model stays on base", "gemini-apikey", "gemini-4-argon", "gemini-apikey", "gemini-4-argon"},
		{"qualified provider/model routes to anthropic", "gemini-apikey", "anthropic/claude-opus-5-5", "anthropic", "claude-opus-5-5"},
		{"gemini display alias routes to gemini-apikey", "anthropic", "gemini/gemini-4-carbon", "gemini-apikey", "gemini-4-carbon"},
		{"openrouter slash model stays on openrouter", "openrouter", "anthropic/claude-opus-5.5", "openrouter", "anthropic/claude-opus-5.5"},
		{"unlisted openrouter slash model stays on openrouter", "openrouter", "deepseek/deepseek-v4", "openrouter", "deepseek/deepseek-v4"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotProv, gotMod := resolveCLIModelOverride(s, tc.base, tc.raw)
			if gotProv != tc.wantProv || gotMod != tc.wantMod {
				t.Fatalf("resolveCLIModelOverride(%q, %q) = (%q, %q), want (%q, %q)",
					tc.base, tc.raw, gotProv, gotMod, tc.wantProv, tc.wantMod)
			}
		})
	}
}

// TestBuildRunnerPreservesProjectPinsAndRespectsCLIModel guards Bugs C and E:
// (1) startup must prune docs.Project against docs.Merged() so valid project
// pins to global custom providers or curated models survive launch;
// (2) CLI --model (-m) must not be routed to a different mode's provider
// override, and must support <provider>/<model> qualification.
func TestBuildRunnerPreservesProjectPinsAndRespectsCLIModel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SAGITTARIUS_HOME", home)
	t.Setenv("GEMINI_API_KEY", "test-gem-key")
	t.Setenv("ANTHROPIC_API_KEY", "test-anth-key")
	withEmptyCredentials(t)

	projDir := t.TempDir()
	t.Chdir(projDir)

	globalSettings := `{
  "providers": {
    "active": "gemini-apikey",
    "gemini-apikey": {
      "model": "gemini-4-carbon",
      "activeModels": ["gemini-4-carbon", "gemini-4-argon"]
    },
    "custom": {
      "anthropic": {
        "displayName": "Anthropic",
        "baseUrl": "https://api.anthropic.com/v1/messages",
        "apiKeyEnvVar": "ANTHROPIC_API_KEY",
        "wireFormat": "anthropic",
        "defaultModel": "claude-opus-5-5"
      }
    },
    "anthropic": {
      "model": "claude-opus-5-5",
      "activeModels": ["claude-opus-5-5", "claude-sonnet-4-6"]
    }
  }
}`
	globalSagDir := filepath.Join(home, ".sagittarius")
	if err := os.MkdirAll(globalSagDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalSagDir, "settings.json"), []byte(globalSettings), 0o600); err != nil {
		t.Fatal(err)
	}

	projSagDir := filepath.Join(projDir, ".sagittarius")
	if err := os.MkdirAll(projSagDir, 0o700); err != nil {
		t.Fatal(err)
	}
	projSettings := `{
  "sagittarius": {
    "modes": {
      "plan": { "provider": "anthropic", "model": "claude-sonnet-4-6" },
      "ask": { "provider": "gemini-apikey", "model": "gemini-4-argon" }
    }
  }
}`
	if err := os.WriteFile(filepath.Join(projSagDir, "settings.json"), []byte(projSettings), 0o600); err != nil {
		t.Fatal(err)
	}

	planMode := modes.ModePlan
	runner, docs, rt, _, _, err := buildRunner(context.Background(), runnerOptions{
		modelOverride: "gemini-4-argon",
		modeOverride:  &planMode,
	})
	if err != nil {
		t.Fatalf("buildRunner(-m gemini-4-argon --mode plan): %v", err)
	}
	_ = runner.Close()
	_ = rt.Close()

	if got := runner.ActiveProviderID(); got != string(config.BuiltInGeminiAPIKey) {
		t.Fatalf("ActiveProviderID = %q, want gemini-apikey (CLI -m must beat plan's anthropic provider override)", got)
	}
	if got := runner.Model(); got != "gemini-4-argon" {
		t.Fatalf("Model = %q, want gemini-4-argon", got)
	}
	if docs.Project.Sagittarius.Modes.Plan.Model != "claude-sonnet-4-6" {
		t.Fatalf("startup pruning destroyed valid project plan pin: %+v", docs.Project.Sagittarius.Modes.Plan)
	}
	if docs.Project.Sagittarius.Modes.Ask.Model != "gemini-4-argon" {
		t.Fatalf("startup pruning destroyed valid project ask pin: %+v", docs.Project.Sagittarius.Modes.Ask)
	}

	runner2, _, rt2, _, _, err := buildRunner(context.Background(), runnerOptions{
		modelOverride: "anthropic/claude-sonnet-4-6",
	})
	if err != nil {
		t.Fatalf("buildRunner(-m anthropic/claude-sonnet-4-6): %v", err)
	}
	_ = runner2.Close()
	_ = rt2.Close()

	if got := runner2.ActiveProviderID(); got != "anthropic" {
		t.Fatalf("ActiveProviderID = %q, want anthropic", got)
	}
	if got := runner2.Model(); got != "claude-sonnet-4-6" {
		t.Fatalf("Model = %q, want claude-sonnet-4-6", got)
	}
}

// TestBuildRunnerCLIQualifiedModelSkipsOnboardingWhenBaseLacksKey guards
// Bugbot #2: interactive launch with `-m anthropic/claude-opus-5-5` when
// persisted providers.active (gemini-apikey) has no API key must route to
// anthropic, split the model id, and skip onboarding.
func TestBuildRunnerCLIQualifiedModelSkipsOnboardingWhenBaseLacksKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SAGITTARIUS_HOME", home)
	withoutEnv(t, "GEMINI_API_KEY")
	withoutEnv(t, "GOOGLE_API_KEY")
	t.Setenv("ANTHROPIC_API_KEY", "test-anth-key")
	withEmptyCredentials(t)

	projDir := t.TempDir()
	t.Chdir(projDir)

	globalSagDir := filepath.Join(home, ".sagittarius")
	if err := os.MkdirAll(globalSagDir, 0o700); err != nil {
		t.Fatal(err)
	}
	globalSettings := `{
  "providers": {
    "active": "gemini-apikey",
    "custom": {
      "anthropic": {
        "displayName": "Anthropic",
        "baseUrl": "https://api.anthropic.com/v1/messages",
        "apiKeyEnvVar": "ANTHROPIC_API_KEY",
        "wireFormat": "anthropic",
        "defaultModel": "claude-opus-5-5"
      }
    },
    "anthropic": {
      "model": "claude-opus-5-5",
      "activeModels": ["claude-opus-5-5"]
    }
  }
}`
	if err := os.WriteFile(filepath.Join(globalSagDir, "settings.json"), []byte(globalSettings), 0o600); err != nil {
		t.Fatal(err)
	}

	runner, docs, rt, _, baseProviderID, err := buildRunner(context.Background(), runnerOptions{
		interactive:   true,
		modelOverride: "anthropic/claude-opus-5-5",
	})
	if err != nil {
		t.Fatalf("buildRunner: %v", err)
	}
	defer func() {
		_ = runner.Close()
		_ = rt.Close()
	}()

	if startupNeedsOnboarding(context.Background(), docs, runner) {
		t.Fatal("startupNeedsOnboarding = true, want false when CLI-selected anthropic provider has a valid key")
	}
	if got := runner.ActiveProviderID(); got != "anthropic" {
		t.Fatalf("ActiveProviderID = %q, want anthropic", got)
	}
	if got := runner.Model(); got != "claude-opus-5-5" {
		t.Fatalf("Model = %q, want claude-opus-5-5 (unsplit provider prefix leaked)", got)
	}
	if got := startupRuntimeProviderID(docs, runner.ActiveProviderID(), baseProviderID); got != "anthropic" {
		t.Fatalf("startupRuntimeProviderID = %q, want anthropic", got)
	}
}
