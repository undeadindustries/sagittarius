package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/agent"
	"github.com/undeadindustries/sagittarius/internal/credentials"
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

	runner, _, settings, runtime, sessID, _, err := buildRunner(context.Background(), runnerOptions{interactive: true})
	if err != nil {
		t.Fatalf("buildRunner: %v", err)
	}
	defer func() { _ = runtime.Close() }()

	if !agent.NeedsProviderSetup(context.Background(), settings) {
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
	if err := os.Chdir(project); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Setenv("SAGITTARIUS_HOME", home)
	withoutEnv(t, "SAGITTARIUS_SESSION_ID")
	withEmptyCredentials(t)

	resumedID := "resumed-session-abc12345"
	chatsDir, err := session.ChatsDir(project)
	if err != nil {
		t.Fatalf("ChatsDir: %v", err)
	}
	rec := session.NewRecorder(chatsDir, resumedID, session.ProjectHash(project))
	rec.RecordUserMessage("hello from prior session")

	runner, _, _, runtime, sessID, _, err := buildRunner(context.Background(), runnerOptions{
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
