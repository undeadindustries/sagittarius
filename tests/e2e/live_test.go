package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// liveEnv builds the subprocess environment for a live provider: the current
// environment (real credentials + ~/.sagittarius home) plus the provider
// selection and an optional pinned session id (for cross-process /diff /undo).
func liveEnv(p liveProvider, sessionID string) []string {
	env := append(os.Environ(), "GEMINI_PROVIDER="+p.id)
	if sessionID != "" {
		env = append(env, "SAGITTARIUS_SESSION_ID="+sessionID)
	}
	return env
}

// liveProvidersOrSkip discovers usable providers, skipping unless the live suite
// is explicitly opted in. Live scenarios make real (billable) API calls, so they
// never run under a plain `go test ./...`; `make e2e` / scripts/smoke-e2e.sh set
// SAGITTARIUS_E2E_LIVE=1. A discovered-but-keyless environment also skips.
func liveProvidersOrSkip(t *testing.T) []liveProvider {
	t.Helper()
	if mockMode() {
		t.Skip("mock mode: live scenarios skipped (run without SAGITTARIUS_E2E_MOCK)")
	}
	if os.Getenv("SAGITTARIUS_E2E_LIVE") != "1" {
		t.Skip("live E2E disabled; run `make e2e` or set SAGITTARIUS_E2E_LIVE=1 (makes real API calls)")
	}
	providers := discoverLiveProviders(context.Background())
	if len(providers) == 0 {
		t.Skip("no live providers configured; set a provider API key or SAGITTARIUS_E2E_MOCK=1")
	}
	return providers
}

func TestE2E_LiveHeadlessRead(t *testing.T) {
	bin := sagittariusBin(t)
	for _, p := range liveProvidersOrSkip(t) {
		p := p
		t.Run(p.id, func(t *testing.T) {
			work := t.TempDir()
			if err := os.WriteFile(filepath.Join(work, "readme.txt"), []byte("hello"), 0o644); err != nil {
				t.Fatalf("seed file: %v", err)
			}
			res := invoke(t, bin, work, liveEnv(p, ""),
				"-m", p.model, "--output-format", "stream-json",
				"-p", "List the files in the current directory.")
			if res.exitCode != 0 {
				t.Fatalf("exit=%d stderr=%s", res.exitCode, res.stderr)
			}
			if !strings.Contains(res.stdout, `"type":"text"`) && !strings.Contains(res.stdout, `"type":"tool_result"`) {
				t.Fatalf("stream missing text/tool_result:\n%s", res.stdout)
			}
		})
	}
}

func TestE2E_LiveHeadlessWriteYolo(t *testing.T) {
	bin := sagittariusBin(t)
	for _, p := range liveProvidersOrSkip(t) {
		p := p
		t.Run(p.id, func(t *testing.T) {
			work := t.TempDir()
			res := invoke(t, bin, work, liveEnv(p, ""),
				"--yolo", "-m", p.model, "--output-format", "stream-json",
				"-p", "Create a file named e2e.txt containing exactly the text ok using the write_file tool.")
			if res.exitCode != 0 {
				t.Fatalf("exit=%d stderr=%s", res.exitCode, res.stderr)
			}
			if _, err := os.Stat(filepath.Join(work, "e2e.txt")); err != nil {
				// The model declined to call write_file; not a harness failure.
				t.Skipf("model did not create e2e.txt (stream:\n%s)", res.stdout)
			}
			if !strings.Contains(res.stdout, `"type":"tool_start"`) {
				t.Fatalf("stream missing tool_start:\n%s", res.stdout)
			}
		})
	}
}

func TestE2E_LiveAskBlocksWrite(t *testing.T) {
	bin := sagittariusBin(t)
	for _, p := range liveProvidersOrSkip(t) {
		p := p
		t.Run(p.id, func(t *testing.T) {
			work := t.TempDir()
			res := invoke(t, bin, work, liveEnv(p, ""),
				"--mode", "ask", "--yolo", "-m", p.model, "--output-format", "stream-json",
				"-p", "Create a file named blocked.txt with content x using write_file.")
			if res.exitCode != 0 {
				t.Fatalf("exit=%d stderr=%s", res.exitCode, res.stderr)
			}
			// Ask mode forbids writes regardless of model behaviour.
			if _, err := os.Stat(filepath.Join(work, "blocked.txt")); err == nil {
				t.Fatalf("ask mode allowed a write (blocked.txt created):\n%s", res.stdout)
			}
		})
	}
}

func TestE2E_LiveSlashModeShow(t *testing.T) {
	bin := sagittariusBin(t)
	providers := liveProvidersOrSkip(t)
	p := providers[0]
	work := t.TempDir()
	res := invoke(t, bin, work, liveEnv(p, ""), "--slash", "/mode show")
	if res.exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", res.exitCode, res.stderr)
	}
	if !strings.Contains(strings.ToLower(res.stdout), "mode") {
		t.Fatalf("/mode show output unexpected:\n%s", res.stdout)
	}
}

func TestE2E_LiveSlashDiffUndo(t *testing.T) {
	bin := sagittariusBin(t)
	providers := liveProvidersOrSkip(t)
	p := providers[0]
	work := t.TempDir()
	session := fmt.Sprintf("e2e-diff-%d", os.Getpid())
	env := liveEnv(p, session)

	// Write via the model.
	write := invoke(t, bin, work, env,
		"--yolo", "-m", p.model, "--output-format", "stream-json",
		"-p", "Create a file named note.txt containing exactly the text hello using the write_file tool.")
	if write.exitCode != 0 {
		t.Fatalf("write exit=%d stderr=%s", write.exitCode, write.stderr)
	}
	if _, err := os.Stat(filepath.Join(work, "note.txt")); err != nil {
		t.Skipf("model did not create note.txt; cannot test diff/undo (stream:\n%s)", write.stdout)
	}

	// /diff (separate process, same session) should report the change.
	diff := invoke(t, bin, work, env, "--slash", "/diff")
	if diff.exitCode != 0 {
		t.Fatalf("diff exit=%d stderr=%s", diff.exitCode, diff.stderr)
	}
	if !strings.Contains(diff.stdout, "note.txt") {
		t.Fatalf("/diff missing note.txt:\n%s", diff.stdout)
	}

	// /undo should restore (remove) the file.
	undo := invoke(t, bin, work, env, "--slash", "/undo")
	if undo.exitCode != 0 {
		t.Fatalf("undo exit=%d stderr=%s", undo.exitCode, undo.stderr)
	}
	if _, err := os.Stat(filepath.Join(work, "note.txt")); !os.IsNotExist(err) {
		t.Fatalf("note.txt still present after /undo (stat err=%v)", err)
	}
}
