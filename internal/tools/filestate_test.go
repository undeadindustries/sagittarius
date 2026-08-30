package tools

import (
	"sync"
	"testing"
	"time"
)

func TestFileStateNilReceiverIsSafe(t *testing.T) {
	t.Parallel()
	var r *FileStateRegistry
	r.RecordRead("a", "/x")
	r.RecordWrite("a", "/x")
	if who, stale := r.CheckStale("a", "/x"); stale || who != "" {
		t.Fatalf("nil registry reported staleness: %q %v", who, stale)
	}
	if got := r.WritesSince("a", time.Time{}); got != nil {
		t.Fatalf("WritesSince on nil = %v, want nil", got)
	}
	r.LockPath("/x")()
	r.LockPaths([]string{"/x", "/y"})()
}

func TestCheckStaleNamesTheSibling(t *testing.T) {
	t.Parallel()
	r := NewFileStateRegistry()

	r.RecordRead("agent_A", "/repo/x.go")
	if _, stale := r.CheckStale("agent_A", "/repo/x.go"); stale {
		t.Fatal("no writer yet, must not be stale")
	}

	r.RecordWrite("agent_B", "/repo/x.go")
	who, stale := r.CheckStale("agent_A", "/repo/x.go")
	if !stale || who != "agent_B" {
		t.Fatalf("CheckStale = %q %v, want agent_B true", who, stale)
	}

	// The writer is never stale against its own write.
	if _, stale := r.CheckStale("agent_B", "/repo/x.go"); stale {
		t.Fatal("writer reported stale against itself")
	}

	// A path this agent never read has no prior view to invalidate.
	r.RecordWrite("agent_B", "/repo/other.go")
	if _, stale := r.CheckStale("agent_A", "/repo/other.go"); stale {
		t.Fatal("unread path reported stale")
	}

	// Re-reading clears staleness.
	r.RecordRead("agent_A", "/repo/x.go")
	if _, stale := r.CheckStale("agent_A", "/repo/x.go"); stale {
		t.Fatal("re-read did not clear staleness")
	}
}

func TestWritesSinceFiltersToOwnReads(t *testing.T) {
	t.Parallel()
	r := NewFileStateRegistry()

	r.RecordRead("parent", "/repo/a.go")
	r.RecordRead("parent", "/repo/b.go")
	start := time.Now()

	r.RecordWrite("child", "/repo/a.go")
	r.RecordWrite("child", "/repo/unread.go")

	got := r.WritesSince("parent", start)
	if len(got) != 1 || got[0] != "/repo/a.go" {
		t.Fatalf("WritesSince = %v, want [/repo/a.go]", got)
	}

	// A write before the cutoff is not reported.
	if got := r.WritesSince("parent", time.Now()); len(got) != 0 {
		t.Fatalf("WritesSince after the write = %v, want empty", got)
	}

	// The writer does not report its own writes back to itself.
	r.RecordRead("child", "/repo/a.go")
	r.RecordWrite("child", "/repo/a.go")
	if got := r.WritesSince("child", start); len(got) != 0 {
		t.Fatalf("self writes reported: %v", got)
	}
}

func TestLockPathSerializesSamePathOnly(t *testing.T) {
	t.Parallel()
	r := NewFileStateRegistry()

	// Same path: the second acquisition must wait.
	release := r.LockPath("/repo/x.go")
	acquired := make(chan struct{})
	go func() {
		defer close(acquired)
		r.LockPath("/repo/x.go")()
	}()
	select {
	case <-acquired:
		t.Fatal("same-path lock was not exclusive")
	case <-time.After(20 * time.Millisecond):
	}
	release()
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("same-path lock never released")
	}

	// Different paths never contend.
	a := r.LockPath("/repo/a.go")
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.LockPath("/repo/b.go")()
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("different paths contended")
	}
	a()
}

// TestLockPathsOrderingAvoidsDeadlock drives two goroutines holding
// overlapping path sets in opposite argument order. Sorted acquisition is what
// keeps this from hanging.
func TestLockPathsOrderingAvoidsDeadlock(t *testing.T) {
	t.Parallel()
	r := NewFileStateRegistry()

	done := make(chan struct{})
	go func() {
		defer close(done)
		var wg sync.WaitGroup
		for i := 0; i < 200; i++ {
			wg.Add(2)
			go func() {
				defer wg.Done()
				r.LockPaths([]string{"/a", "/b", "/c"})()
			}()
			go func() {
				defer wg.Done()
				r.LockPaths([]string{"/c", "/b", "/a"})()
			}()
		}
		wg.Wait()
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("deadlock acquiring overlapping path sets")
	}
}

func TestFileStateConcurrentAccess(t *testing.T) {
	t.Parallel()
	r := NewFileStateRegistry()

	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			agent := "agent"
			path := "/repo/shared.go"
			r.RecordRead(agent, path)
			release := r.LockPath(path)
			r.RecordWrite("writer", path)
			release()
			r.CheckStale(agent, path)
			r.WritesSince(agent, time.Time{})
		}(i)
	}
	wg.Wait()
}
