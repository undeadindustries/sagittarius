package snapshot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestManager builds a Manager rooted at a temp dir, redirecting the
// Sagittarius home so the session index is isolated per test.
func newTestManager(t *testing.T) (*Manager, string) {
	t.Helper()
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("SAGITTARIUS_HOME", home)
	mgr, err := NewManager(root, "test-session", Options{MaxFileBytes: 1024})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return mgr, root
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotReplayFromIndex(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("SAGITTARIUS_HOME", home)

	// First "process": record a write.
	mgr1, err := NewManager(root, "shared-session", Options{MaxFileBytes: 1024})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	abs := filepath.Join(root, "hello.txt")
	mgr1.CaptureWrite(abs)
	writeFile(t, abs, "first\n")
	mgr1.CommitWrite(abs, "write_file")

	// Second "process": a fresh Manager with the same session id replays the
	// index and sees the change.
	mgr2, err := NewManager(root, "shared-session", Options{MaxFileBytes: 1024})
	if err != nil {
		t.Fatalf("NewManager (replay): %v", err)
	}
	diff, err := mgr2.Diff("")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !strings.Contains(diff, "hello.txt") || !strings.Contains(diff, "+first") {
		t.Fatalf("replayed diff missing change:\n%s", diff)
	}

	// And it can undo the replayed change, removing the file.
	restored, err := mgr2.Undo(1)
	if err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if len(restored) != 1 || restored[0] != "hello.txt" {
		t.Fatalf("Undo restored = %v, want [hello.txt]", restored)
	}
	if _, statErr := os.Stat(abs); !os.IsNotExist(statErr) {
		t.Fatalf("file still exists after undo (stat err = %v)", statErr)
	}

	// A third "process" replays the post-undo (empty) index: nothing to undo.
	mgr3, err := NewManager(root, "shared-session", Options{MaxFileBytes: 1024})
	if err != nil {
		t.Fatalf("NewManager (post-undo): %v", err)
	}
	if _, err := mgr3.Undo(1); err == nil {
		t.Fatal("expected nothing-to-undo after replaying post-undo index")
	}
}

func TestSnapshotTracksNewFile(t *testing.T) {
	mgr, root := newTestManager(t)
	abs := filepath.Join(root, "new.txt")

	mgr.CaptureWrite(abs) // file does not exist yet
	writeFile(t, abs, "hello\nworld\n")
	mgr.CommitWrite(abs, "write_file")

	got := mgr.ChangedFiles()
	if len(got) != 1 || got[0] != "new.txt" {
		t.Fatalf("ChangedFiles = %v, want [new.txt]", got)
	}

	diff, err := mgr.Diff("")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !strings.Contains(diff, "+hello") || !strings.Contains(diff, "+world") {
		t.Fatalf("diff missing added lines:\n%s", diff)
	}
}

func TestSnapshotDiffEditExistingFile(t *testing.T) {
	mgr, root := newTestManager(t)
	abs := filepath.Join(root, "a.txt")
	writeFile(t, abs, "one\ntwo\nthree\n")

	mgr.CaptureWrite(abs)
	writeFile(t, abs, "one\nTWO\nthree\n")
	mgr.CommitWrite(abs, "write_file")

	diff, err := mgr.Diff("")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !strings.Contains(diff, "-two") || !strings.Contains(diff, "+TWO") {
		t.Fatalf("unexpected diff:\n%s", diff)
	}
	if !strings.Contains(diff, " one") || !strings.Contains(diff, " three") {
		t.Fatalf("diff missing context lines:\n%s", diff)
	}
}

func TestSnapshotUndoRestoresBytes(t *testing.T) {
	mgr, root := newTestManager(t)
	abs := filepath.Join(root, "edit.txt")
	writeFile(t, abs, "original\n")

	mgr.CaptureWrite(abs)
	writeFile(t, abs, "changed\n")
	mgr.CommitWrite(abs, "write_file")

	restored, err := mgr.Undo(1)
	if err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if len(restored) != 1 || restored[0] != "edit.txt" {
		t.Fatalf("restored = %v, want [edit.txt]", restored)
	}
	data, _ := os.ReadFile(abs)
	if string(data) != "original\n" {
		t.Fatalf("file content = %q, want original", string(data))
	}
	// After full undo, /diff shows no changes.
	diff, _ := mgr.Diff("")
	if diff != "" {
		t.Fatalf("expected empty diff after undo, got:\n%s", diff)
	}
}

func TestSnapshotUndoNewFileRemovesIt(t *testing.T) {
	mgr, root := newTestManager(t)
	abs := filepath.Join(root, "created.txt")

	mgr.CaptureWrite(abs)
	writeFile(t, abs, "data\n")
	mgr.CommitWrite(abs, "write_file")

	if _, err := mgr.Undo(1); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if _, err := os.Stat(abs); !os.IsNotExist(err) {
		t.Fatalf("expected file removed, stat err = %v", err)
	}
}

func TestSnapshotUndoEmptyStack(t *testing.T) {
	mgr, _ := newTestManager(t)
	if _, err := mgr.Undo(1); err == nil {
		t.Fatal("expected error undoing empty stack")
	}
}

func TestSnapshotOversizedFileSkipped(t *testing.T) {
	mgr, root := newTestManager(t)
	abs := filepath.Join(root, "big.txt")
	big := strings.Repeat("x", 2048) // exceeds the 1024 cap

	mgr.CaptureWrite(abs)
	writeFile(t, abs, big)
	mgr.CommitWrite(abs, "write_file")

	diff, err := mgr.Diff("")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !strings.Contains(diff, "too large") {
		t.Fatalf("expected oversize note, got:\n%s", diff)
	}
	// Undo of an oversized (metadata-only) change reports it cannot revert.
	if _, err := mgr.Undo(1); err == nil {
		t.Fatal("expected error undoing oversized change")
	}
}

func TestSnapshotDiffPathFilter(t *testing.T) {
	mgr, root := newTestManager(t)
	a := filepath.Join(root, "src", "a.txt")
	b := filepath.Join(root, "docs", "b.txt")
	mgr.CaptureWrite(a)
	writeFile(t, a, "a\n")
	mgr.CommitWrite(a, "write_file")
	mgr.CaptureWrite(b)
	writeFile(t, b, "b\n")
	mgr.CommitWrite(b, "write_file")

	diff, err := mgr.Diff("docs")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if strings.Contains(diff, "src/a.txt") || !strings.Contains(diff, "docs/b.txt") {
		t.Fatalf("filter did not scope to docs:\n%s", diff)
	}
}

func TestUnifiedDiffIdentical(t *testing.T) {
	if got := UnifiedDiff("same\n", "same\n", "f"); got != "" {
		t.Fatalf("expected empty diff, got %q", got)
	}
}

// TestSnapshotRootCanonicalized verifies the manager root is symlink-resolved
// like tools.NewWorkspace, so captures keyed on the real (EvalSymlinks'd) path
// the scheduler produces are tracked even when the project is opened via a
// symlinked working directory.
func TestSnapshotRootCanonicalized(t *testing.T) {
	realDir := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(realDir, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	t.Setenv("SAGITTARIUS_HOME", t.TempDir())

	mgr, err := NewManager(link, "test-session", Options{MaxFileBytes: 1024})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// The scheduler resolves write targets through Workspace.ResolvePath, which
	// returns the real path — simulate that by using realDir, not the symlink.
	abs := filepath.Join(realDir, "f.txt")
	mgr.CaptureWrite(abs)
	writeFile(t, abs, "hi\n")
	mgr.CommitWrite(abs, "write_file")

	got := mgr.ChangedFiles()
	if len(got) != 1 || got[0] != "f.txt" {
		t.Fatalf("ChangedFiles = %v, want [f.txt] (capture rejected by uncanonical root)", got)
	}
}

// TestSnapshotUndoFailurePreservesStack verifies that a failed restore leaves
// the change on the undo stack so the user can retry after fixing the cause,
// rather than silently dropping it.
func TestSnapshotUndoFailurePreservesStack(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses file permission checks")
	}
	mgr, root := newTestManager(t)
	abs := filepath.Join(root, "edit.txt")
	writeFile(t, abs, "original\n")

	mgr.CaptureWrite(abs)
	writeFile(t, abs, "changed\n")
	mgr.CommitWrite(abs, "write_file")

	// Make the file unwritable so restore (os.WriteFile) fails.
	if err := os.Chmod(abs, 0o400); err != nil {
		t.Fatal(err)
	}
	restored, err := mgr.Undo(1)
	if err == nil {
		t.Fatal("expected undo error when restore fails")
	}
	if len(restored) != 0 {
		t.Fatalf("restored = %v, want none", restored)
	}

	// The change must still be tracked and retryable.
	if files := mgr.ChangedFiles(); len(files) != 1 || files[0] != "edit.txt" {
		t.Fatalf("ChangedFiles = %v, want [edit.txt] still tracked", files)
	}
	if err := os.Chmod(abs, 0o600); err != nil {
		t.Fatal(err)
	}
	restored, err = mgr.Undo(1)
	if err != nil {
		t.Fatalf("retry Undo: %v", err)
	}
	if len(restored) != 1 || restored[0] != "edit.txt" {
		t.Fatalf("retry restored = %v, want [edit.txt]", restored)
	}
	if data, _ := os.ReadFile(abs); string(data) != "original\n" {
		t.Fatalf("content = %q, want original after retry", string(data))
	}
}
