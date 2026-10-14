package session_test

import (
	"os"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/session"
)

// TestAttachToFileAppendsWithoutNewHeader pins the in-process switch contract:
// the recorder retargets an existing session file, later turns land in it, and
// it keeps exactly one metadata header so its StartTime and title are unchanged.
func TestAttachToFileAppendsWithoutNewHeader(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	departing := session.NewRecorder(dir, "session-a", "hash", "main")
	departing.RecordUserMessage("message in A")

	target := session.NewRecorder(dir, "session-b", "hash", "main")
	target.RecordUserMessage("first message in B")
	targetPath := target.FilePath()
	if err := target.SetSummary("Target title"); err != nil {
		t.Fatalf("SetSummary: %v", err)
	}
	headersBefore := countHeaders(t, targetPath)

	if err := departing.AttachToFile(targetPath, "session-b", "Target title"); err != nil {
		t.Fatalf("AttachToFile: %v", err)
	}
	departing.RecordUserMessage("message after switch")

	if got := departing.SessionID(); got != "session-b" {
		t.Errorf("SessionID = %q, want session-b", got)
	}
	if got := departing.FilePath(); got != targetPath {
		t.Errorf("FilePath = %q, want %q", got, targetPath)
	}
	if got := departing.Summary(); got != "Target title" {
		t.Errorf("Summary = %q, want the target's title", got)
	}
	if got := countHeaders(t, targetPath); got != headersBefore {
		t.Errorf("header lines = %d after attach, want %d (attach must not write a header)", got, headersBefore)
	}
	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if !strings.Contains(string(data), "message after switch") {
		t.Errorf("post-switch turn was not appended to the target file")
	}
}

// TestAttachToFileRejectsEmptyTarget guards the input contract: an empty path or
// id must not silently retarget the recorder at nothing.
func TestAttachToFileRejectsEmptyTarget(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, path, id string
	}{
		{name: "empty path", path: "", id: "session-b"},
		{name: "blank path", path: "   ", id: "session-b"},
		{name: "empty id", path: "/tmp/x.jsonl", id: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := session.NewRecorder(t.TempDir(), "session-a", "hash", "main")
			before := rec.FilePath()
			if err := rec.AttachToFile(tc.path, tc.id, ""); err == nil {
				t.Fatalf("AttachToFile(%q, %q) = nil, want error", tc.path, tc.id)
			}
			if rec.FilePath() != before || rec.SessionID() != "session-a" {
				t.Errorf("rejected attach must leave the recorder unchanged")
			}
		})
	}
}

// countHeaders counts metadata header lines (the only lines carrying a
// top-level sessionId key) in a session file.
func countHeaders(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, `{"sessionId"`) {
			n++
		}
	}
	return n
}
