package contextmgmt

import (
	"context"
	"strings"
	"testing"
)

func TestExtractSnapshot(t *testing.T) {
	t.Parallel()

	const snap = "<state_snapshot>\n<overall_goal>ship it</overall_goal>\n</state_snapshot>"

	tests := []struct {
		name string
		raw  string
		want string
	}{
		// The reported case: 28% of a real stored summary was scratchpad.
		{"scratchpad before snapshot", "<scratchpad>\nlet me re-examine\n</scratchpad>\n\n" + snap, snap},
		{"think tags", "<think>reasoning</think>\n" + snap, snap},
		{"nested reasoning tag names", "<thinking>a</thinking><reasoning>b</reasoning>" + snap, snap},
		{"trailing commentary after snapshot", snap + "\n\nHope that helps!", snap},
		{"snapshot only", snap, snap},

		// Reasoning-only output must fail closed so Compress preserves history
		// rather than replacing it with a reasoning trace.
		{"scratchpad only", "<scratchpad>\nthinking out loud\n</scratchpad>", ""},
		{"unclosed scratchpad only", "<scratchpad>\nran out of budget mid-thought", ""},
		{"empty", "", ""},
		{"whitespace", "   \n  ", ""},

		// A model that ignores the envelope can still return a usable summary.
		{"prose summary without tags", "The user wants X. Files touched: a.go.", "The user wants X. Files touched: a.go."},
		{"prose summary after scratchpad", "<scratchpad>hmm</scratchpad>\nThe user wants X.", "The user wants X."},
		{"stray closing tag with no opener", "reasoning spillage</think>\nThe user wants X.", "The user wants X."},

		// Truncation mid-snapshot keeps the dense leading sections.
		{"unclosed snapshot", "<scratchpad>x</scratchpad>\n<state_snapshot>\n<overall_goal>ship", "<state_snapshot>\n<overall_goal>ship"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := extractSnapshot(tc.raw); got != tc.want {
				t.Errorf("extractSnapshot(%q):\n got: %q\nwant: %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestSummarizeDropsScratchpadBeforeVerification pins the token saving on both
// sides of the verification pass: the scratchpad must not be stored, and it must
// not be replayed into the second summarizer call either.
func TestSummarizeDropsScratchpadBeforeVerification(t *testing.T) {
	t.Parallel()

	const scratch = "<scratchpad>NINE-KILOBYTES-OF-REASONING</scratchpad>"
	q := &queuedSummarizer{responses: []string{
		scratch + "\n<state_snapshot>first</state_snapshot>",
		scratch + "\n<state_snapshot>final</state_snapshot>",
	}}

	res, err := newCompressor(q).Compress(context.Background(), CompressOptions{
		History:            []Message{msg("user", "msg1"), msg("model", "msg2"), msg("user", "msg3"), msg("model", "msg4")},
		OriginalTokenCount: 600_000,
		Threshold:          0.5,
		EffectiveLimit:     1_000_000,
		PreserveFraction:   0.3,
	})
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}

	stored := res.NewHistory[0].Parts[0].Text
	if !strings.Contains(stored, "<state_snapshot>final</state_snapshot>") {
		t.Errorf("stored summary lost the snapshot: %q", stored)
	}
	if strings.Contains(stored, "NINE-KILOBYTES-OF-REASONING") {
		t.Errorf("scratchpad was stored in history: %q", stored)
	}

	if len(q.calls) != 2 {
		t.Fatalf("summarizer calls = %d, want 2", len(q.calls))
	}
	for _, m := range q.calls[1] {
		for _, p := range m.Parts {
			if strings.Contains(p.Text, "NINE-KILOBYTES-OF-REASONING") {
				t.Error("scratchpad was replayed into the verification request")
			}
		}
	}
}

// TestCompressFailsOnReasoningOnlySummary is the data-loss class OpenCode still
// carries (their #44080/#41571): a thinking model burns its output budget on
// reasoning, and the summary that replaces the conversation holds no summary at
// all. Extraction turns it into an empty summary, which fails closed.
func TestCompressFailsOnReasoningOnlySummary(t *testing.T) {
	t.Parallel()

	q := &queuedSummarizer{responses: []string{
		"<scratchpad>I should start by reviewing the goal</scratchpad>",
		"<scratchpad>still reasoning</scratchpad>",
	}}
	res, err := newCompressor(q).Compress(context.Background(), CompressOptions{
		History:            []Message{msg("user", "msg1"), msg("model", "msg2")},
		OriginalTokenCount: 600_000,
		Threshold:          0.5,
		EffectiveLimit:     1_000_000,
		PreserveFraction:   0.3,
	})
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}
	if res.Info.Status != CompressionFailedEmptySummary {
		t.Errorf("status = %v, want CompressionFailedEmptySummary", res.Info.Status)
	}
	if res.NewHistory != nil {
		t.Error("history was replaced by a reasoning-only summary")
	}
}

// TestCompressFramesSummaryAsReference covers the mimicry fix: the injected
// summary occupies the user slot, so it needs a reference-only label and a
// closing boundary or the model reads it as its own output format.
func TestCompressFramesSummaryAsReference(t *testing.T) {
	t.Parallel()

	q := &queuedSummarizer{responses: []string{
		"<state_snapshot>first</state_snapshot>",
		"<state_snapshot>final</state_snapshot>",
	}}
	res, err := newCompressor(q).Compress(context.Background(), CompressOptions{
		History:            []Message{msg("user", "msg1"), msg("model", "msg2"), msg("user", "msg3"), msg("model", "msg4")},
		OriginalTokenCount: 600_000,
		Threshold:          0.5,
		EffectiveLimit:     1_000_000,
		PreserveFraction:   0.3,
	})
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}

	framed := res.NewHistory[0].Parts[0].Text
	if !strings.HasPrefix(framed, compressedSummaryPrefix) {
		t.Errorf("summary is missing the reference-only label: %q", framed)
	}
	if !strings.HasSuffix(framed, compressedSummaryEndMarker) {
		t.Errorf("summary is missing the end marker: %q", framed)
	}
	if !strings.Contains(framed, "never reproduce this snapshot format") {
		t.Errorf("framing must forbid emitting the format: %q", framed)
	}

	// The anchored-update path keys off the tag, so framing must not hide it
	// from a later compression of this same history.
	if !historyHasSnapshot(res.NewHistory) {
		t.Error("framed summary is no longer detectable as a snapshot")
	}
}
