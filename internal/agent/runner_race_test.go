package agent

import (
	"sync"
	"testing"
)

// TestRunnerHistoryConcurrentAccess guards history + turnCounter against a data
// race: between-turns mutators (History, ClearHistory, ReplaceHistory) run on
// the TUI/slash goroutine while a turn goroutine appends. historyMu must make
// these safe. Run with -race to catch unsynchronized access.
func TestRunnerHistoryConcurrentAccess(t *testing.T) {
	t.Parallel()

	r := newRaceTestRunner(t)

	const iterations = 50
	var wg sync.WaitGroup
	for i := 0; i < iterations; i++ {
		wg.Add(3)
		go func() { defer wg.Done(); _ = r.History() }()
		go func() { defer wg.Done(); r.ClearHistory() }()
		go func() { defer wg.Done(); _ = r.LastAssistantText() }()
	}
	wg.Wait()
}

// TestRunnerModelFieldsConcurrentAccess guards providerDefaultModel + modelPinned
// (now folded under modelMu) against concurrent readers/writers. The turn
// goroutine reads them via refreshModelFromMode while hooks mutate them.
func TestRunnerModelFieldsConcurrentAccess(t *testing.T) {
	t.Parallel()

	r := newRaceTestRunner(t)

	const iterations = 50
	var wg sync.WaitGroup
	for i := 0; i < iterations; i++ {
		wg.Add(3)
		go func() { defer wg.Done(); r.SetProviderDefaultModel("model-a") }()
		go func() { defer wg.Done(); _ = r.ModelPinned() }()
		go func() { defer wg.Done(); r.refreshModelFromMode() }()
	}
	wg.Wait()
}

// TestAppStatusConcurrentAccess guards App.status: Status() is read by the TUI
// while background goroutines (RebuildRunner, cycleModel, mode switches) write
// it. statusMu must serialize them. Before the fix this races under -race.
func TestAppStatusConcurrentAccess(t *testing.T) {
	t.Parallel()

	runner := newRaceTestRunner(t)
	app := NewApp(AppConfig{
		Runner:        runner,
		ProviderLabel: "test",
		Model:         "test-model",
	})

	const iterations = 50
	var wg sync.WaitGroup
	for i := 0; i < iterations; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); _ = app.Status() }()
		go func() {
			defer wg.Done()
			app.statusMu.Lock()
			app.status.Right = providerModelLabel("test", "model-b")
			app.statusMu.Unlock()
		}()
	}
	wg.Wait()
}

func newRaceTestRunner(t *testing.T) *Runner {
	t.Helper()
	r, err := NewRunner(RunnerConfig{
		Generator:   &fakeGenerator{},
		Model:       "test-model",
		WorkDir:     t.TempDir(),
		Interactive: false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	return r
}
