package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

func TestThinkingBudgetWatch(t *testing.T) {
	t.Parallel()

	// A delta long enough to force a recheck, and cheap to reason about.
	longDelta := strings.Repeat("a", thinkingBudgetRecheckChars)

	t.Run("zero budget never fires", func(t *testing.T) {
		t.Parallel()
		w := newThinkingBudgetWatch(0)
		w.add(strings.Repeat("a", 10*thinkingBudgetRecheckChars))
		if w.exceeded() {
			t.Fatal("a zero budget must never report exceeded")
		}
		if w.text() != "" {
			t.Fatalf("text = %q, want empty when disabled", w.text())
		}
	})

	t.Run("negative budget never fires", func(t *testing.T) {
		t.Parallel()
		w := newThinkingBudgetWatch(-5)
		w.add(longDelta)
		if w.exceeded() {
			t.Fatal("a negative budget must never report exceeded")
		}
	})

	t.Run("small deltas below the stride do not fire", func(t *testing.T) {
		t.Parallel()
		w := newThinkingBudgetWatch(1)
		// Well over one token's worth of characters, but under the recheck
		// stride, so the estimate has not been refreshed yet.
		for range 10 {
			w.add("abcde")
		}
		if w.exceeded() {
			t.Fatal("estimate should not have been refreshed below the stride")
		}
	})

	t.Run("fires once the estimate passes the budget", func(t *testing.T) {
		t.Parallel()
		w := newThinkingBudgetWatch(1)
		w.add(longDelta)
		if !w.exceeded() {
			t.Fatalf("tokenCount = %d, want > 1 and exceeded", w.tokenCount())
		}
		if !strings.Contains(w.text(), "aaa") {
			t.Fatal("text should return the accumulated reasoning")
		}
	})

	t.Run("stays under an ample budget", func(t *testing.T) {
		t.Parallel()
		w := newThinkingBudgetWatch(1_000_000)
		w.add(longDelta)
		if w.exceeded() {
			t.Fatalf("tokenCount = %d exceeded a 1M budget", w.tokenCount())
		}
	})

	t.Run("caps replayed text by keeping the tail", func(t *testing.T) {
		t.Parallel()
		w := newThinkingBudgetWatch(1)
		w.add(strings.Repeat("x", thinkingReplayMaxChars) + "THE-END")
		got := w.text()
		if !strings.Contains(got, "THE-END") {
			t.Error("the tail of the reasoning must survive the cap")
		}
		if !strings.Contains(got, "earlier reasoning omitted") {
			t.Error("an elision marker should say text was dropped")
		}
	})
}

func TestThinkingCutNote(t *testing.T) {
	t.Parallel()

	t.Run("frames replayed reasoning as reference only", func(t *testing.T) {
		t.Parallel()
		note := thinkingCutNote(2048, "step one: read the file")
		for _, want := range []string{
			"2048",
			"reference only",
			"not a new instruction",
			"must not",
			"<partial_reasoning>",
			"step one: read the file",
			"</partial_reasoning>",
			"Do not think further",
		} {
			if !strings.Contains(note, want) {
				t.Errorf("note missing %q:\n%s", want, note)
			}
		}
	})

	t.Run("blank reasoning still explains the stop", func(t *testing.T) {
		t.Parallel()
		note := thinkingCutNote(512, "   \n  ")
		if strings.Contains(note, "<partial_reasoning>") {
			t.Error("no reasoning to quote, so the wrapper must be omitted")
		}
		if !strings.Contains(note, "Thinking budget reached") ||
			!strings.Contains(note, "Do not think further") {
			t.Errorf("note should still state why and what to do:\n%s", note)
		}
	})
}

// localBudgetRunner builds a runner on an openai-chat custom provider whose
// per-model config is parsed from raw JSON, so the test exercises the real
// settings path rather than a hand-built struct.
func localBudgetRunner(t *testing.T, gen provider.ContentGenerator, modelJSON string) *Runner {
	t.Helper()
	runner, err := NewRunner(RunnerConfig{
		Generator: gen,
		Model:     "qwen3",
		WorkDir:   t.TempDir(),
		Settings: &config.Settings{Providers: &config.ProvidersSettings{
			Active: "local",
			Custom: map[string]config.CustomProviderDefinition{
				"local": {WireFormat: config.WireFormatOpenAIChat},
			},
			Extra: map[string]json.RawMessage{
				"local": json.RawMessage(`{"models":{"qwen3":` + modelJSON + `}}`),
			},
		}},
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	return runner
}

// reasoningBurst returns a stream of reasoning deltas large enough to trip any
// small budget, with no Done frame — a model that thinks and never answers.
func reasoningBurst() []provider.StreamResponse {
	out := make([]provider.StreamResponse, 0, 8)
	for range 8 {
		out = append(out, provider.StreamResponse{
			ReasoningDelta: strings.Repeat("thinking ", thinkingBudgetRecheckChars/8),
		})
	}
	return out
}

// TestThinkingBudgetAdvertisedWithoutHardFlag pins the always-try-native half:
// a budget with no hard flag still reaches the provider, because a server that
// enforces it during generation beats any client-side cut.
func TestThinkingBudgetAdvertisedWithoutHardFlag(t *testing.T) {
	t.Parallel()

	r := localBudgetRunner(t, &fakeGenerator{}, `{"thinkingBudgetTokens":4096}`)
	req := r.buildGenerateRequest()
	if req.ThinkingBudgetTokens != 4096 {
		t.Fatalf("ThinkingBudgetTokens = %d, want 4096", req.ThinkingBudgetTokens)
	}
	if req.SuppressThinking {
		t.Error("SuppressThinking = true on a fresh turn, want false")
	}
}

// TestThinkingBudgetCutRetriesWithSuppression is the end-to-end behavior: a
// model that only thinks gets cut, is handed its own reasoning back, and the
// retry asks the provider to skip thinking entirely.
func TestThinkingBudgetCutRetriesWithSuppression(t *testing.T) {
	t.Parallel()

	gen := &fakeGenerator{batches: [][]provider.StreamResponse{
		reasoningBurst(),
		{{TextDelta: "done thinking, here is the answer"}, {Done: true}},
	}}
	r := localBudgetRunner(t, gen, `{"thinkingBudgetTokens":1,"hardThinkingBudget":true}`)

	events := collectEvents(t, mustRunTurn(t, r, "solve it"))

	var notices, answers int
	for _, ev := range events {
		switch ev.Type {
		case ui.StreamThinkingBudget:
			notices++
			if !strings.Contains(ev.Text, "1 tokens") {
				t.Errorf("notice should name the budget: %q", ev.Text)
			}
			if !strings.Contains(ev.Text, "thinking disabled for the retry") {
				t.Errorf("notice should say what happens next: %q", ev.Text)
			}
		case ui.StreamTextDelta:
			if strings.Contains(ev.Text, "here is the answer") {
				answers++
			}
		}
	}
	if notices != 1 {
		t.Errorf("StreamThinkingBudget events = %d, want 1", notices)
	}
	if answers != 1 {
		t.Errorf("answer deltas = %d, want 1 (the retry must still produce a reply)", answers)
	}

	if got := gen.calls(); got != 2 {
		t.Fatalf("generator calls = %d, want 2 (cut round plus retry)", got)
	}
	retry := gen.lastRequest()
	if retry == nil {
		t.Fatal("no retry request recorded")
	}
	if !retry.SuppressThinking {
		t.Error("retry must set SuppressThinking")
	}
	if retry.ThinkingBudgetTokens != 0 {
		t.Errorf("retry ThinkingBudgetTokens = %d, want 0 when thinking is suppressed",
			retry.ThinkingBudgetTokens)
	}
	if retry.IncludeThoughts {
		t.Error("retry must not ask for thought text it just suppressed")
	}
	last := retry.Messages[len(retry.Messages)-1]
	if last.Role != provider.RoleUser {
		t.Fatalf("last message role = %q, want user", last.Role)
	}
	if !strings.Contains(last.Parts[0].Text, "Thinking budget reached") {
		t.Errorf("retry is missing the cut note:\n%s", last.Parts[0].Text)
	}
	if !strings.Contains(last.Parts[0].Text, "thinking") {
		t.Error("retry note should quote the reasoning the model already produced")
	}
}

// TestThinkingBudgetCutNotPersistedToHistory guards the note's scope: it exists
// for one retry request only. Leaving it in history would make every later
// round tell the model its thinking was cut.
func TestThinkingBudgetCutNotPersistedToHistory(t *testing.T) {
	t.Parallel()

	gen := &fakeGenerator{batches: [][]provider.StreamResponse{
		reasoningBurst(),
		{{TextDelta: "answer"}, {Done: true}},
	}}
	r := localBudgetRunner(t, gen, `{"thinkingBudgetTokens":1,"hardThinkingBudget":true}`)
	drainEvents(t, mustRunTurn(t, r, "solve it"))

	for _, msg := range r.History() {
		for _, p := range msg.Parts {
			if strings.Contains(p.Text, "Thinking budget reached") {
				t.Fatal("the cut note leaked into persistent history")
			}
		}
	}

	// A second turn must start clean.
	gen2 := &fakeGenerator{batches: [][]provider.StreamResponse{
		{{TextDelta: "second"}, {Done: true}},
	}}
	r2 := localBudgetRunner(t, gen2, `{"thinkingBudgetTokens":1,"hardThinkingBudget":true}`)
	drainEvents(t, mustRunTurn(t, r2, "hi"))
	if req := gen2.lastRequest(); req == nil || req.SuppressThinking {
		t.Error("a turn with no cut must not suppress thinking")
	}
}

// TestThinkingBudgetCutLatchedAgainstImmediateRepeat is the anti-loop guard: a
// cut disables the budget for exactly the retry round, so a model that thinks
// too long twice in a row cannot be cut twice in a row.
func TestThinkingBudgetCutLatchedAgainstImmediateRepeat(t *testing.T) {
	t.Parallel()

	gen := &fakeGenerator{batches: [][]provider.StreamResponse{
		reasoningBurst(),
		reasoningBurst(),
		{{TextDelta: "answer"}, {Done: true}},
	}}
	r := localBudgetRunner(t, gen, `{"thinkingBudgetTokens":1,"hardThinkingBudget":true}`)

	events := collectEvents(t, mustRunTurn(t, r, "solve it"))

	notices := 0
	for _, ev := range events {
		if ev.Type == ui.StreamThinkingBudget {
			notices++
		}
	}
	if notices != 1 {
		t.Fatalf("StreamThinkingBudget events = %d, want 1 (the retry round is never cut)", notices)
	}
}

// TestThinkingBudgetNotCutAfterAnswerStarts pins the phase check: the budget
// bounds thinking, so once answer text has arrived the round must run to
// completion rather than discarding a reply the model already began.
func TestThinkingBudgetNotCutAfterAnswerStarts(t *testing.T) {
	t.Parallel()

	batch := []provider.StreamResponse{{TextDelta: "answering now"}}
	batch = append(batch, reasoningBurst()...)
	batch = append(batch, provider.StreamResponse{Done: true})

	gen := &fakeGenerator{batches: [][]provider.StreamResponse{batch}}
	r := localBudgetRunner(t, gen, `{"thinkingBudgetTokens":1,"hardThinkingBudget":true}`)

	events := collectEvents(t, mustRunTurn(t, r, "solve it"))
	for _, ev := range events {
		if ev.Type == ui.StreamThinkingBudget {
			t.Fatal("a round that already produced answer text must not be cut")
		}
	}
	if got := gen.calls(); got != 1 {
		t.Errorf("generator calls = %d, want 1 (no retry)", got)
	}
}

// TestThinkingBudgetCutDoesNotConsumeToolRound is the Bugbot finding: a cut
// is not a completed tool round, so it must not advance the maxToolRounds
// counter. With the cap at 1 the retry would otherwise never run and the
// turn would end with "max tool rounds exceeded".
func TestThinkingBudgetCutDoesNotConsumeToolRound(t *testing.T) {
	t.Parallel()

	one := 1
	gen := &fakeGenerator{batches: [][]provider.StreamResponse{
		reasoningBurst(),
		{{TextDelta: "done thinking, here is the answer"}, {Done: true}},
	}}
	r, err := NewRunner(RunnerConfig{
		Generator: gen,
		Model:     "qwen3",
		WorkDir:   t.TempDir(),
		Settings: &config.Settings{Providers: &config.ProvidersSettings{
			Active: "local",
			Custom: map[string]config.CustomProviderDefinition{
				"local": {WireFormat: config.WireFormatOpenAIChat},
			},
			Extra: map[string]json.RawMessage{
				"local": json.RawMessage(`{"models":{"qwen3":{"thinkingBudgetTokens":1,"hardThinkingBudget":true}}}`),
			},
		}},
		MaxToolRoundsOverride: &one,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	events := collectEvents(t, mustRunTurn(t, r, "solve it"))

	for _, ev := range events {
		if ev.Type == ui.StreamError && strings.Contains(ev.Text, "max tool rounds exceeded") {
			t.Fatal("a budget cut consumed the only tool-round slot; the retry never ran")
		}
	}
	var notices, answers int
	for _, ev := range events {
		switch ev.Type {
		case ui.StreamThinkingBudget:
			notices++
		case ui.StreamTextDelta:
			if strings.Contains(ev.Text, "here is the answer") {
				answers++
			}
		}
	}
	if notices != 1 {
		t.Errorf("StreamThinkingBudget events = %d, want 1", notices)
	}
	if answers != 1 {
		t.Errorf("answer deltas = %d, want 1 (the retry must still produce a reply)", answers)
	}
	if got := gen.calls(); got != 2 {
		t.Fatalf("generator calls = %d, want 2 (cut round plus retry, same slot)", got)
	}
}

// TestThinkingBudgetIgnoredWithoutHardFlag asserts the client-side cut is
// strictly opt-in: an advertised budget alone never truncates a stream.
func TestThinkingBudgetIgnoredWithoutHardFlag(t *testing.T) {
	t.Parallel()

	batch := append(reasoningBurst(), provider.StreamResponse{TextDelta: "answer"}, provider.StreamResponse{Done: true})
	gen := &fakeGenerator{batches: [][]provider.StreamResponse{batch}}
	r := localBudgetRunner(t, gen, `{"thinkingBudgetTokens":1}`)

	events := collectEvents(t, mustRunTurn(t, r, "solve it"))
	for _, ev := range events {
		if ev.Type == ui.StreamThinkingBudget {
			t.Fatal("no hardThinkingBudget, so nothing may be cut")
		}
	}
	if got := gen.calls(); got != 1 {
		t.Errorf("generator calls = %d, want 1", got)
	}
}
