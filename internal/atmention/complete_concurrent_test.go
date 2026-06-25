package atmention

import (
	"sync"
	"testing"
	"time"
)

// TestFilesConcurrentNonBlocking hammers Index.files() (indirectly, via the same
// serve-stale + background-refresh path) from many goroutines and asserts: (1) no
// data race on the cache fields (run under -race), and (2) calls return promptly
// without blocking on the workspace walk. The first call may return nil (the
// background walk has not landed yet); once it does, subsequent calls see data.
func TestFilesConcurrentNonBlocking(t *testing.T) {
	t.Parallel()

	files := map[string]string{}
	for i := 0; i < 500; i++ {
		files["dir/file"+itoa(i)+".go"] = "x"
	}
	ws := newWorkspace(t, files)
	idx := NewIndex(ws)

	const goroutines = 32
	const itersEach = 200
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < itersEach; i++ {
				// Each call must return quickly even while a background refresh
				// is in flight; a blocking walk under the lock would serialize
				// these and blow the deadline below.
				start := time.Now()
				_ = idx.files()
				if time.Since(start) > time.Second {
					t.Errorf("files() blocked for %v, expected near-immediate return", time.Since(start))
					return
				}
			}
		}()
	}
	wg.Wait()

	// After the hammering, give any in-flight refresh a moment to land, then
	// confirm the cache eventually populates (not stuck on nil).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(idx.files()) > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("cache never populated after concurrent access")
}

// itoa is a tiny dependency-free int->string helper for test file names.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
