package lsp

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/undeadindustries/sagittarius/internal/diagnostics"
)

func countLines(t *testing.T, path string) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("ReadFile: %v", err)
	}
	if len(b) == 0 {
		return 0
	}
	return bytes.Count(b, []byte("\n"))
}

// TestPoolGetOrCreateSingleStartPerKey asserts that concurrent GetOrCreate
// calls for the same (command, root) key start the server exactly once: the
// start happens off the pool mutex, but later callers must wait on the
// in-flight start's ready channel rather than racing to spawn their own.
func TestPoolGetOrCreateSingleStartPerKey(t *testing.T) {
	t.Setenv("LSP_FAKE_SERVER", "1")
	logPath := filepath.Join(t.TempDir(), "starts.log")
	t.Setenv("LSP_FAKE_SERVER_LOG", logPath)

	pool := NewPool()
	t.Cleanup(pool.Close)
	root := t.TempDir()
	spec := &diagnostics.ServerSpec{Command: os.Args[0]}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	const n = 10
	clients := make([]*Client, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			clients[i], errs[i] = pool.GetOrCreate(ctx, root, spec)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("GetOrCreate[%d]: %v", i, err)
		}
	}
	for i := 1; i < n; i++ {
		if clients[i] != clients[0] {
			t.Fatalf("GetOrCreate returned distinct clients for the same key: [0]=%p [%d]=%p", clients[0], i, clients[i])
		}
	}

	if got := countLines(t, logPath); got != 1 {
		t.Fatalf("server start count = %d, want exactly 1", got)
	}
}

// TestPoolGetOrCreatePerRootKeys asserts that GetOrCreate keys clients by
// (command, root): the per-language module root threaded through Diagnoser
// must actually select (and start) a distinct server per root rather than
// reusing one server across nested modules.
func TestPoolGetOrCreatePerRootKeys(t *testing.T) {
	t.Setenv("LSP_FAKE_SERVER", "1")
	logPath := filepath.Join(t.TempDir(), "starts.log")
	t.Setenv("LSP_FAKE_SERVER_LOG", logPath)

	pool := NewPool()
	t.Cleanup(pool.Close)
	rootA := t.TempDir()
	rootB := t.TempDir()
	spec := &diagnostics.ServerSpec{Command: os.Args[0]}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	clientA, err := pool.GetOrCreate(ctx, rootA, spec)
	if err != nil {
		t.Fatalf("GetOrCreate(rootA): %v", err)
	}
	clientB, err := pool.GetOrCreate(ctx, rootB, spec)
	if err != nil {
		t.Fatalf("GetOrCreate(rootB): %v", err)
	}
	if clientA == clientB {
		t.Fatal("GetOrCreate returned the same client for two different roots")
	}

	if got := countLines(t, logPath); got != 2 {
		t.Fatalf("server start count = %d, want exactly 2 (one per root)", got)
	}
}
