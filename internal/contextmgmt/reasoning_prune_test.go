package contextmgmt

import (
	"strings"
	"testing"
)

func TestPruneReasoningProtectsActiveTurnAndBatchesOlderTurns(t *testing.T) {
	t.Parallel()

	think := func(nChars int) string { return strings.Repeat("t", nChars) }
	// 400 chars = 100 tokens at 4 chars/token.
	history := []Message{
		{Role: RoleUser, Parts: []Part{{Text: "turn 1"}}},
		{Role: RoleModel, Parts: []Part{{Reasoning: think(400), Text: "ans 1"}}}, // 100 tok (unprotected #2)
		{Role: RoleUser, Parts: []Part{{Text: "turn 2"}}},
		{Role: RoleModel, Parts: []Part{{Reasoning: think(400), Text: "ans 2"}}}, // 100 tok (unprotected #1)
		{Role: RoleUser, Parts: []Part{{Text: "turn 3"}}},
		{Role: RoleModel, Parts: []Part{{Reasoning: think(400), Text: "ans 3"}}}, // 100 tok (inside 120-tok protection window)
		{Role: RoleUser, Parts: []Part{{Text: "turn 4 (active)"}}},
		{Role: RoleModel, Parts: []Part{{
			Reasoning:    think(800), // 200 tok inside active tool loop: MUST be protected
			FunctionCall: &ToolCall{ID: "c1", Name: "read_file", Args: map[string]any{"path": "a.go"}},
		}}},
		{Role: RoleUser, Parts: []Part{{
			FunctionResponse: &FunctionResponse{Name: "read_file", CallID: "c1", Response: map[string]any{"output": "ok"}},
		}}},
	}

	// Below MinPrunableThresholdTokens (250 > 200): no-op, zero mutations.
	noopRes := PruneReasoning(history, ReasoningPruneConfig{
		ProtectionThresholdTokens:  120,
		MinPrunableThresholdTokens: 250,
	}, EstimateTokens)
	if noopRes.PrunedCount != 0 || noopRes.TokensSaved != 0 {
		t.Fatalf("below threshold: PrunedCount=%d TokensSaved=%d, want 0/0", noopRes.PrunedCount, noopRes.TokensSaved)
	}
	if noopRes.NewHistory[1].Parts[0].Reasoning == "" || noopRes.NewHistory[3].Parts[0].Reasoning == "" {
		t.Fatal("below threshold mutated history")
	}

	// At MinPrunableThresholdTokens (180 <= 200): both unprotected turns pruned in one batch.
	res := PruneReasoning(history, ReasoningPruneConfig{
		ProtectionThresholdTokens:  120,
		MinPrunableThresholdTokens: 180,
	}, EstimateTokens)
	if res.PrunedCount != 2 {
		t.Fatalf("PrunedCount = %d, want 2", res.PrunedCount)
	}
	if res.TokensSaved != 200 {
		t.Fatalf("TokensSaved = %d, want 200", res.TokensSaved)
	}
	// Original slice not mutated.
	if history[1].Parts[0].Reasoning == "" {
		t.Fatal("input history slice was mutated in place")
	}
	// Turn 1 and Turn 2 reasoning cleared; Turn 3 (protected window) and Turn 4 (active tool loop) kept.
	if res.NewHistory[1].Parts[0].Reasoning != "" || res.NewHistory[3].Parts[0].Reasoning != "" {
		t.Fatal("expected turns 1 and 2 reasoning to be cleared")
	}
	if res.NewHistory[5].Parts[0].Reasoning == "" {
		t.Fatal("expected turn 3 reasoning inside protection window to be preserved")
	}
	if res.NewHistory[7].Parts[0].Reasoning == "" {
		t.Fatal("expected active tool-loop reasoning after latest user message to be preserved")
	}
}

func TestStripAllReasoning(t *testing.T) {
	t.Parallel()

	call := &ToolCall{ID: "c1", Name: "read_file", Args: map[string]any{"path": "a.go"}}
	tests := []struct {
		name         string
		history      []Message
		wantStripped int
		wantParts    []int
	}{
		{
			name:         "empty history",
			history:      nil,
			wantStripped: 0,
			wantParts:    nil,
		},
		{
			name: "no reasoning returns input unchanged",
			history: []Message{
				{Role: RoleUser, Parts: []Part{{Text: "u1"}}},
				{Role: RoleModel, Parts: []Part{{Text: "a1"}}},
			},
			wantStripped: 0,
			wantParts:    []int{1, 1},
		},
		{
			name: "strips every model turn including the active tool loop",
			history: []Message{
				{Role: RoleUser, Parts: []Part{{Text: "u1"}}},
				{Role: RoleModel, Parts: []Part{{Reasoning: "old", Text: "a1"}}},
				{Role: RoleUser, Parts: []Part{{Text: "u2"}}},
				{Role: RoleModel, Parts: []Part{{Reasoning: "active", Text: "calling"}, {FunctionCall: call}}},
			},
			wantStripped: 2,
			wantParts:    []int{1, 1, 1, 2},
		},
		{
			name: "reasoning-only part is dropped but message survives",
			history: []Message{
				{Role: RoleUser, Parts: []Part{{Text: "u1"}}},
				{Role: RoleModel, Parts: []Part{{Reasoning: "only thinking"}, {FunctionCall: call}}},
			},
			wantStripped: 1,
			wantParts:    []int{1, 1},
		},
		{
			name: "user reasoning field is left alone",
			history: []Message{
				{Role: RoleUser, Parts: []Part{{Text: "u1", Reasoning: "not a model turn"}}},
			},
			wantStripped: 0,
			wantParts:    []int{1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			before := EstimateTokens(flattenParts(tt.history))
			got, n := StripAllReasoning(tt.history)
			if n != tt.wantStripped {
				t.Fatalf("stripped = %d, want %d", n, tt.wantStripped)
			}
			if len(got) != len(tt.wantParts) {
				t.Fatalf("len(history) = %d, want %d", len(got), len(tt.wantParts))
			}
			for i := range got {
				if len(got[i].Parts) != tt.wantParts[i] {
					t.Errorf("msg[%d] parts = %d, want %d", i, len(got[i].Parts), tt.wantParts[i])
				}
				if got[i].Role == RoleModel {
					for _, p := range got[i].Parts {
						if p.Reasoning != "" {
							t.Errorf("msg[%d] still carries reasoning %q", i, p.Reasoning)
						}
					}
				}
			}
			if tt.wantStripped > 0 {
				if after := EstimateTokens(flattenParts(got)); after >= before {
					t.Errorf("token estimate %d -> %d, want a reduction", before, after)
				}
				if EstimateTokens(flattenParts(tt.history)) != before {
					t.Error("input history was mutated in place")
				}
			}
		})
	}
}

func TestBuildCompressedHistoryStripsReasoningBeforeLastUserInPreservedTail(t *testing.T) {
	t.Parallel()

	keep := []Message{
		{Role: RoleUser, Parts: []Part{{Text: "earlier kept user turn"}}},
		{Role: RoleModel, Parts: []Part{{Reasoning: "old kept thinking", Text: "earlier answer"}}},
		{Role: RoleUser, Parts: []Part{{Text: "latest user turn"}}},
		{Role: RoleModel, Parts: []Part{
			{Reasoning: "active turn thinking", Text: "calling tool"},
			{FunctionCall: &ToolCall{ID: "c1", Name: "read_file", Args: map[string]any{"path": "x.go"}}},
		}},
		{Role: RoleUser, Parts: []Part{
			{FunctionResponse: &FunctionResponse{Name: "read_file", CallID: "c1", Response: map[string]any{"output": "ok"}}},
		}},
	}

	compressed := buildCompressedHistory("<state_snapshot>summary</state_snapshot>", keep)
	// compressed[0] = summary user message, compressed[1] = model ack
	// compressed[2] = keep[0] (earlier user), compressed[3] = keep[1] (earlier model -> reasoning stripped)
	// compressed[4] = keep[2] (latest user), compressed[5] = keep[3] (active model -> reasoning preserved)
	if compressed[3].Parts[0].Reasoning != "" {
		t.Errorf("expected reasoning before latest user message to be stripped, got %q", compressed[3].Parts[0].Reasoning)
	}
	if compressed[5].Parts[0].Reasoning != "active turn thinking" {
		t.Errorf("expected reasoning after latest user message to be preserved, got %q", compressed[5].Parts[0].Reasoning)
	}
}
