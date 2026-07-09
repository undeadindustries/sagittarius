package session_test

import (
	"testing"
	"time"

	"github.com/undeadindustries/sagittarius/internal/grill"
	"github.com/undeadindustries/sagittarius/internal/session"
)

// TestGrillSnapshotPersistsAndLoads verifies that a grill-me session snapshot
// written via Recorder.SetGrill survives a LoadSession reload, so --resume can
// restore an in-progress interrogation (internal/agent's InitialGrill wiring).
func TestGrillSnapshotPersistsAndLoads(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	hash := session.ProjectHash(dir)
	rec := session.NewRecorder(dir, "grill-persist-test", hash)

	rec.RecordUserMessage("/grill widget pricing")

	g := &grill.Session{
		Topic:         "widget pricing",
		Status:        grill.StatusActive,
		QuestionCount: 2,
		StartedAt:     time.Now(),
		Decisions: []grill.Decision{
			{Question: "Per-seat or flat-rate?", Answer: "Flat-rate", Rationale: "simpler to explain"},
		},
	}
	if err := rec.SetGrill(g.ToSnapshot()); err != nil {
		t.Fatalf("SetGrill: %v", err)
	}

	record, err := session.LoadSession(rec.FilePath())
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if record.Grill == nil {
		t.Fatal("record.Grill is nil after SetGrill + reload")
	}
	if record.Grill.Topic != "widget pricing" {
		t.Errorf("Grill.Topic = %q, want %q", record.Grill.Topic, "widget pricing")
	}
	if record.Grill.Status != string(grill.StatusActive) {
		t.Errorf("Grill.Status = %q, want %q", record.Grill.Status, grill.StatusActive)
	}
	if record.Grill.QuestionCount != 2 {
		t.Errorf("Grill.QuestionCount = %d, want 2", record.Grill.QuestionCount)
	}
	if len(record.Grill.Decisions) != 1 || record.Grill.Decisions[0].Answer != "Flat-rate" {
		t.Fatalf("Grill.Decisions = %+v, want one Flat-rate decision", record.Grill.Decisions)
	}

	restored := grill.FromSnapshot(record.Grill)
	if restored == nil || restored.Topic != "widget pricing" || restored.Status != grill.StatusActive {
		t.Fatalf("FromSnapshot round trip = %+v", restored)
	}
}

// TestGrillSnapshotUpdatesOverwritePriorState verifies later SetGrill calls
// (e.g. pause -> resume -> done) win, matching the goal metadata update
// semantics already relied on for --resume.
func TestGrillSnapshotUpdatesOverwritePriorState(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	hash := session.ProjectHash(dir)
	rec := session.NewRecorder(dir, "grill-overwrite-test", hash)

	active := &grill.Session{Topic: "onboarding", Status: grill.StatusActive, StartedAt: time.Now()}
	if err := rec.SetGrill(active.ToSnapshot()); err != nil {
		t.Fatalf("SetGrill(active): %v", err)
	}
	paused := &grill.Session{Topic: "onboarding", Status: grill.StatusPaused, StartedAt: active.StartedAt, Note: "taking a break"}
	if err := rec.SetGrill(paused.ToSnapshot()); err != nil {
		t.Fatalf("SetGrill(paused): %v", err)
	}

	record, err := session.LoadSession(rec.FilePath())
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if record.Grill == nil || record.Grill.Status != string(grill.StatusPaused) {
		t.Fatalf("Grill = %+v, want latest paused status", record.Grill)
	}
	if record.Grill.Note != "taking a break" {
		t.Errorf("Grill.Note = %q, want %q", record.Grill.Note, "taking a break")
	}
}

// TestGrillSnapshotAbsentByDefault verifies sessions without any grill
// activity load with a nil Grill snapshot (no spurious empty struct).
func TestGrillSnapshotAbsentByDefault(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	hash := session.ProjectHash(dir)
	rec := session.NewRecorder(dir, "no-grill-test", hash)
	rec.RecordUserMessage("hello")

	record, err := session.LoadSession(rec.FilePath())
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if record.Grill != nil {
		t.Fatalf("record.Grill = %+v, want nil when no grill session was ever set", record.Grill)
	}
}
