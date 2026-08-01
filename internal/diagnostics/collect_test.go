package diagnostics

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// writeFakeTool creates an executable shell script named name inside dir
// that echoes its received argv (space-joined) to stdout and exits with
// exitCode. It returns the script's absolute path.
func writeFakeTool(t *testing.T, dir, name string, exitCode int) string {
	t.Helper()
	path := filepath.Join(dir, name)
	script := "#!/bin/sh\necho \"$@\"\nexit " + itoa(exitCode) + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// withFakeBin prepends a fresh directory containing fake tool scripts to
// PATH for the duration of the test and clears any cached lookups for the
// given commands so the fakes are actually resolved.
func withFakeBin(t *testing.T) string {
	t.Helper()
	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return bin
}

func clearLookPathCache(names ...string) {
	for _, n := range names {
		lookPathCache.Delete(n)
	}
}

func TestToolResolverResolve(t *testing.T) {
	bin := withFakeBin(t)
	writeFakeTool(t, bin, "fake-main-ok", 0)
	writeFakeTool(t, bin, "fake-fallback-ok", 0)
	clearLookPathCache("fake-main-ok", "fake-fallback-ok", "fake-missing-xyz", "fake-missing-fallback-xyz")

	tr := newToolResolver(Options{})

	t.Run("precondition false skips silently", func(t *testing.T) {
		tool := Tool{Command: "fake-main-ok", Precondition: func(string) bool { return false }}
		got, err := tr.resolve("/tmp", tool)
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if got != nil {
			t.Fatalf("resolve = %v, want nil (precondition should skip)", got)
		}
	})

	t.Run("resolves the main command when present", func(t *testing.T) {
		tool := Tool{Name: "main", Command: "fake-main-ok"}
		got, err := tr.resolve("/tmp", tool)
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if got == nil || got.Command != "fake-main-ok" {
			t.Fatalf("resolve = %v, want the main tool", got)
		}
	})

	t.Run("falls back when the main command is missing", func(t *testing.T) {
		tool := Tool{
			Name:    "main",
			Command: "fake-missing-xyz",
			Fallback: &Tool{
				Name:    "fallback",
				Command: "fake-fallback-ok",
			},
		}
		got, err := tr.resolve("/tmp", tool)
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if got == nil || got.Command != "fake-fallback-ok" {
			t.Fatalf("resolve = %v, want the fallback tool", got)
		}
	})

	t.Run("errors when neither the command nor its fallback exist", func(t *testing.T) {
		tool := Tool{
			Name:    "main",
			Command: "fake-missing-xyz",
			Fallback: &Tool{
				Name:    "fallback",
				Command: "fake-missing-fallback-xyz",
			},
		}
		got, err := tr.resolve("/tmp", tool)
		if err == nil {
			t.Fatalf("resolve = %v, %v; want a lookup error", got, err)
		}
		if got != nil {
			t.Fatalf("resolve tool = %v, want nil on error", got)
		}
	})
}

func TestToolResolverRepoLocalPolicy(t *testing.T) {
	wsRoot := t.TempDir()
	binDir := filepath.Join(wsRoot, "node_modules", ".bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	newTool := func(cmd string) Tool { return Tool{Name: cmd, Command: cmd} }

	t.Run("allow runs without prompting", func(t *testing.T) {
		writeFakeTool(t, binDir, "repolocal-allow", 0)
		clearLookPathCache("repolocal-allow")
		tr := newToolResolver(Options{Root: wsRoot, RepoLocalPolicy: RepoLocalAllow})
		got, err := tr.resolve(wsRoot, newTool("repolocal-allow"))
		if err != nil || got == nil {
			t.Fatalf("resolve = %v, %v; want the tool allowed", got, err)
		}
	})

	t.Run("deny skips silently without a missing-tool error", func(t *testing.T) {
		writeFakeTool(t, binDir, "repolocal-deny", 0)
		clearLookPathCache("repolocal-deny")
		tr := newToolResolver(Options{Root: wsRoot, RepoLocalPolicy: RepoLocalDeny})
		got, err := tr.resolve(wsRoot, newTool("repolocal-deny"))
		if err != nil {
			t.Fatalf("resolve returned an error, want a silent skip: %v", err)
		}
		if got != nil {
			t.Fatalf("resolve = %v, want nil (denied)", got)
		}
	})

	t.Run("prompt defers to ApproveRepoLocal and memoizes the answer", func(t *testing.T) {
		writeFakeTool(t, binDir, "repolocal-prompt", 0)
		clearLookPathCache("repolocal-prompt")
		calls := 0
		tr := newToolResolver(Options{
			Root:            wsRoot,
			RepoLocalPolicy: RepoLocalPrompt,
			ApproveRepoLocal: func(tool, path string) bool {
				calls++
				return true
			},
		})
		for i := 0; i < 3; i++ {
			got, err := tr.resolve(wsRoot, newTool("repolocal-prompt"))
			if err != nil || got == nil {
				t.Fatalf("resolve[%d] = %v, %v; want approved", i, got, err)
			}
		}
		if calls != 1 {
			t.Fatalf("ApproveRepoLocal called %d times, want exactly 1 (memoized per command)", calls)
		}
	})

	t.Run("prompt denies without blocking when no approver is configured", func(t *testing.T) {
		writeFakeTool(t, binDir, "repolocal-headless", 0)
		clearLookPathCache("repolocal-headless")
		tr := newToolResolver(Options{Root: wsRoot, RepoLocalPolicy: RepoLocalPrompt, ApproveRepoLocal: nil})
		got, err := tr.resolve(wsRoot, newTool("repolocal-headless"))
		if err != nil {
			t.Fatalf("resolve returned an error, want a silent deny: %v", err)
		}
		if got != nil {
			t.Fatalf("resolve = %v, want nil (headless denies without a prompt)", got)
		}
	})

	t.Run("a system-wide tool is never classified as repo-local", func(t *testing.T) {
		sysDir := t.TempDir() // outside wsRoot
		t.Setenv("PATH", sysDir+string(os.PathListSeparator)+os.Getenv("PATH"))
		writeFakeTool(t, sysDir, "system-tool", 0)
		clearLookPathCache("system-tool")
		tr := newToolResolver(Options{Root: wsRoot, RepoLocalPolicy: RepoLocalDeny})
		got, err := tr.resolve(wsRoot, newTool("system-tool"))
		if err != nil || got == nil {
			t.Fatalf("resolve = %v, %v; a system-wide tool must run even under RepoLocalDeny", got, err)
		}
	})
}

func TestFindRootNearestMarker(t *testing.T) {
	ws := t.TempDir()
	outer := filepath.Join(ws, "outer")
	inner := filepath.Join(outer, "inner")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outer, "go.mod"), []byte("module outer\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(inner, "go.mod"), []byte("module inner\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Run("picks the nearest marker, not the workspace root", func(t *testing.T) {
		got := findRoot(inner, ws, []string{"go.mod"})
		if got != inner {
			t.Fatalf("findRoot = %q, want the nested module root %q", got, inner)
		}
	})

	t.Run("walks up past a directory with no marker to find one above", func(t *testing.T) {
		leaf := filepath.Join(outer, "nomarker")
		if err := os.MkdirAll(leaf, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		got := findRoot(leaf, ws, []string{"go.mod"})
		if got != outer {
			t.Fatalf("findRoot = %q, want %q", got, outer)
		}
	})

	t.Run("stops at the workspace boundary and reports no root", func(t *testing.T) {
		noModuleWS := t.TempDir()
		sub := filepath.Join(noModuleWS, "a", "b")
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		got := findRoot(sub, noModuleWS, []string{"go.mod"})
		if got != "" {
			t.Fatalf("findRoot = %q, want \"\" (no marker within the workspace)", got)
		}
	})
}

// TestCollectPreservesFileCheckFlags is a regression test for the bug where
// diagnostics.Collect used checks.Argv (built for run_project_checks, whose
// Args end in a replaceable target like "gofmt -l .") to build file-check
// argv. The registry's Tool.Args are pure flags with no trailing target, so
// Argv silently dropped the last flag, and a tool with Args: []string{}
// (like eslint) got an empty argv instead of the file list. Collect must
// append the target paths directly instead.
func TestCollectPreservesFileCheckFlags(t *testing.T) {
	bin := withFakeBin(t)
	capture := writeFakeTool(t, bin, "fake-flagged-linter", 1)
	clearLookPathCache("fake-flagged-linter")
	_ = capture

	wsRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(wsRoot, "a.src"), []byte("a"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wsRoot, "b.src"), []byte("b"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	orig := registry
	registry = []Language{
		{
			ID:         "fakelang",
			Extensions: []string{".src"},
			FileChecks: []Tool{
				{Name: "flagged", Command: "fake-flagged-linter", Args: []string{"--flag1", "--flag2"}, Severity: SeverityWarning},
			},
		},
	}
	t.Cleanup(func() { registry = orig })

	report, err := Collect(context.Background(), Options{
		Root:    wsRoot,
		Paths:   []string{"a.src", "b.src"},
		Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(report.Findings) != 1 {
		t.Fatalf("Findings = %v, want exactly 1", report.Findings)
	}
	got := report.Findings[0].Message
	want := "--flag1 --flag2 a.src b.src"
	if got != want {
		t.Fatalf("received argv (via echoed message) = %q, want %q (flags must survive, both files must be appended)", got, want)
	}
}

// TestCollectHandlesEmptyArgsTool is a regression test for tools registered
// with Args: []string{} (eslint's real registry entry): appending paths to a
// clone of an empty slice must still produce the file list, not an empty
// argv.
func TestCollectHandlesEmptyArgsTool(t *testing.T) {
	bin := withFakeBin(t)
	writeFakeTool(t, bin, "fake-noargs-linter", 1)
	clearLookPathCache("fake-noargs-linter")

	wsRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(wsRoot, "c.src2"), []byte("c"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	orig := registry
	registry = []Language{
		{
			ID:         "fakelang2",
			Extensions: []string{".src2"},
			FileChecks: []Tool{
				{Name: "noargs", Command: "fake-noargs-linter", Args: []string{}, Severity: SeverityWarning},
			},
		},
	}
	t.Cleanup(func() { registry = orig })

	report, err := Collect(context.Background(), Options{
		Root:    wsRoot,
		Paths:   []string{"c.src2"},
		Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(report.Findings) != 1 {
		t.Fatalf("Findings = %v, want exactly 1", report.Findings)
	}
	if got, want := report.Findings[0].Message, "c.src2"; got != want {
		t.Fatalf("received argv = %q, want %q (the file list must not be dropped)", got, want)
	}
}

func TestCollectReportsMissingTools(t *testing.T) {
	// Deliberately do not put anything on PATH under this name.
	clearLookPathCache("definitely-not-a-real-binary-xyz")

	wsRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(wsRoot, "d.src3"), []byte("d"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	orig := registry
	registry = []Language{
		{
			ID:         "fakelang3",
			Extensions: []string{".src3"},
			FileChecks: []Tool{
				{Name: "missing", Command: "definitely-not-a-real-binary-xyz", Args: []string{}, Severity: SeverityWarning, InstallHint: "install it"},
			},
		},
	}
	t.Cleanup(func() { registry = orig })

	report, err := Collect(context.Background(), Options{
		Root:    wsRoot,
		Paths:   []string{"d.src3"},
		Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(report.Findings) != 0 {
		t.Fatalf("Findings = %v, want none", report.Findings)
	}
	if len(report.MissingTools) != 1 || report.MissingTools[0].Name != "definitely-not-a-real-binary-xyz" {
		t.Fatalf("MissingTools = %v, want one entry for the missing binary", report.MissingTools)
	}
}

// TestCollectEmptyPathsIsANoOp guards the early-return fast path.
func TestCollectEmptyPathsIsANoOp(t *testing.T) {
	report, err := Collect(context.Background(), Options{Root: t.TempDir(), Timeout: time.Second})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(report.Findings) != 0 || len(report.MissingTools) != 0 {
		t.Fatalf("Collect with no paths = %+v, want an empty report", report)
	}
}

// Sanity: this test package must not leak goroutines/processes across the
// fake-tool subtests (each is a synchronous exec.Command, not backgrounded).
func TestMain_NoGoroutineLeakSanity(t *testing.T) {
	before := runtime.NumGoroutine()
	_, _ = Collect(context.Background(), Options{})
	after := runtime.NumGoroutine()
	if after > before+2 {
		t.Fatalf("goroutine count grew from %d to %d after a no-op Collect", before, after)
	}
}
