package agent

import (
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/tools"
)

// TestConversationalPhrasesDoNotGateTools is the AD-107 regression test. A
// phrase table used to turn any of these messages into a session-wide tool ban
// that neither /readonly off nor a mode switch could clear. Two of them are the
// nastiest cases: the deny message the user pastes back when asking why they
// are blocked, and an ordinary sentence that happens to contain "read-only".
func TestConversationalPhrasesDoNotGateTools(t *testing.T) {
	t.Parallel()

	messages := []string{
		"don't change anything yet, just discuss this",
		"inspect mode: modifying files is not allowed; session is in read-only inspection mode",
		"mount the volume read-only and tell me what you see",
		"make that field read only in the schema",
	}

	batches := make([][]provider.StreamResponse, len(messages))
	for i := range batches {
		batches[i] = []provider.StreamResponse{{TextDelta: "ok", Done: true}}
	}

	runner, err := NewRunner(RunnerConfig{
		Generator: &fakeGenerator{batches: batches},
		Model:     "test-model",
		WorkDir:   t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	for _, msg := range messages {
		events, err := runner.RunTurn(testContext(t), msg)
		if err != nil {
			t.Fatalf("RunTurn(%q): %v", msg, err)
		}
		_ = collectEvents(t, events)

		if got := runner.readOnlyPolicy(); got != tools.PolicyNone {
			t.Fatalf("readOnlyPolicy after %q = %v, want PolicyNone (only /readonly may gate tools)", msg, got)
		}
	}
}

// TestReadOnlyPostureGatesAndLifts verifies the one remaining trigger still
// works in both directions, and that /readonly off genuinely clears it.
func TestReadOnlyPostureGatesAndLifts(t *testing.T) {
	t.Parallel()

	runner, err := NewRunner(RunnerConfig{
		Generator: &fakeGenerator{},
		Model:     "test-model",
		WorkDir:   t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	if got := runner.readOnlyPolicy(); got != tools.PolicyNone {
		t.Fatalf("initial readOnlyPolicy = %v, want PolicyNone", got)
	}

	if err := runner.SetReadOnly(true); err != nil {
		t.Fatalf("SetReadOnly(true): %v", err)
	}
	if got := runner.readOnlyPolicy(); got != tools.PolicyInspect {
		t.Fatalf("readOnlyPolicy with posture on = %v, want PolicyInspect", got)
	}
	if !runner.ReadOnlyActive() {
		t.Fatal("ReadOnlyActive() = false with the posture on; /readonly status would misreport it")
	}

	if err := runner.SetReadOnly(false); err != nil {
		t.Fatalf("SetReadOnly(false): %v", err)
	}
	if got := runner.readOnlyPolicy(); got != tools.PolicyNone {
		t.Fatalf("readOnlyPolicy after /readonly off = %v, want PolicyNone", got)
	}
	if runner.ReadOnlyActive() {
		t.Fatal("ReadOnlyActive() = true after /readonly off")
	}
}

// TestReadOnlyDirectiveNamesTheExit asserts the system suffix tells the model
// how the user lifts the posture. Without it the model has been observed
// inventing advice that does not work, such as restarting the process.
func TestReadOnlyDirectiveNamesTheExit(t *testing.T) {
	t.Parallel()

	runner, err := NewRunner(RunnerConfig{
		Generator: &fakeGenerator{},
		Model:     "test-model",
		WorkDir:   t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	if err := runner.SetReadOnly(true); err != nil {
		t.Fatalf("SetReadOnly(true): %v", err)
	}

	runner.modelMu.RLock()
	system := runner.system
	runner.modelMu.RUnlock()

	for _, want := range []string{"READ-ONLY INSPECTION MODE", "/readonly off"} {
		if !strings.Contains(system, want) {
			t.Fatalf("system instruction missing %q:\n%s", want, system)
		}
	}
}
