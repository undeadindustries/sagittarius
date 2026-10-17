package agent

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/contextmgmt"
	"github.com/undeadindustries/sagittarius/internal/modes"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/session"
	"github.com/undeadindustries/sagittarius/internal/tools"
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

func TestScratchpadReachesSystemInstructionAtRebuildPoints(t *testing.T) {
	t.Parallel()

	runner := newScratchpadRunner(t)
	if got := runner.Scratchpad(); got != "" {
		t.Fatalf("initial Scratchpad() = %q, want empty", got)
	}
	sysBefore := systemInstruction(t, runner)

	const note = "Port 8015 serves DiffusionGemma; the bridge script is vllm_scripts/serve-diffusiongemma.py"
	if err := runner.SetScratchpad(note); err != nil {
		t.Fatalf("SetScratchpad: %v", err)
	}
	if got := runner.Scratchpad(); got != note {
		t.Errorf("Scratchpad() = %q, want %q", got, note)
	}

	// SetScratchpad is deferred: r.system must be byte-identical until a rebuild point.
	if got := systemInstruction(t, runner); got != sysBefore {
		t.Fatal("SetScratchpad mutated r.system before a rebuild point")
	}

	// Trigger a rebuild point and verify the live note now renders in r.system.
	runner.refreshSystemAtRebuild("compression")
	system := systemInstruction(t, runner)
	if !strings.Contains(system, note) {
		t.Fatalf("system instruction missing the note after rebuild:\n%s", system)
	}
	if !strings.Contains(system, "This is your note as of the last context refresh.") {
		t.Errorf("system instruction missing context-refresh framing:\n%s", system)
	}
	// The framing is what keeps the model from copying the block into a reply
	// (AD-119). Without it this feature reintroduces that failure.
	if !strings.Contains(system, "never reproduce this block") {
		t.Errorf("system instruction missing the no-reproduce framing:\n%s", system)
	}

	// ClearScratchpad (/scratchpad clear) is user-initiated, so it applies immediately.
	if err := runner.ClearScratchpad(); err != nil {
		t.Fatalf("ClearScratchpad: %v", err)
	}
	if got := runner.Scratchpad(); got != "" {
		t.Errorf("Scratchpad() after clear = %q, want empty", got)
	}
	if strings.Contains(systemInstruction(t, runner), note) {
		t.Error("cleared note is still in the system instruction after ClearScratchpad")
	}
}

func TestScratchpadRebuildPointsTable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		trigger func(t *testing.T, r *Runner)
	}{
		{
			name: "mode-change",
			trigger: func(_ *testing.T, r *Runner) {
				r.SetInteractionMode(modes.ModePlan)
			},
		},
		{
			name: "constraints-set",
			trigger: func(t *testing.T, r *Runner) {
				if err := r.AddConstraint("no network"); err != nil {
					t.Fatalf("AddConstraint: %v", err)
				}
			},
		},
		{
			name: "readonly-toggle",
			trigger: func(t *testing.T, r *Runner) {
				if err := r.SetReadOnly(true); err != nil {
					t.Fatalf("SetReadOnly: %v", err)
				}
			},
		},
		{
			name: "truncation-budget",
			trigger: func(_ *testing.T, r *Runner) {
				r.refreshSystemAtRebuild("truncation")
			},
		},
		{
			name: "session-switch",
			trigger: func(t *testing.T, r *Runner) {
				dir := t.TempDir()
				rec := session.NewRecorder(dir, "switch-target", session.ProjectHash(dir), "main")
				if err := rec.SetScratchpad("live note from target session"); err != nil {
					t.Fatalf("SetScratchpad on recorder: %v", err)
				}
				loaded, err := session.LoadSession(rec.FilePath())
				if err != nil {
					t.Fatalf("LoadSession: %v", err)
				}
				if err := r.SwitchSession(&session.SelectionResult{
					Record:      loaded,
					SessionPath: rec.FilePath(),
				}); err != nil {
					t.Fatalf("SwitchSession: %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			r, err := NewRunner(RunnerConfig{
				Generator:       &fakeGenerator{},
				Model:           "test-model",
				WorkDir:         dir,
				Interactive:     false,
				SessionRecorder: session.NewRecorder(dir, "initial-sess", session.ProjectHash(dir), "main"),
				LivenessDir:     t.TempDir(),
			})
			if err != nil {
				t.Fatalf("NewRunner: %v", err)
			}
			wantNote := "live note for " + tt.name
			if tt.name == "session-switch" {
				wantNote = "live note from target session"
			} else {
				if err := r.SetScratchpad(wantNote); err != nil {
					t.Fatalf("SetScratchpad: %v", err)
				}
				if strings.Contains(systemInstruction(t, r), wantNote) {
					t.Fatal("note appeared in r.system before rebuild point")
				}
			}
			tt.trigger(t, r)
			if !strings.Contains(systemInstruction(t, r), wantNote) {
				t.Fatalf("r.system missing %q after %s rebuild point", wantNote, tt.name)
			}
		})
	}
}

func TestClearScratchpadAppendsNoticeOnceToNextUserTurn(t *testing.T) {
	t.Parallel()

	gen := &fakeGenerator{batches: [][]provider.StreamResponse{
		{{TextDelta: "ok 1", Done: true}},
		{{TextDelta: "ok 2", Done: true}},
	}}
	runner, err := NewRunner(RunnerConfig{
		Generator:         gen,
		Model:             "test-model",
		WorkDir:           t.TempDir(),
		Interactive:       false,
		InitialScratchpad: "old note",
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	if !strings.Contains(systemInstruction(t, runner), "old note") {
		t.Fatal("expected seeded note in initial system instruction")
	}

	if err := runner.ClearScratchpad(); err != nil {
		t.Fatalf("ClearScratchpad: %v", err)
	}
	if strings.Contains(systemInstruction(t, runner), "old note") {
		t.Fatal("expected ClearScratchpad to remove old note from system instruction immediately")
	}

	evs1, err := runner.RunTurn(testContext(t), "first prompt after clear")
	if err != nil {
		t.Fatalf("RunTurn 1: %v", err)
	}
	collectEvents(t, evs1)
	req1 := gen.lastRequest()
	if req1 == nil || len(req1.Messages) == 0 {
		t.Fatal("missing request on turn 1")
	}
	lastMsg1 := req1.Messages[len(req1.Messages)-1]
	if len(lastMsg1.Parts) == 0 || !strings.Contains(lastMsg1.Parts[len(lastMsg1.Parts)-1].Text, scratchpadClearedNotice) {
		t.Fatalf("turn 1 user message missing cleared notice: %+v", lastMsg1)
	}

	evs2, err := runner.RunTurn(testContext(t), "second prompt after clear")
	if err != nil {
		t.Fatalf("RunTurn 2: %v", err)
	}
	collectEvents(t, evs2)
	req2 := gen.lastRequest()
	lastMsg2 := req2.Messages[len(req2.Messages)-1]
	if len(lastMsg2.Parts) == 0 || strings.Contains(lastMsg2.Parts[len(lastMsg2.Parts)-1].Text, scratchpadClearedNotice) {
		t.Fatalf("turn 2 user message should not repeat cleared notice: %+v", lastMsg2)
	}
}

func TestScratchpadVisibilityInvariantAcrossLifecycle(t *testing.T) {
	t.Parallel()

	gen := &fakeGenerator{batches: [][]provider.StreamResponse{
		{{TextDelta: "<state_snapshot>compressed</state_snapshot>"}, {Done: true}},
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
		"sess-invariant", nil, nil)
	runner.SetContextManager(mgr)

	// Helper asserting the invariant: latest note is visible in either r.system
	// or an unmasked update_scratchpad FunctionResponse in history.
	assertNoteVisible := func(step, want string) {
		t.Helper()
		if strings.Contains(systemInstruction(t, runner), want) {
			return
		}
		for _, m := range runner.History() {
			for _, p := range m.Parts {
				if p.FunctionResponse != nil && p.FunctionResponse.Name == tools.UpdateScratchpadToolName {
					if c, _ := p.FunctionResponse.Response["content"].(string); strings.Contains(c, want) && !strings.Contains(c, contextmgmt.MaskingIndicatorTag) {
						return
					}
				}
			}
		}
		t.Fatalf("[%s] invariant violated: note %q not found in r.system or unmasked update_scratchpad result", step, want)
	}

	// Step 1: Model writes note v1 via tool call.
	const noteV1 = "note v1: port 8080"
	if err := runner.SetScratchpad(noteV1); err != nil {
		t.Fatal(err)
	}
	history := []provider.Message{
		{Role: provider.RoleUser, Parts: []provider.Part{{Text: "start task"}}},
		{Role: provider.RoleModel, Parts: []provider.Part{{FunctionCall: &provider.ToolCall{ID: "c1", Name: tools.UpdateScratchpadToolName, Args: map[string]any{"content": noteV1}}}}},
		{Role: provider.RoleUser, Parts: []provider.Part{{FunctionResponse: &provider.FunctionResponse{CallID: "c1", Name: tools.UpdateScratchpadToolName, Response: map[string]any{"status": "updated", "content": noteV1}}}}},
	}
	for i := 0; i < 8; i++ {
		history = append(history,
			provider.Message{Role: provider.RoleUser, Parts: []provider.Part{{Text: strings.Repeat("filler question ", 200)}}},
			provider.Message{Role: provider.RoleModel, Parts: []provider.Part{{Text: strings.Repeat("filler answer ", 200)}}},
		)
	}
	runner.ReplaceHistory(history, nil)
	assertNoteVisible("after write v1 (in tool result)", noteV1)

	// Step 2: ForceCompress summarizes early history (wiping c1) and rebuilds system.
	if _, err := runner.ForceCompress(context.Background()); err != nil {
		t.Fatalf("ForceCompress: %v", err)
	}
	assertNoteVisible("after compress (in system)", noteV1)

	// Step 3: Model writes note v2 after compress. System still has v1, tool result has v2.
	const noteV2 = "note v2: port 9090"
	if err := runner.SetScratchpad(noteV2); err != nil {
		t.Fatal(err)
	}
	h2 := append(runner.History(),
		provider.Message{Role: provider.RoleModel, Parts: []provider.Part{{FunctionCall: &provider.ToolCall{ID: "c2", Name: tools.UpdateScratchpadToolName, Args: map[string]any{"content": noteV2}}}}},
		provider.Message{Role: provider.RoleUser, Parts: []provider.Part{{FunctionResponse: &provider.FunctionResponse{CallID: "c2", Name: tools.UpdateScratchpadToolName, Response: map[string]any{"status": "updated", "content": strings.Repeat("p", 800) + " " + noteV2}}}}},
		// Add another bulky read_file turn so masking scans past the latest turn.
		provider.Message{Role: provider.RoleModel, Parts: []provider.Part{{FunctionCall: &provider.ToolCall{ID: "c3", Name: "read_file"}}}},
		provider.Message{Role: provider.RoleUser, Parts: []provider.Part{{FunctionResponse: &provider.FunctionResponse{CallID: "c3", Name: "read_file", Response: map[string]any{"output": strings.Repeat("bulk ", 5000)}}}}},
		provider.Message{Role: provider.RoleUser, Parts: []provider.Part{{Text: "latest turn"}}},
	)
	runner.ReplaceHistory(h2, nil)

	// Step 4: Run Masker with tiny thresholds — update_scratchpad must be exempt!
	masker := &contextmgmt.Masker{
		OutputDir:   t.TempDir(),
		ExemptTools: contextmgmt.DefaultExemptTools(),
	}
	maskRes, err := masker.Mask(runner.History(), contextmgmt.ToolOutputMaskingConfig{
		ProtectionThresholdTokens:  10,
		MinPrunableThresholdTokens: 10,
		ProtectLatestTurn:          true,
	})
	if err != nil {
		t.Fatalf("Mask: %v", err)
	}
	if maskRes.MaskedCount == 0 {
		t.Fatal("expected read_file to be masked")
	}
	runner.ReplaceHistory(maskRes.NewHistory, nil)
	assertNoteVisible("after masking (update_scratchpad exempt)", noteV2)

	// Step 5: Overflow truncation rebuilds the system prompt with noteV2.
	runner.refreshSystemAtRebuild("truncation")
	if !strings.Contains(systemInstruction(t, runner), noteV2) {
		t.Fatal("expected noteV2 in r.system after truncation rebuild")
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
