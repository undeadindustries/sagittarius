package agent

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/modes"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/session"
)

func TestSanitizeScratchpad(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "whitespace only", in: "  \n\t ", want: ""},
		{name: "trims surrounding space", in: "\n  note  \n", want: "note"},
		{name: "normalizes CRLF", in: "a\r\nb\rc", want: "a\nb\nc"},
		{name: "keeps interior newlines", in: "line 1\nline 2", want: "line 1\nline 2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := sanitizeScratchpad(tt.in); got != tt.want {
				t.Errorf("sanitizeScratchpad(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestSanitizeScratchpadCapsOnRuneBoundary pins the cap against multi-byte
// input. Cutting by bytes would split a rune and put invalid UTF-8 into the
// system prompt, which some providers reject outright.
func TestSanitizeScratchpadCapsOnRuneBoundary(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("日", ScratchpadMaxRunes+500)
	got := sanitizeScratchpad(long)

	if n := len([]rune(got)); n != ScratchpadMaxRunes {
		t.Fatalf("capped length = %d runes, want %d", n, ScratchpadMaxRunes)
	}
	if !strings.HasPrefix(long, got) {
		t.Fatal("capped text is not a prefix of the input: the cut split a rune")
	}
}

func newScratchpadRunner(t *testing.T) *Runner {
	t.Helper()
	runner, err := NewRunner(RunnerConfig{
		Generator:   &fakeGenerator{},
		Model:       "test-model",
		WorkDir:     t.TempDir(),
		Interactive: false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	return runner
}

func systemInstruction(t *testing.T, r *Runner) string {
	t.Helper()
	r.modelMu.RLock()
	defer r.modelMu.RUnlock()
	return r.system
}

func TestScratchpadReachesSystemInstruction(t *testing.T) {
	t.Parallel()

	runner := newScratchpadRunner(t)
	if got := runner.Scratchpad(); got != "" {
		t.Fatalf("initial Scratchpad() = %q, want empty", got)
	}

	const note = "Port 8015 serves DiffusionGemma; the bridge script is vllm_scripts/serve-diffusiongemma.py"
	if err := runner.SetScratchpad(note); err != nil {
		t.Fatalf("SetScratchpad: %v", err)
	}
	if got := runner.Scratchpad(); got != note {
		t.Errorf("Scratchpad() = %q, want %q", got, note)
	}

	system := systemInstruction(t, runner)
	if !strings.Contains(system, note) {
		t.Fatalf("system instruction missing the note:\n%s", system)
	}
	// The framing is what keeps the model from copying the block into a reply
	// (AD-119). Without it this feature reintroduces that failure.
	if !strings.Contains(system, "never reproduce this block") {
		t.Errorf("system instruction missing the no-reproduce framing:\n%s", system)
	}

	if err := runner.ClearScratchpad(); err != nil {
		t.Fatalf("ClearScratchpad: %v", err)
	}
	if got := runner.Scratchpad(); got != "" {
		t.Errorf("Scratchpad() after clear = %q, want empty", got)
	}
	if strings.Contains(systemInstruction(t, runner), note) {
		t.Error("cleared note is still in the system instruction")
	}
}

// TestScratchpadOutranksNothingAndConstraintsWin pins the ordering inside the
// suffix: the scratchpad is the model's own reference material, so a standing
// user constraint must still be the last thing the model reads.
func TestScratchpadOrderedBeforeConstraints(t *testing.T) {
	t.Parallel()

	runner := newScratchpadRunner(t)
	if err := runner.SetScratchpad("working note"); err != nil {
		t.Fatalf("SetScratchpad: %v", err)
	}
	if err := runner.AddConstraint("do not touch AGENTS.md"); err != nil {
		t.Fatalf("AddConstraint: %v", err)
	}

	system := systemInstruction(t, runner)
	pad := strings.Index(system, "**Your scratchpad.**")
	con := strings.Index(system, "**Active session constraints.**")
	if pad < 0 || con < 0 {
		t.Fatalf("missing a block: scratchpad=%d constraints=%d\n%s", pad, con, system)
	}
	if pad > con {
		t.Error("scratchpad renders after constraints; constraints must keep last-word recency")
	}
}

// TestScratchpadSurvivesCompression is the load-bearing assertion for the whole
// feature: compression rewrites history, and the note must not be in history to
// be rewritten. ForceCompress here runs against a real summarizer-backed
// manager, so the history really is replaced.
func TestScratchpadSurvivesCompression(t *testing.T) {
	t.Parallel()

	gen := &fakeGenerator{batches: [][]provider.StreamResponse{
		{{TextDelta: "<state_snapshot>earlier work summarized</state_snapshot>"}, {Done: true}},
	}}
	settings := &config.Settings{
		Providers: &config.ProvidersSettings{
			Active: string(config.BuiltInOpenAI),
		},
	}
	runner, err := NewRunner(RunnerConfig{
		Generator:   gen,
		Model:       "test-model",
		WorkDir:     t.TempDir(),
		Interactive: false,
		Settings:    settings,
		InitialMode: modes.ModeAgent,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	mgr := NewContextManager(settings, gen, runner.CompressionModel,
		runner.ActiveProviderID, func() string { return runner.InteractionMode().String() },
		"sess-scratchpad", nil, nil)
	if mgr == nil {
		t.Fatal("expected a context manager for the openai-chat provider")
	}
	runner.SetContextManager(mgr)
	if !mgr.CompressionAvailable() {
		t.Fatal("compression unavailable: the test would prove nothing")
	}

	const note = "The failing test is TestFoo in internal/bar; root cause is the nil map"
	if err := runner.SetScratchpad(note); err != nil {
		t.Fatalf("SetScratchpad: %v", err)
	}

	history := make([]provider.Message, 0, 20)
	for i := 0; i < 10; i++ {
		history = append(history,
			provider.Message{Role: provider.RoleUser, Parts: []provider.Part{{Text: strings.Repeat("question ", 200)}}},
			provider.Message{Role: provider.RoleModel, Parts: []provider.Part{{Text: strings.Repeat("answer ", 200)}}},
		)
	}
	runner.ReplaceHistory(history, nil)

	if _, err := runner.ForceCompress(context.Background()); err != nil {
		t.Fatalf("ForceCompress: %v", err)
	}
	if got := len(runner.History()); got >= len(history) {
		t.Fatalf("history length = %d after compression, want fewer than %d", got, len(history))
	}

	if got := runner.Scratchpad(); got != note {
		t.Errorf("Scratchpad() = %q after compression, want it untouched", got)
	}
	if !strings.Contains(systemInstruction(t, runner), note) {
		t.Error("the note is gone from the system instruction after compression")
	}
}

// TestScratchpadInitialSeeding covers the --resume path: a note written before
// the restart must be in the system prompt on the first turn after it, without
// waiting for the model to call update_scratchpad again.
func TestScratchpadInitialSeeding(t *testing.T) {
	t.Parallel()

	runner, err := NewRunner(RunnerConfig{
		Generator:         &fakeGenerator{},
		Model:             "test-model",
		WorkDir:           t.TempDir(),
		Interactive:       false,
		InitialScratchpad: "  resumed note  ",
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	if got := runner.Scratchpad(); got != "resumed note" {
		t.Fatalf("Scratchpad() = %q, want the sanitized seed", got)
	}
	if !strings.Contains(systemInstruction(t, runner), "resumed note") {
		t.Error("seeded note never reached the system instruction")
	}
}

// TestScratchpadDirectiveSuppressedWhenToolDisabled ensures that if the scratchpad
// tool is disabled, even if a note was stored or seeded, it is suppressed from
// the system instruction so it does not waste tokens.
func TestScratchpadDirectiveSuppressedWhenToolDisabled(t *testing.T) {
	t.Parallel()

	off := false
	settings := &config.Settings{
		Sagittarius: &config.SagittariusSettings{
			ScratchpadEnabled: &off,
		},
	}
	runner, err := NewRunner(RunnerConfig{
		Generator:   &fakeGenerator{},
		Model:       "test-model",
		WorkDir:     t.TempDir(),
		Interactive: false,
		Settings:    settings,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	const note = "working note that should be suppressed"
	if err := runner.SetScratchpad(note); err != nil {
		t.Fatalf("SetScratchpad: %v", err)
	}

	system := systemInstruction(t, runner)
	if strings.Contains(system, note) {
		t.Fatalf("system instruction contains scratchpad note even though tool is disabled:\n%s", system)
	}
	if strings.Contains(system, "Your scratchpad") {
		t.Fatalf("system instruction contains scratchpad directive even though tool is disabled:\n%s", system)
	}
}

// TestSetScratchpadRetriesAfterPersistFailure verifies that if persistence fails,
// in-memory state is not committed, and calling SetScratchpad again with the same
// content retries the persist rather than hitting an early-return.
func TestSetScratchpadRetriesAfterPersistFailure(t *testing.T) {
	dir := t.TempDir()
	rec := session.NewRecorder(dir, "persist-retry-test", session.ProjectHash(dir), "main")

	runner, err := NewRunner(RunnerConfig{
		Generator:       &fakeGenerator{},
		Model:           "test-model",
		WorkDir:         dir,
		Interactive:     false,
		SessionRecorder: rec,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	// Make the session file read-only so the initial persist fails.
	filePath := rec.FilePath()
	if err := os.Chmod(filePath, 0o400); err != nil {
		t.Fatalf("Chmod read-only: %v", err)
	}

	const note = "critical state that must survive"

	// First attempt: should fail on persist.
	err = runner.SetScratchpad(note)
	if err == nil {
		t.Fatal("SetScratchpad succeeded despite read-only file; want error")
	}

	// In-memory note must NOT be committed on persist failure, so Scratchpad() is still empty.
	if got := runner.Scratchpad(); got != "" {
		t.Fatalf("Scratchpad() = %q after failed persist; want empty", got)
	}

	// Restore write permissions.
	if err := os.Chmod(filePath, 0o600); err != nil {
		t.Fatalf("Chmod writeable: %v", err)
	}

	// Second attempt with the EXACT SAME content must retry the persist instead of early-returning.
	if err := runner.SetScratchpad(note); err != nil {
		t.Fatalf("SetScratchpad retry failed: %v", err)
	}

	if got := runner.Scratchpad(); got != note {
		t.Fatalf("Scratchpad() = %q, want %q", got, note)
	}

	// Check that the note was actually persisted to the recorder file.
	loaded, err := session.LoadSession(filePath)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if loaded.Scratchpad != note {
		t.Fatalf("persisted scratchpad = %q, want %q", loaded.Scratchpad, note)
	}
}
