package tools

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCappedLogWriterStaysUnderLimit(t *testing.T) {
	t.Parallel()
	const limit = 256
	f := tempLog(t)
	w := newCappedLogWriter(f, limit)

	chunk := bytes.Repeat([]byte("a"), 100)
	for i := 0; i < 20; i++ {
		if _, err := w.Write(chunk); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > limit {
		t.Fatalf("size = %d, want <= %d", info.Size(), limit)
	}
	data, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, capMarker(limit)) {
		t.Fatalf("rotated log missing marker: %q", data)
	}
	if !bytes.Contains(data, []byte("aaa")) {
		t.Fatalf("rotated log missing recent bytes: %q", data)
	}
}

func TestCappedLogWriterKeepsNewestBytes(t *testing.T) {
	t.Parallel()
	const limit = 128
	f := tempLog(t)
	w := newCappedLogWriter(f, limit)

	if _, err := w.Write([]byte("OLD-PREFIX-SHOULD-DROP\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(bytes.Repeat([]byte("x"), 200)); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("NEW-TAIL")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("OLD-PREFIX")) {
		t.Fatalf("kept prefix after rotation: %q", data)
	}
	if !bytes.Contains(data, []byte("NEW-TAIL")) {
		t.Fatalf("missing newest bytes: %q", data)
	}
	if int64(len(data)) > limit {
		t.Fatalf("size = %d, want <= %d", len(data), limit)
	}
}

func TestCappedLogWriterOversizedSingleWrite(t *testing.T) {
	t.Parallel()
	const limit = 256
	f := tempLog(t)
	w := newCappedLogWriter(f, limit)

	payload := append(bytes.Repeat([]byte("H"), 400), []byte("TAIL-END")...)
	n, err := w.Write(payload)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len(payload) {
		t.Fatalf("Write accepted %d, want %d", n, len(payload))
	}
	data, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(data)) > limit {
		t.Fatalf("size = %d, want <= %d", len(data), limit)
	}
	if !bytes.Contains(data, []byte("TAIL-END")) {
		t.Fatalf("missing write tail after rotation: %q", data)
	}
	if bytes.Contains(data, bytes.Repeat([]byte("H"), 300)) {
		t.Fatalf("kept the oversized prefix instead of the tail: %q", data)
	}
}

func TestCappedLogWriterFitsWithoutRotate(t *testing.T) {
	t.Parallel()
	const limit = 1024
	f := tempLog(t)
	w := newCappedLogWriter(f, limit)
	if _, err := w.Write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello\n" {
		t.Fatalf("got %q, want exact write (no marker)", data)
	}
}

func TestSweepStaleShellArtifacts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	oldLog := filepath.Join(dir, "sagittarius-shell-old.log")
	newLog := filepath.Join(dir, "sagittarius-shell-new.log")
	oldJobs := filepath.Join(dir, "sagittarius-jobs-old.pid")
	other := filepath.Join(dir, "unrelated.log")
	for _, p := range []string{oldLog, newLog, oldJobs, other} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	oldTime := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(oldLog, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(oldJobs, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	removed := SweepStaleShellArtifacts(dir, 24*time.Hour)
	if removed != 2 {
		t.Fatalf("removed = %d, want 2", removed)
	}
	if _, err := os.Stat(oldLog); !os.IsNotExist(err) {
		t.Fatalf("old log still present: %v", err)
	}
	if _, err := os.Stat(oldJobs); !os.IsNotExist(err) {
		t.Fatalf("old jobs file still present: %v", err)
	}
	if _, err := os.Stat(newLog); err != nil {
		t.Fatalf("fresh log was swept: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("unrelated file was swept: %v", err)
	}
}

func TestSweepSkipsSymlinks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "target.log")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "sagittarius-shell-link.log")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(link, oldTime, oldTime); err != nil {
		// Chtimes on a symlink may follow; still assert the target survives.
		_ = err
	}
	SweepStaleShellArtifacts(dir, time.Hour)
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("symlink target removed: %v", err)
	}
}

func tempLog(t *testing.T) *os.File {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "cap-*.log")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func TestCapMarkerNamesLimit(t *testing.T) {
	t.Parallel()
	got := string(capMarker(8192))
	if !strings.Contains(got, "8192") {
		t.Fatalf("marker = %q, want it to name the byte limit", got)
	}
}

func TestApplyLogWriteStatusEmptyENOSPC(t *testing.T) {
	t.Parallel()
	out, errKey := applyLogWriteStatus("", syscall.ENOSPC)
	if out == emptyShellOutput || strings.Contains(out, emptyShellOutput) {
		t.Fatalf("output = %q, must not be %s when the log write failed", out, emptyShellOutput)
	}
	if !strings.Contains(out, "disk") {
		t.Fatalf("output = %q, want it to name disk full", out)
	}
	if errKey == "" {
		t.Fatal("want error key so the TUI card shows as failed")
	}
	if errKey != out {
		t.Fatalf("error key = %q, want it to match output %q", errKey, out)
	}
}

func TestApplyLogWriteStatusKeepsSnapshot(t *testing.T) {
	t.Parallel()
	const captured = "hello from before the disk filled"
	out, errKey := applyLogWriteStatus(captured, syscall.ENOSPC)
	if !strings.Contains(out, captured) {
		t.Fatalf("output = %q, want captured snapshot kept", out)
	}
	if !strings.Contains(out, "disk") {
		t.Fatalf("output = %q, want a write-failure warning", out)
	}
	if strings.Contains(out, emptyShellOutput) {
		t.Fatalf("output = %q, must not replace captured tail with %s", out, emptyShellOutput)
	}
	if errKey != "" {
		t.Fatalf("error key = %q, want empty so the captured tail stays in the card", errKey)
	}
}

func TestApplyLogWriteStatusNoError(t *testing.T) {
	t.Parallel()
	out, errKey := applyLogWriteStatus("", nil)
	if out != emptyShellOutput {
		t.Fatalf("empty success output = %q, want %s", out, emptyShellOutput)
	}
	if errKey != "" {
		t.Fatalf("error key = %q, want empty", errKey)
	}
	out, errKey = applyLogWriteStatus("ok", nil)
	if out != "ok" || errKey != "" {
		t.Fatalf("snapshot success = (%q, %q)", out, errKey)
	}
}

func TestLogWriteErrorMessageENOSPC(t *testing.T) {
	t.Parallel()
	msg := logWriteErrorMessage(syscall.ENOSPC)
	if !strings.Contains(msg, "disk") {
		t.Fatalf("message = %q, want disk full", msg)
	}
	if !strings.Contains(strings.ToUpper(msg), "ENOSPC") && !strings.Contains(msg, "no space") {
		t.Fatalf("message = %q, want ENOSPC or no-space wording", msg)
	}
}
