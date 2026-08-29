package contextmgmt

import (
	"strings"
	"testing"
)

func TestSplitIntoBudgetedChunksEmpty(t *testing.T) {
	t.Parallel()
	if got := splitIntoBudgetedChunks(nil, 100, EstimateTokens); got != nil {
		t.Fatalf("empty history = %#v, want nil", got)
	}
}

func TestSplitIntoBudgetedChunksFitsInOne(t *testing.T) {
	t.Parallel()
	history := []Message{
		msg("user", "hello"),
		msg("model", "hi"),
	}
	got := splitIntoBudgetedChunks(history, 10_000, EstimateTokens)
	if len(got) != 1 {
		t.Fatalf("chunks = %d, want 1", len(got))
	}
	if len(got[0]) != 2 {
		t.Fatalf("chunk[0] len = %d, want 2", len(got[0]))
	}
}

func TestSplitIntoBudgetedChunksSafeBoundaries(t *testing.T) {
	t.Parallel()
	// Each pair is ~60 tokens (240 ASCII chars / 4). Budget 80 forces a cut
	// between pairs, which is a user-turn boundary.
	pair := func(n string) []Message {
		body := strings.Repeat(n+" ", 40)
		return []Message{msg("user", body), msg("model", body)}
	}
	history := append(pair("a"), pair("b")...)
	history = append(history, pair("c")...)
	budget := 80
	got := splitIntoBudgetedChunks(history, budget, EstimateTokens)
	if len(got) < 2 {
		t.Fatalf("chunks = %d, want at least 2", len(got))
	}
	for i, chunk := range got {
		tok := EstimateTokens(flattenParts(chunk))
		if tok > budget && len(chunk) > 1 {
			t.Errorf("chunk %d tokens = %d, want <= %d", i, tok, budget)
		}
		if chunk[0].Role != RoleUser {
			t.Errorf("chunk %d starts with role %q, want user", i, chunk[0].Role)
		}
	}
}

func TestSplitIntoBudgetedChunksKeepsToolPairs(t *testing.T) {
	t.Parallel()
	history := []Message{
		msg("user", strings.Repeat("ask ", 40)),
		fcMsg("read_file"),
		{Role: RoleUser, Parts: []Part{{FunctionResponse: &FunctionResponse{
			Name: "read_file", CallID: "c1", Response: map[string]any{"content": strings.Repeat("out ", 40)},
		}}}},
		msg("user", strings.Repeat("next ", 40)),
		msg("model", strings.Repeat("ok ", 40)),
	}
	got := splitIntoBudgetedChunks(history, 80, EstimateTokens)
	for _, chunk := range got {
		assertToolPairsIntact(t, chunk)
	}
}

func TestSplitIntoBudgetedChunksToolPairOvershootBoundary(t *testing.T) {
	t.Parallel()
	// Test the tool pair no-flush boundary fix:
	// Msg 0: user text (~30 tokens)
	// Msg 1: model tool call (~20 tokens)
	// Msg 2: user tool response (~40 tokens)
	// Budget: 60 tokens.
	// Acc at Msg 1 is 50 <= 60. At Msg 2, acc+tokens = 90 > 60.
	// The chunker must flush before the tool call (flush(i-1)), keeping the
	// tool call and tool response together in the next chunk without splitting.
	history := []Message{
		msg("user", strings.Repeat("prompt ", 20)), // ~30 tokens
		fcMsg("read_file"),                         // ~15 tokens
		{Role: RoleUser, Parts: []Part{{FunctionResponse: &FunctionResponse{
			Name: "read_file", CallID: "c1", Response: map[string]any{"content": strings.Repeat("data ", 30)}, // ~37 tokens
		}}}},
	}
	got := splitIntoBudgetedChunks(history, 60, EstimateTokens)
	if len(got) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(got))
	}
	if len(got[0]) != 1 || got[0][0].Role != RoleUser {
		t.Fatalf("chunk 0 should contain only the leading user message, got %+v", got[0])
	}
	if len(got[1]) != 2 {
		t.Fatalf("chunk 1 should contain the tool pair intact, got %+v", got[1])
	}
	assertToolPairsIntact(t, got[0])
	assertToolPairsIntact(t, got[1])
}

func TestSplitIntoBudgetedChunksOversizedSingleMessage(t *testing.T) {
	t.Parallel()
	huge := msg("user", strings.Repeat("x", 800)) // 200 tokens
	history := []Message{
		huge,
		msg("user", "later"),
		msg("model", "ok"),
	}
	got := splitIntoBudgetedChunks(history, 50, EstimateTokens)
	if len(got) < 2 {
		t.Fatalf("chunks = %d, want the oversized message isolated", len(got))
	}
	if len(got[0]) != 1 || got[0][0].Parts[0].Text != huge.Parts[0].Text {
		t.Fatalf("first chunk = %#v, want the single oversized message", got[0])
	}
}

func TestSplitIntoBudgetedChunksDoesNotMutateInput(t *testing.T) {
	t.Parallel()
	history := []Message{msg("user", strings.Repeat("a ", 40)), msg("model", strings.Repeat("b ", 40))}
	before := history[0].Parts[0].Text
	_ = splitIntoBudgetedChunks(history, 40, EstimateTokens)
	if history[0].Parts[0].Text != before {
		t.Fatal("input history was mutated")
	}
}

func TestSummarizerBudget(t *testing.T) {
	t.Parallel()
	want := int(1000*summarizerInputFraction) - summarizerRequestOverhead()
	if got := summarizerBudget(1000); got != want {
		t.Errorf("summarizerBudget(1000) = %d, want %d", got, want)
	}
	if got := summarizerBudget(0); got != 0 {
		t.Errorf("summarizerBudget(0) = %d, want 0", got)
	}
}

func assertToolPairsIntact(t *testing.T, chunk []Message) {
	t.Helper()
	for i := range chunk {
		if !hasFunctionCall(chunk[i]) {
			continue
		}
		if i+1 >= len(chunk) || !hasFunctionResponse(chunk[i+1]) {
			t.Fatalf("functionCall at %d has no following functionResponse in chunk %#v", i, chunk)
		}
	}
	if len(chunk) > 0 && hasFunctionResponse(chunk[0]) && (len(chunk) < 2 || !hasFunctionCall(chunk[0])) {
		// A leading FR with no preceding FC in this chunk is an orphan.
		t.Fatalf("chunk opens with an orphaned functionResponse: %#v", chunk[0])
	}
}
