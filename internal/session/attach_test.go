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

func TestNewManagerForResumePreservesRecorderMetadata(t *testing.T) {
	t.Setenv("SAGITTARIUS_HOME", t.TempDir())
	projectRoot := t.TempDir()
	chatsDir, err := session.ChatsDir(projectRoot)
	if err != nil {
		t.Fatalf("ChatsDir: %v", err)
	}

	rec := session.NewRecorderWithConfig(chatsDir, "sess-meta", session.ProjectHash(projectRoot), "main", session.RecorderConfig{
		AgentVersion:  "v0.29.5",
		PersonaPreset: "sysadmin",
	})
	rec.RecordUserMessage("hello")

	sel := session.NewSelector(chatsDir, "")
	res, err := sel.ResolveSession("sess-meta")
	if err != nil {
		t.Fatalf("ResolveSession: %v", err)
	}

	mgr, err := session.NewManagerForResumeWithConfig(projectRoot, "sess-meta", res, session.RecorderConfig{
		AgentVersion:  "v0.29.6",
		PersonaPreset: "programmer",
	})
	if err != nil {
		t.Fatalf("NewManagerForResumeWithConfig: %v", err)
	}
	got := mgr.Recorder()
	if got.Kind() != "main" {
		t.Errorf("Kind = %q, want main", got.Kind())
	}
	if got.AgentVersion() != "v0.29.5" {
		t.Errorf("AgentVersion = %q, want v0.29.5 from record", got.AgentVersion())
	}
	if got.PersonaPreset() != "sysadmin" {
		t.Errorf("PersonaPreset = %q, want sysadmin from record", got.PersonaPreset())
	}

	// Fallback when an older session file lacked AgentVersion/PersonaPreset.
	res.Record.AgentVersion = ""
	res.Record.PersonaPreset = ""
	res.Record.Kind = ""
	mgrFallback, err := session.NewManagerForResumeWithConfig(projectRoot, "sess-meta", res, session.RecorderConfig{
		AgentVersion:  "v0.29.6",
		PersonaPreset: "programmer",
	})
	if err != nil {
		t.Fatalf("NewManagerForResumeWithConfig(fallback): %v", err)
	}
	fb := mgrFallback.Recorder()
	if fb.Kind() != "main" || fb.AgentVersion() != "v0.29.6" || fb.PersonaPreset() != "programmer" {
		t.Errorf("fallback metadata = (%q, %q, %q), want (main, v0.29.6, programmer)",
			fb.Kind(), fb.AgentVersion(), fb.PersonaPreset())
	}

	fb.SetPersonaPreset("custom-reviewer")
	if fb.PersonaPreset() != "custom-reviewer" {
		t.Errorf("SetPersonaPreset = %q, want custom-reviewer", fb.PersonaPreset())
	}

	// Rotate writes AgentVersion and PersonaPreset into the new session file header.
	fb.Rotate()
	rotatedRec, err := session.LoadSession(fb.FilePath())
	if err != nil {
		t.Fatalf("LoadSession(rotated): %v", err)
	}
	if rotatedRec.AgentVersion != "v0.29.6" || rotatedRec.PersonaPreset != "custom-reviewer" {
		t.Errorf("rotated header metadata = (%q, %q), want (v0.29.6, custom-reviewer)",
			rotatedRec.AgentVersion, rotatedRec.PersonaPreset)
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
