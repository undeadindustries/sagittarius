package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/undeadindustries/sagittarius/internal/session"
	"github.com/undeadindustries/sagittarius/internal/storage"
)

// writeSessionFile mints a session JSONL under the project's chats dir. It
// records one user message, optionally a summary and a clean-exit marker, then
// backdates the file's LastUpdated metadata via a $set line so the recency
// check is controllable.
func writeSessionFile(t *testing.T, projectRoot, sessID, summary, lastUpdated string, cleanExit bool) string {
	t.Helper()
	chatsDir, err := session.ChatsDir(projectRoot)
	if err != nil {
		t.Fatalf("ChatsDir: %v", err)
	}
	rec := session.NewRecorder(chatsDir, sessID, "hash", "main")
	rec.RecordUserMessage("hello")
	if summary != "" {
		if err := rec.SetSummary(summary); err != nil {
			t.Fatalf("SetSummary: %v", err)
		}
	}
	if cleanExit {
		if err := rec.SetCleanExit(); err != nil {
			t.Fatalf("SetCleanExit: %v", err)
		}
	}
	// Backdate LastUpdated with a $set metadata line.
	metaLine, err := json.Marshal(session.SetRecord{Set: &session.MetadataRecord{LastUpdated: lastUpdated}})
	if err != nil {
		t.Fatalf("marshal $set: %v", err)
	}
	path := rec.FilePath()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := f.Write(append(metaLine, '\n')); err != nil {
		_ = f.Close()
		t.Fatalf("write $set: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return path
}

func tsAgo(d time.Duration) string {
	return time.Now().UTC().Add(-d).Format(time.RFC3339Nano)
}

// TestComputeResumeOffer_OffersForAbandoned verifies an abandoned, recent,
// unclean session is offered for resume.
func TestComputeResumeOffer_OffersForAbandoned(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SAGITTARIUS_HOME", home)
	projectRoot := t.TempDir()

	writeSessionFile(t, projectRoot, "sess-abandoned", "Fix LSP race", tsAgo(time.Hour), false)

	got := computeResumeOffer(projectRoot, "different-current")
	if !strings.Contains(got, "--resume") {
		t.Fatalf("expected a resume offer, got %q", got)
	}
	if !strings.Contains(got, "Fix LSP race") {
		t.Errorf("offer missing session title: %q", got)
	}
}

// TestComputeResumeOffer_SkipsCleanExit verifies a session that ended cleanly
// is never offered.
func TestComputeResumeOffer_SkipsCleanExit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SAGITTARIUS_HOME", home)
	projectRoot := t.TempDir()

	writeSessionFile(t, projectRoot, "sess-clean", "done work", tsAgo(time.Hour), true)

	if got := computeResumeOffer(projectRoot, "different-current"); got != "" {
		t.Fatalf("expected no offer for clean exit, got %q", got)
	}
}

// TestComputeResumeOffer_SkipsStale verifies an old session is not offered even
// when unclean.
func TestComputeResumeOffer_SkipsStale(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SAGITTARIUS_HOME", home)
	projectRoot := t.TempDir()

	writeSessionFile(t, projectRoot, "sess-stale", "old work", tsAgo(48*time.Hour), false)

	if got := computeResumeOffer(projectRoot, "different-current"); got != "" {
		t.Fatalf("expected no offer for stale session, got %q", got)
	}
}

// TestComputeResumeOffer_LiveSessionInfoLine verifies a session whose lock is
// held yields an informational line, never a resume offer.
func TestComputeResumeOffer_LiveSessionInfoLine(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SAGITTARIUS_HOME", home)
	projectRoot := t.TempDir()

	writeSessionFile(t, projectRoot, "sess-live", "live work", tsAgo(time.Hour), false)

	// Hold the liveness lock for that session in this process so Probe reports
	// ProbeLive.
	tmpDir, err := storage.ProjectTmpDir(projectRoot)
	if err != nil {
		t.Fatalf("ProjectTmpDir: %v", err)
	}
	release := session.Acquire(tmpDir, session.LivenessInfo{SessionID: "sess-live"})
	if release == nil {
		t.Fatal("failed to acquire liveness lock for test")
	}
	defer release()

	got := computeResumeOffer(projectRoot, "different-current")
	if strings.Contains(got, "--resume") {
		t.Fatalf("live session must not be offered for resume: %q", got)
	}
	if !strings.Contains(got, "already running") {
		t.Errorf("expected an informational 'already running' line, got %q", got)
	}
}
