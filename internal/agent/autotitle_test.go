package agent

import (
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/session"
)

func TestSanitizeTitle(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "Fix LSP pool race", "Fix LSP pool race"},
		{"trailing period dropped", "Fix LSP pool race.", "Fix LSP pool race"},
		{"trailing punctuation set dropped", "Add retry logic!?", "Add retry logic"},
		{"surrounding quotes stripped", "\"Fix LSP pool race\"", "Fix LSP pool race"},
		{"smart quotes stripped", "“Fix LSP pool race”", "Fix LSP pool race"},
		{"internal whitespace collapsed", "Fix   LSP\t pool  race", "Fix LSP pool race"},
		{"first line only", "Fix LSP pool race\nHere is why", "Fix LSP pool race"},
		{"control chars stripped", "Fix\x00 LSP pool race", "Fix LSP pool race"},
		{"empty rejected", "   ", ""},
		{"only punctuation rejected", "...", ""},
		{"too many words rejected", "one two three four five six seven", ""},
		{"six words accepted", "one two three four five six", "one two three four five six"},
		{"filler analyze rejected", "Analyze code and persona", ""},
		{"filler help with rejected", "Help with the build", ""},
		{"filler discussion about rejected", "Discussion about sessions", ""},
		{"filler question about rejected", "Question about recovery", ""},
		{"filler how to rejected", "How to fix the race", ""},
		{"filler bare word rejected", "analyze", ""},
		{"non-filler verb ok", "Refactor the loader", "Refactor the loader"},
		{"too long rejected", "thisisaverylongsinglewordthatexceedstheeightyrunebudgetfortitlesandshouldberejectedok", ""},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := sanitizeTitle(tc.in); got != tc.want {
				t.Errorf("sanitizeTitle(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestHasFillerPrefix(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"Analyze x", "help with y", "question about z", "How to w"} {
		if !hasFillerPrefix(in) {
			t.Errorf("hasFillerPrefix(%q) = false, want true", in)
		}
	}
	for _, in := range []string{"Fix LSP pool race", "Rename the picker", "refactor loader"} {
		if hasFillerPrefix(in) {
			t.Errorf("hasFillerPrefix(%q) = true, want false", in)
		}
	}
}

// newTitleRunner builds a Runner with a session recorder in a temp chats dir so
// auto-titling has somewhere to write. The fake generator's batches serve the
// conversation turn first, then the aux titling call.
func newTitleRunner(t *testing.T, gen *fakeGenerator, policy string) *Runner {
	t.Helper()
	workDir := t.TempDir()
	chatsDir := t.TempDir()
	rec := session.NewRecorder(chatsDir, "title-test", "hash", "main")

	settings := &config.Settings{
		Sagittarius: &config.SagittariusSettings{
			Sessions: &config.SagittariusSessionsConfig{AutoTitle: &policy},
		},
	}

	runner, err := NewRunner(RunnerConfig{
		Generator:       gen,
		Model:           "test-model",
		WorkDir:         workDir,
		Interactive:     false,
		SessionRecorder: rec,
		Settings:        settings,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	return runner
}

// TestAutoTitleAppliesTitle verifies the first turn triggers a title generation
// that is written to the session and announced in prompt mode.
func TestAutoTitleAppliesTitle(t *testing.T) {
	t.Parallel()

	gen := &fakeGenerator{batches: [][]provider.StreamResponse{
		// Conversation turn reply.
		{{TextDelta: "Done, fixed the race."}, {Done: true}},
		// Aux titling call reply.
		{{TextDelta: "Fix LSP pool race"}, {Done: true}},
	}}
	policy := "prompt"
	runner := newTitleRunner(t, gen, policy)

	drainEvents(t, mustRunTurn(t, runner, "fix the lsp pool deadlock"))

	if got := runner.sessionRecorder.Summary(); got != "Fix LSP pool race" {
		t.Errorf("Summary = %q, want %q", got, "Fix LSP pool race")
	}
	if got := runner.TitleAnnouncement(); got != "Fix LSP pool race" {
		t.Errorf("TitleAnnouncement = %q, want %q", got, "Fix LSP pool race")
	}
	// Peek semantics: a second read returns the same value (the TUI owns the
	// shown-once / dismissed lifecycle, so the provider never consumes it).
	if got := runner.TitleAnnouncement(); got != "Fix LSP pool race" {
		t.Errorf("TitleAnnouncement second read = %q, want %q (peek, not consumed)", got, "Fix LSP pool race")
	}
}

// TestAutoTitleFireOnce verifies the titling call happens only once even across
// multiple turns.
func TestAutoTitleFireOnce(t *testing.T) {
	t.Parallel()

	gen := &fakeGenerator{batches: [][]provider.StreamResponse{
		{{TextDelta: "first reply"}, {Done: true}},
		{{TextDelta: "Fix thing"}, {Done: true}},
		{{TextDelta: "second reply"}, {Done: true}},
	}}
	policy := "auto"
	runner := newTitleRunner(t, gen, policy)

	drainEvents(t, mustRunTurn(t, runner, "first"))
	drainEvents(t, mustRunTurn(t, runner, "second"))

	gen.mu.Lock()
	calls := gen.call
	gen.mu.Unlock()
	// 2 conversation turns + 1 titling call = 3, not 4.
	if calls != 3 {
		t.Errorf("generator call count = %d, want 3 (fire-once titling)", calls)
	}
}

// TestAutoTitleRejectsFiller verifies a filler-prefixed model answer leaves the
// session untitled (falls back to the first-message display name).
func TestAutoTitleRejectsFiller(t *testing.T) {
	t.Parallel()

	gen := &fakeGenerator{batches: [][]provider.StreamResponse{
		{{TextDelta: "analyzed it"}, {Done: true}},
		{{TextDelta: "Analyze code and persona"}, {Done: true}},
	}}
	policy := "auto"
	runner := newTitleRunner(t, gen, policy)

	drainEvents(t, mustRunTurn(t, runner, "analyze this code"))

	if got := runner.sessionRecorder.Summary(); got != "" {
		t.Errorf("Summary = %q, want empty (filler rejected)", got)
	}
}

// TestAutoTitleOffSkipsModelCall verifies the off policy never calls the aux
// model.
func TestAutoTitleOffSkipsModelCall(t *testing.T) {
	t.Parallel()

	gen := &fakeGenerator{batches: [][]provider.StreamResponse{
		{{TextDelta: "reply"}, {Done: true}},
	}}
	policy := "off"
	runner := newTitleRunner(t, gen, policy)

	drainEvents(t, mustRunTurn(t, runner, "hello"))

	gen.mu.Lock()
	calls := gen.call
	gen.mu.Unlock()
	if calls != 1 {
		t.Errorf("generator call count = %d, want 1 (off skips titling)", calls)
	}
	if got := runner.sessionRecorder.Summary(); got != "" {
		t.Errorf("Summary = %q, want empty", got)
	}
}

// TestAutoTitleNeverOverwritesExisting verifies a pre-existing title (manual
// rename) is preserved.
func TestAutoTitleNeverOverwritesExisting(t *testing.T) {
	t.Parallel()

	gen := &fakeGenerator{batches: [][]provider.StreamResponse{
		{{TextDelta: "reply"}, {Done: true}},
		{{TextDelta: "Model chosen title"}, {Done: true}},
	}}
	policy := "auto"
	runner := newTitleRunner(t, gen, policy)
	if err := runner.sessionRecorder.SetSummary("My manual title"); err != nil {
		t.Fatalf("SetSummary: %v", err)
	}

	drainEvents(t, mustRunTurn(t, runner, "hello"))

	if got := runner.sessionRecorder.Summary(); got != "My manual title" {
		t.Errorf("Summary = %q, want preserved %q", got, "My manual title")
	}
}

// TestAutoTitleAutoModeSilent verifies auto mode applies the title without an
// announcement.
func TestAutoTitleAutoModeSilent(t *testing.T) {
	t.Parallel()

	gen := &fakeGenerator{batches: [][]provider.StreamResponse{
		{{TextDelta: "reply"}, {Done: true}},
		{{TextDelta: "Fix the thing"}, {Done: true}},
	}}
	policy := "auto"
	runner := newTitleRunner(t, gen, policy)

	drainEvents(t, mustRunTurn(t, runner, "hello"))

	if got := runner.sessionRecorder.Summary(); got != "Fix the thing" {
		t.Errorf("Summary = %q, want %q", got, "Fix the thing")
	}
	if got := runner.TitleAnnouncement(); got != "" {
		t.Errorf("TitleAnnouncement = %q, want empty in auto mode", got)
	}
}
