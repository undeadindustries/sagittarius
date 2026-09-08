package session_test

import (
	"testing"

	"github.com/undeadindustries/sagittarius/internal/session"
)

// TestScratchpadPersistsAndLoads verifies a note written via
// Recorder.SetScratchpad survives a LoadSession reload, so --resume restores
// the model's working memory (internal/agent's InitialScratchpad wiring).
func TestScratchpadPersistsAndLoads(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	rec := session.NewRecorder(dir, "scratchpad-persist-test", session.ProjectHash(dir), "main")
	rec.RecordUserMessage("start the migration")

	const note = "Migration plan: 1) drop the v1 index 2) backfill 3) flip the flag"
	if err := rec.SetScratchpad(note); err != nil {
		t.Fatalf("SetScratchpad: %v", err)
	}

	record, err := session.LoadSession(rec.FilePath())
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if record.Scratchpad != note {
		t.Errorf("Scratchpad = %q, want %q", record.Scratchpad, note)
	}
}

// TestScratchpadClearSurvivesMerge is the reason MetadataRecord.Scratchpad is a
// pointer: a later clear must win over an earlier note rather than being read
// as "this $set line did not touch the scratchpad".
func TestScratchpadClearSurvivesMerge(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	rec := session.NewRecorder(dir, "scratchpad-clear-test", session.ProjectHash(dir), "main")

	if err := rec.SetScratchpad("temporary note"); err != nil {
		t.Fatalf("SetScratchpad: %v", err)
	}
	if err := rec.SetScratchpad(""); err != nil {
		t.Fatalf("SetScratchpad(clear): %v", err)
	}

	record, err := session.LoadSession(rec.FilePath())
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if record.Scratchpad != "" {
		t.Errorf("Scratchpad = %q after clear, want empty", record.Scratchpad)
	}
}

// TestScratchpadAbsentByDefault verifies a session that never touched the
// scratchpad loads with an empty string rather than a spurious value.
func TestScratchpadAbsentByDefault(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	rec := session.NewRecorder(dir, "no-scratchpad-test", session.ProjectHash(dir), "main")
	rec.RecordUserMessage("hello")

	record, err := session.LoadSession(rec.FilePath())
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if record.Scratchpad != "" {
		t.Errorf("Scratchpad = %q, want empty", record.Scratchpad)
	}
}
