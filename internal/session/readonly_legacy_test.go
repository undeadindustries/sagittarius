package session_test

import (
	"os"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/session"
)

// TestLegacyConversationalLockIgnoredOnResume covers the AD-107 upgrade path.
// Sessions recorded before AD-107 can carry a readOnlyConversational key from
// the deleted phrase-inferred lock. Loading one must not resurrect a gate the
// user has no way to clear, and must not be mistaken for the durable posture.
func TestLegacyConversationalLockIgnoredOnResume(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	hash := session.ProjectHash(dir)
	rec := session.NewRecorder(dir, "legacy-readonly-test", hash, "main")
	rec.RecordUserMessage("don't change anything yet")

	// Append the pre-AD-107 metadata line by hand: the field no longer exists
	// on MetadataRecord, so nothing in the codebase can write it any more.
	f, err := os.OpenFile(rec.FilePath(), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open session file: %v", err)
	}
	_, writeErr := f.WriteString("{\"$set\":{\"readOnlyConversational\":true}}\n")
	closeErr := f.Close()
	if writeErr != nil {
		t.Fatalf("append legacy $set: %v", writeErr)
	}
	if closeErr != nil {
		t.Fatalf("close session file: %v", closeErr)
	}

	record, err := session.LoadSession(rec.FilePath())
	if err != nil {
		t.Fatalf("LoadSession with legacy readOnlyConversational key: %v", err)
	}
	if record.ReadOnly != nil {
		t.Fatalf("record.ReadOnly = %v, want nil (a legacy inferred lock must not become the durable posture)", *record.ReadOnly)
	}
	if len(record.Messages) != 1 {
		t.Fatalf("messages = %d, want 1 (the unknown key must not abort the load)", len(record.Messages))
	}
}

// TestDurableReadOnlyPostureStillPersists guards the trigger that survived
// AD-107: /readonly on must still be restored by --resume.
func TestDurableReadOnlyPostureStillPersists(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	hash := session.ProjectHash(dir)
	rec := session.NewRecorder(dir, "durable-readonly-test", hash, "main")
	rec.RecordUserMessage("hello")
	if err := rec.SetReadOnly(true); err != nil {
		t.Fatalf("SetReadOnly: %v", err)
	}

	record, err := session.LoadSession(rec.FilePath())
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if record.ReadOnly == nil || !*record.ReadOnly {
		t.Fatalf("record.ReadOnly = %v, want true", record.ReadOnly)
	}
}
