package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaybeSpillOutputLeavesSmallOutputUnchanged(t *testing.T) {
	t.Parallel()
	in := "hello\nworld\n"
	got := maybeSpillOutputN(in, t.TempDir(), 64)
	if got.truncated || got.spillPath != "" {
		t.Fatalf("small output spilled: %+v", got)
	}
	if got.output != in {
		t.Fatalf("output = %q, want %q", got.output, in)
	}
}

func TestMaybeSpillOutputTruncatesAndWritesFile(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	for i := 0; i < 200; i++ {
		b.WriteString("line of shell output that is reasonably long\n")
	}
	full := b.String()
	dir := t.TempDir()
	got := maybeSpillOutputN(full, dir, 256)
	if !got.truncated {
		t.Fatal("expected truncation")
	}
	if !strings.Contains(got.output, "bytes omitted") {
		t.Fatalf("missing omit marker: %q", got.output)
	}
	if !strings.Contains(got.output, got.spillPath) {
		t.Fatalf("marker missing spill path %q: %q", got.spillPath, got.output)
	}
	if !strings.Contains(got.output, "read_file") {
		t.Fatalf("marker missing read_file hint: %q", got.output)
	}
	if !strings.Contains(got.output, "lines total") {
		t.Fatalf("marker missing line count: %q (total_lines=%d)", got.output, got.totalLines)
	}
	if len(got.output) >= len(full) {
		t.Fatalf("truncated output not smaller: %d >= %d", len(got.output), len(full))
	}
	if got.spillPath == "" {
		t.Fatal("expected spill_file path")
	}
	if !strings.HasPrefix(got.spillPath, dir) {
		t.Fatalf("spill path %q not under %q", got.spillPath, dir)
	}
	data, err := os.ReadFile(got.spillPath)
	if err != nil {
		t.Fatalf("read spill: %v", err)
	}
	if string(data) != full {
		t.Fatal("spill file does not contain the full output")
	}
	if got.totalLines < 200 {
		t.Fatalf("total_lines = %d, want >= 200", got.totalLines)
	}
}

func TestMaybeSpillOutputTruncatesWithoutDir(t *testing.T) {
	t.Parallel()
	full := strings.Repeat("x", 400)
	got := maybeSpillOutputN(full, "", 80)
	if !got.truncated {
		t.Fatal("expected truncation without a spill dir")
	}
	if got.spillPath != "" {
		t.Fatalf("unexpected spill path %q", got.spillPath)
	}
	if !strings.Contains(got.output, "no spill file was written") {
		t.Fatalf("marker should say no spill file: %q", got.output)
	}
	if strings.Contains(got.output, "full output in spill file") {
		t.Fatalf("marker claimed a spill file: %q", got.output)
	}
}

func TestMaybeSpillOutputTruncatesWhenWriteFails(t *testing.T) {
	t.Parallel()
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := maybeSpillOutputN(strings.Repeat("y", 400), blocker, 80)
	if !got.truncated {
		t.Fatal("expected truncation when spill write fails")
	}
	if got.spillPath != "" {
		t.Fatalf("unexpected spill path %q", got.spillPath)
	}
	if !strings.Contains(got.output, "no spill file was written") {
		t.Fatalf("failed write still claimed a spill file: %q", got.output)
	}
}

func TestResolveSpillReadPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	okPath := filepath.Join(dir, spillFilePrefix+"abc123"+spillFileSuffix)
	if err := os.WriteFile(okPath, []byte("full\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, ok := resolveSpillReadPath(okPath, dir)
	if !ok || got != okPath {
		t.Fatalf("accepted path = %q ok=%v", got, ok)
	}

	wrongParent := filepath.Join(t.TempDir(), spillFilePrefix+"abc123"+spillFileSuffix)
	if err := os.WriteFile(wrongParent, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := resolveSpillReadPath(wrongParent, dir); ok {
		t.Fatal("accepted a file outside spillDir")
	}

	traversal := filepath.Join(dir, "..", filepath.Base(wrongParent))
	if _, ok := resolveSpillReadPath(traversal, dir); ok {
		t.Fatal("accepted a traversal path")
	}

	badName := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(badName, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := resolveSpillReadPath(badName, dir); ok {
		t.Fatal("accepted a non-spill filename")
	}

	if _, ok := resolveSpillReadPath(filepath.Base(okPath), dir); ok {
		t.Fatal("accepted a relative path")
	}

	link := filepath.Join(dir, spillFilePrefix+"link"+spillFileSuffix)
	if err := os.Symlink(okPath, link); err != nil {
		t.Fatal(err)
	}
	if _, ok := resolveSpillReadPath(link, dir); ok {
		t.Fatal("accepted a symlink")
	}
}

func TestWriteExclusiveRefusesExistingPath(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), spillFilePrefix+"exists"+spillFileSuffix)
	if err := os.WriteFile(path, []byte("planted"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeExclusive(path, []byte("hijack")); err == nil {
		t.Fatal("expected exclusive create to fail")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "planted" {
		t.Fatalf("existing file overwritten: %q", data)
	}
}

func TestApplySpillMetaAndHint(t *testing.T) {
	t.Parallel()
	result := map[string]any{"output": "short"}
	applySpillMeta(result, spillResult{truncated: true, totalLines: 12, spillPath: "/tmp/spill.log"})
	if result["truncated"] != true {
		t.Fatal("truncated not set")
	}
	if result["total_lines"] != 12 {
		t.Fatalf("total_lines = %v", result["total_lines"])
	}
	text := appendSpillHint("body", result)
	if !strings.Contains(text, "/tmp/spill.log") {
		t.Fatalf("hint missing path: %q", text)
	}
}

func TestBackgroundedResultMarkerNamesLogFile(t *testing.T) {
	t.Parallel()
	logPath := filepath.Join(t.TempDir(), "cmd.log")
	if err := os.WriteFile(logPath, []byte(strings.Repeat("x", maxModelOutputBytes+128)), 0o600); err != nil {
		t.Fatal(err)
	}
	got := backgroundedResult(42, logPath, true, 0, nil)
	out, _ := got["output"].(string)
	if strings.Contains(out, "full output in spill file") {
		t.Fatalf("backgrounded marker claimed a spill file: %q", out)
	}
	if !strings.Contains(out, logPath) {
		t.Fatalf("backgrounded marker missing log_file: %q", out)
	}
	if !strings.Contains(out, "still being written") {
		t.Fatalf("backgrounded marker missing live-log wording: %q", out)
	}
}

func TestFormatShellResultShowsSpillPath(t *testing.T) {
	t.Parallel()
	text, _, _ := formatShellResult(map[string]any{
		"output":     "ok",
		"spill_file": filepath.Join("tmp", "sagittarius-spill-test.log"),
	})
	if !strings.Contains(text, "sagittarius-spill-test.log") {
		t.Fatalf("UI result missing spill path: %q", text)
	}
}
