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

func TestRunHeadlessMissingAPIKey(t *testing.T) {
	home := t.TempDir()
	geminiDir := filepath.Join(home, ".gemini")
	if err := os.MkdirAll(geminiDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	settings := filepath.Join(geminiDir, "settings.json")
	if err := os.WriteFile(settings, []byte(`{"providers":{"active":"gemini-apikey"}}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Setenv("GEMINI_CLI_HOME", home)
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
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	os.Stderr = w
	t.Cleanup(func() {
		os.Stderr = old
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
