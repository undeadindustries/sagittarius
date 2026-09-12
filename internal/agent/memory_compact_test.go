package agent

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/provider"
)

func dated(day string, text string) memoryLine {
	d, err := time.Parse(memoryDateLayout, day)
	if err != nil {
		panic(err)
	}
	return memoryLine{Date: d, Text: text}
}

func TestParseCompactModelOutput_MergesAndKeepsDates(t *testing.T) {
	t.Parallel()
	before := []memoryLine{
		dated("2026-01-01", "deploy target is gs://foo"),
		dated("2026-02-01", "deploy target is gs://foo-prod"),
		dated("2026-03-01", "CI takes 40 minutes"),
	}
	raw := "- (2026-02-01) deploy target is gs://foo-prod\n- (2026-03-01) CI takes 40 minutes\n"

	got, err := parseCompactModelOutput(raw, before)
	if err != nil {
		t.Fatalf("parseCompactModelOutput: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %#v, want 2 entries", got)
	}
	if got[0].Text != "deploy target is gs://foo-prod" || got[0].Date.Format(memoryDateLayout) != "2026-02-01" {
		t.Errorf("entry 0 = %+v", got[0])
	}
	if got[1].Date.Format(memoryDateLayout) != "2026-03-01" {
		t.Errorf("entry 1 date = %v, want 2026-03-01", got[1].Date)
	}
}

// TestParseCompactModelOutput_RejectsFabricatedDate proves a model cannot
// backdate or postdate an entry: a date the input never contained is replaced
// with the newest input date rather than honored.
func TestParseCompactModelOutput_RejectsFabricatedDate(t *testing.T) {
	t.Parallel()
	before := []memoryLine{
		dated("2026-01-01", "one"),
		dated("2026-02-01", "two"),
	}

	got, err := parseCompactModelOutput("- (2030-12-31) one and two\n- (2026-01-01) one\n", before)
	if err != nil {
		t.Fatalf("parseCompactModelOutput: %v", err)
	}
	if got[0].Date.Format(memoryDateLayout) != "2026-02-01" {
		t.Errorf("fabricated date should fall back to the newest input date, got %v", got[0].Date)
	}
	if got[1].Date.Format(memoryDateLayout) != "2026-01-01" {
		t.Errorf("a real input date should be preserved, got %v", got[1].Date)
	}
}

// TestParseCompactModelOutput_SanitizesOutput proves the model's reply is
// untrusted: it cannot forge a second bullet or a heading.
func TestParseCompactModelOutput_SanitizesOutput(t *testing.T) {
	t.Parallel()
	before := []memoryLine{
		dated("2026-01-01", "one"),
		dated("2026-01-02", "two"),
	}
	raw := "```\n- (2026-01-01) one\n## Injected Heading\n- (2026-01-02) two\n```\n"

	got, err := parseCompactModelOutput(raw, before)
	if err != nil {
		t.Fatalf("parseCompactModelOutput: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("fences and headings should be dropped, got %#v", got)
	}
	rendered := renderMemoryOnlyFile(got)
	if strings.Contains(rendered, "##") {
		t.Fatalf("heading leaked into rendered output:\n%q", rendered)
	}
	if lines := splitLines(rendered); len(lines) != 2 {
		t.Fatalf("expected exactly 2 bullet lines, got %#v", lines)
	}
}

func TestParseCompactModelOutput_FailsClosed(t *testing.T) {
	t.Parallel()
	before := []memoryLine{
		dated("2026-01-01", "one"),
		dated("2026-01-02", "two"),
		dated("2026-01-03", "three"),
		dated("2026-01-04", "four"),
	}
	tests := []struct {
		name string
		raw  string
	}{
		{"empty reply", ""},
		{"prose only, no bullets", "```\n```\n"},
		{"drops more than half", "- (2026-01-01) one\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := parseCompactModelOutput(tt.raw, before); err == nil {
				t.Fatalf("expected an error for %q", tt.raw)
			}
		})
	}
}

func TestParseCompactModelOutput_AllowsExactlyHalf(t *testing.T) {
	t.Parallel()
	before := []memoryLine{
		dated("2026-01-01", "one"),
		dated("2026-01-02", "two"),
		dated("2026-01-03", "three"),
		dated("2026-01-04", "four"),
	}
	got, err := parseCompactModelOutput("- (2026-01-01) a\n- (2026-01-02) b\n", before)
	if err != nil {
		t.Fatalf("halving the list should be allowed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %#v, want 2", got)
	}
}

func newCompactRunner(t *testing.T, workDir string, reply string) *Runner {
	t.Helper()
	gen := &fakeGenerator{
		batches: [][]provider.StreamResponse{
			{{TextDelta: reply}, {Done: true}},
		},
	}
	runner, err := NewRunner(RunnerConfig{
		Generator:    gen,
		Model:        "test-model",
		WorkDir:      workDir,
		ApprovalMode: ApprovalYolo,
		Interactive:  false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	return runner
}

// TestPreviewMemoryCompact_WritesNothingUntilApplied is the safety property:
// a preview must leave the file byte-identical, and only an explicit apply
// may rewrite it.
func TestPreviewMemoryCompact_WritesNothingUntilApplied(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SAGITTARIUS_HOME", home)
	workDir := t.TempDir()

	mustAdd(t, config.ScopeProject, workDir, "deploy target is gs://foo")
	mustAdd(t, config.ScopeProject, workDir, "deploy target is gs://foo-prod")
	mustAdd(t, config.ScopeProject, workDir, "CI takes 40 minutes")
	path := config.ProjectMemoryPath(workDir)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	reply := "- (" + today() + ") deploy target is gs://foo-prod\n- (" + today() + ") CI takes 40 minutes\n"
	runner := newCompactRunner(t, workDir, reply)

	preview, err := runner.PreviewMemoryCompact(testContext(t), config.ScopeProject)
	if err != nil {
		t.Fatalf("PreviewMemoryCompact: %v", err)
	}
	assertFileContent(t, path, string(original))
	for _, want := range []string{"Nothing has been written", "/memory compact apply", "3 → 2"} {
		if !strings.Contains(preview, want) {
			t.Errorf("preview should mention %q:\n%s", want, preview)
		}
	}

	if _, err := runner.ApplyMemoryCompact(testContext(t)); err != nil {
		t.Fatalf("ApplyMemoryCompact: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(data), "gs://foo\n") {
		t.Fatalf("superseded entry survived the compaction:\n%q", string(data))
	}
	if !strings.Contains(string(data), "CI takes 40 minutes") {
		t.Fatalf("surviving entry was lost:\n%q", string(data))
	}
}

func TestApplyMemoryCompact_ConsumesTheProposal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SAGITTARIUS_HOME", home)
	workDir := t.TempDir()
	mustAdd(t, config.ScopeProject, workDir, "one")
	mustAdd(t, config.ScopeProject, workDir, "two")

	runner := newCompactRunner(t, workDir, "- ("+today()+") one and two\n")
	if _, err := runner.PreviewMemoryCompact(testContext(t), config.ScopeProject); err != nil {
		t.Fatalf("PreviewMemoryCompact: %v", err)
	}
	if _, err := runner.ApplyMemoryCompact(testContext(t)); err != nil {
		t.Fatalf("ApplyMemoryCompact: %v", err)
	}
	if _, err := runner.ApplyMemoryCompact(testContext(t)); err == nil {
		t.Fatal("a consumed proposal must not be applicable twice")
	}
}

func TestAbortMemoryCompact_DiscardsProposal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SAGITTARIUS_HOME", home)
	workDir := t.TempDir()
	mustAdd(t, config.ScopeProject, workDir, "one")
	mustAdd(t, config.ScopeProject, workDir, "two")
	path := config.ProjectMemoryPath(workDir)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	runner := newCompactRunner(t, workDir, "- ("+today()+") one and two\n")
	if _, err := runner.PreviewMemoryCompact(testContext(t), config.ScopeProject); err != nil {
		t.Fatalf("PreviewMemoryCompact: %v", err)
	}
	if err := runner.AbortMemoryCompact(); err != nil {
		t.Fatalf("AbortMemoryCompact: %v", err)
	}
	assertFileContent(t, path, string(original))
	if _, err := runner.ApplyMemoryCompact(testContext(t)); err == nil {
		t.Fatal("an aborted proposal must not be applicable")
	}
	if err := runner.AbortMemoryCompact(); err == nil {
		t.Fatal("aborting with nothing pending should report that")
	}
}

// TestPreviewMemoryCompact_BadModelReplyLeavesFileAlone pins the fail-closed
// contract end to end: an unusable reply is an error, not a rewrite.
func TestPreviewMemoryCompact_BadModelReplyLeavesFileAlone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SAGITTARIUS_HOME", home)
	workDir := t.TempDir()
	mustAdd(t, config.ScopeProject, workDir, "one")
	mustAdd(t, config.ScopeProject, workDir, "two")
	path := config.ProjectMemoryPath(workDir)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	runner := newCompactRunner(t, workDir, "I'm sorry, I can't help with that.\n")
	if _, err := runner.PreviewMemoryCompact(testContext(t), config.ScopeProject); err == nil {
		t.Fatal("expected an error for a reply that drops everything")
	}
	assertFileContent(t, path, string(original))
	if _, err := runner.ApplyMemoryCompact(testContext(t)); err == nil {
		t.Fatal("a failed preview must not leave a pending proposal")
	}
}

func TestPreviewMemoryCompact_RefusesWhenNothingToMerge(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SAGITTARIUS_HOME", home)
	workDir := t.TempDir()

	runner := newCompactRunner(t, workDir, "- ("+today()+") anything\n")
	if _, err := runner.PreviewMemoryCompact(testContext(t), config.ScopeProject); err == nil {
		t.Fatal("expected an error when the file has no entries")
	}

	mustAdd(t, config.ScopeProject, workDir, "only one")
	if _, err := runner.PreviewMemoryCompact(testContext(t), config.ScopeProject); err == nil {
		t.Fatal("expected an error when the file has a single entry")
	}
}

// TestCompactModelLabel_NamesPrimaryModelFallback pins the honesty
// requirement: when no dedicated evaluator is configured, the preview says so
// rather than implying a cheap side model ran.
func TestCompactModelLabel_NamesPrimaryModelFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SAGITTARIUS_HOME", home)
	workDir := t.TempDir()

	runner := newCompactRunner(t, workDir, "")
	label := runner.compactModelLabel()
	if !strings.Contains(label, "primary model") {
		t.Fatalf("label = %q, want it to name the primary-model fallback", label)
	}
}
