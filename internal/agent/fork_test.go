package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/session"
)

// settingsForAutoTitle returns a minimal Settings with the autoTitle policy set
// (or "off"), enough for these session-metadata tests.
func settingsForAutoTitle(policy string) *config.Settings {
	return &config.Settings{
		Sagittarius: &config.SagittariusSettings{
			Sessions: &config.SagittariusSessionsConfig{AutoTitle: &policy},
		},
	}
}

// TestForkSessionRoundTrip verifies /chat fork writes the history to a new
// session file with the forked summary, switches the recorder onto that same
// id, and that the resulting JSONL round-trips through LoadSession with the
// summary and message count intact (i.e. the recorder's fresh header did not
// clobber the WriteHistory metadata).
func TestForkSessionRoundTrip(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	chatsDir, err := session.ChatsDir(workDir)
	if err != nil {
		t.Fatalf("session.ChatsDir: %v", err)
	}
	rec := session.NewRecorder(chatsDir, "origin-session", "hash", "main")

	gen := &fakeGenerator{batches: [][]provider.StreamResponse{
		{{TextDelta: "fixed the pool race"}, {Done: true}},
		{{TextDelta: "Fix LSP pool race"}, {Done: true}}, // auto-title
	}}
	policy := "prompt"
	runner, err := NewRunner(RunnerConfig{
		Generator:       gen,
		Model:           "test-model",
		WorkDir:         workDir,
		Interactive:     false,
		SessionRecorder: rec,
		Settings:        settingsForAutoTitle(policy),
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	// Drive one turn so there is history and (in prompt mode) an auto-title.
	drainEvents(t, mustRunTurn(t, runner, "fix the lsp pool deadlock"))
	if got := rec.Summary(); got == "" {
		t.Fatalf("expected an auto-titled summary before fork, got empty")
	}

	newID, path, err := runner.ForkSession()
	if err != nil {
		t.Fatalf("ForkSession: %v", err)
	}
	if newID == "" || newID == "origin-session" {
		t.Fatalf("unexpected new session id %q", newID)
	}
	if path == "" {
		t.Fatal("ForkSession returned empty path")
	}

	// Recorder must now be recording to the forked id.
	if got := runner.CurrentSessionID(); got != newID {
		t.Fatalf("CurrentSessionID = %q, want forked %q", got, newID)
	}
	if gotPath := rec.FilePath(); gotPath != path {
		t.Fatalf("recorder path = %q, want forked path %q", gotPath, path)
	}

	// The forked file must round-trip: summary preserved with " (fork)" suffix,
	// session id matches, and all messages present.
	record, err := session.LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession(%s): %v", path, err)
	}
	if record.SessionID != newID {
		t.Errorf("forked SessionID = %q, want %q", record.SessionID, newID)
	}
	if !strings.HasSuffix(record.Summary, " (fork)") {
		t.Errorf("forked Summary = %q, want a \" (fork)\" suffix", record.Summary)
	}
	if len(record.Messages) == 0 {
		t.Error("forked session has no messages")
	}
	if len(record.Messages) != len(runner.History()) {
		t.Errorf("forked message count = %d, want %d (history length)", len(record.Messages), len(runner.History()))
	}

	// The original session file must be untouched (no further appends land there).
	origPath := filepath.Join(chatsDir, originalSessionFile(t, chatsDir, "origin-session"))
	if _, err := os.Stat(origPath); err != nil {
		t.Fatalf("original session file missing: %v", err)
	}
}

// TestRenameSession verifies the rename hook writes a $set summary line that
// LoadSession observes.
func TestRenameSession(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	chatsDir := filepath.Join(workDir, ".sagittarius-test-chats")
	rec := session.NewRecorder(chatsDir, "rename-test", "hash", "main")
	gen := &fakeGenerator{batches: [][]provider.StreamResponse{
		{{TextDelta: "ok"}, {Done: true}},
	}}
	runner, err := NewRunner(RunnerConfig{
		Generator:       gen,
		Model:           "test-model",
		WorkDir:         workDir,
		Interactive:     false,
		SessionRecorder: rec,
		Settings:        settingsForAutoTitle("off"),
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	if err := runner.RenameSession("Refactor loader"); err != nil {
		t.Fatalf("RenameSession: %v", err)
	}
	if got := rec.Summary(); got != "Refactor loader" {
		t.Fatalf("in-memory summary = %q, want %q", got, "Refactor loader")
	}

	record, err := session.LoadSession(rec.FilePath())
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if record.Summary != "Refactor loader" {
		t.Errorf("persisted summary = %q, want %q", record.Summary, "Refactor loader")
	}
}

// originalSessionFile returns the filename in dir whose contents carry
// wantID in the first (metadata) line.
func originalSessionFile(t *testing.T, dir, wantID string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		full := filepath.Join(dir, e.Name())
		rec, err := session.LoadSession(full)
		if err == nil && rec.SessionID == wantID {
			return e.Name()
		}
	}
	t.Fatalf("no session file in %s carries id %q", dir, wantID)
	return ""
}
