package lsp

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// startFakeClient launches the current test binary as a fake LSP server
// rooted at root. It registers a cleanup that closes the client.
func startFakeClient(t *testing.T, root string) *Client {
	t.Helper()
	t.Setenv("LSP_FAKE_SERVER", "1")

	// Intentionally not derived from a deferred-cancel context: Start wraps
	// this in its own context.WithCancel and keeps that cancel func for
	// Close, so canceling the parent here would kill the subprocess the
	// moment this helper returns instead of when the test finishes.
	client, err := Start(context.Background(), root, os.Args[0])
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestClientDiagnosticsBasic(t *testing.T) {
	root := t.TempDir()
	path := writeFile(t, root, "a.txt", "TRIGGER_DIAG")
	client := startFakeClient(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	findings, err := client.Diagnostics(ctx, []string{path})
	if err != nil {
		t.Fatalf("Diagnostics: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %v, want 1 finding", findings)
	}
	if !strings.Contains(findings[0].Message, "boom") {
		t.Fatalf("finding message = %q, want it to contain %q", findings[0].Message, "boom")
	}
}

// TestClientDiagnosticsStaleInvalidation asserts that a file whose issue was
// fixed no longer returns the previous call's finding: Diagnostics must
// clear stale entries before waiting for the server to republish.
func TestClientDiagnosticsStaleInvalidation(t *testing.T) {
	root := t.TempDir()
	path := writeFile(t, root, "a.txt", "TRIGGER_DIAG")
	client := startFakeClient(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	findings, err := client.Diagnostics(ctx, []string{path})
	if err != nil {
		t.Fatalf("Diagnostics (first): %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("first call findings = %v, want 1 finding", findings)
	}

	writeFile(t, root, "a.txt", "OK now")

	findings, err = client.Diagnostics(ctx, []string{path})
	if err != nil {
		t.Fatalf("Diagnostics (second): %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("second call findings = %v, want none (stale finding must be cleared)", findings)
	}
}

// TestClientDiagnosticsTimeoutNoLeak asserts that when the server never
// publishes for a URI, Diagnostics returns promptly (bounded by
// diagnosticsWaitTimeout) with no error, and leaves no goroutine behind.
func TestClientDiagnosticsTimeoutNoLeak(t *testing.T) {
	root := t.TempDir()
	path := writeFile(t, root, "b.txt", "NEVER_RESPOND")
	client := startFakeClient(t, root)

	before := runtime.NumGoroutine()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	start := time.Now()
	findings, err := client.Diagnostics(ctx, []string{path})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Diagnostics: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %v, want none", findings)
	}
	if elapsed < diagnosticsWaitTimeout {
		t.Fatalf("Diagnostics returned after %v, want at least %v (should wait out the timeout)", elapsed, diagnosticsWaitTimeout)
	}
	if elapsed > diagnosticsWaitTimeout+3*time.Second {
		t.Fatalf("Diagnostics returned after %v, want close to %v (should not hang)", elapsed, diagnosticsWaitTimeout)
	}

	// Give the watchdog goroutine a moment to actually exit, then assert we
	// are back near the starting goroutine count (no leak).
	deadline := time.Now().Add(2 * time.Second)
	for {
		if runtime.NumGoroutine() <= before+1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("goroutine count = %d after timeout wait, want <= %d (possible leak)", runtime.NumGoroutine(), before+1)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestClientDiagnosticsConcurrent exercises Diagnostics from multiple
// goroutines at once, primarily to be run under -race.
func TestClientDiagnosticsConcurrent(t *testing.T) {
	root := t.TempDir()
	pathA := writeFile(t, root, "a.txt", "TRIGGER_DIAG")
	pathB := writeFile(t, root, "b.txt", "clean")
	client := startFakeClient(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			paths := []string{pathA, pathB}
			if i%2 == 0 {
				paths = []string{pathB, pathA}
			}
			if _, err := client.Diagnostics(ctx, paths); err != nil {
				t.Errorf("Diagnostics: %v", err)
			}
		}(i)
	}
	wg.Wait()
}
