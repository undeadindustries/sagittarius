package contextmgmt

import (
	"context"
	"os"
	"strings"
	"testing"
)

func msg(role string, text string) Message {
	r := RoleUser
	if role == "model" {
		r = RoleModel
	}
	return Message{Role: r, Parts: []Part{{Text: text}}}
}

func fcMsg(name string) Message {
	return Message{Role: RoleModel, Parts: []Part{{FunctionCall: &ToolCall{Name: name, Args: map[string]any{}}}}}
}

func grepMsg(content string) Message {
	return Message{Role: RoleUser, Parts: []Part{{FunctionResponse: &FunctionResponse{
		Name: "grep", Response: map[string]any{"content": content}},
	}}}
}

func TestFindCompressSplitPoint(t *testing.T) {
	t.Parallel()

	five := []Message{
		msg("user", "This is the first message."),
		msg("model", "This is the second message."),
		msg("user", "This is the third message."),
		msg("model", "This is the fourth message."),
		msg("user", "This is the fifth message."),
	}
	four := []Message{
		msg("user", "This is the first message."),
		msg("model", "This is the second message."),
		msg("user", "This is the third message."),
		msg("model", "This is the fourth message."),
	}
	withFC := []Message{
		msg("user", "This is the first message."),
		msg("model", "This is the second message."),
		msg("user", "This is the third message."),
		fcMsg("foo"),
	}

	tests := []struct {
		name     string
		contents []Message
		fraction float64
		want     int
		wantErr  bool
	}{
		{name: "fraction zero errors", contents: nil, fraction: 0, wantErr: true},
		{name: "fraction one errors", contents: nil, fraction: 1, wantErr: true},
		{name: "empty history", contents: []Message{}, fraction: 0.5, want: 0},
		{name: "middle fraction", contents: five, fraction: 0.5, want: 4},
		{name: "last index fraction", contents: five, fraction: 0.9, want: 4},
		{name: "after last index", contents: four, fraction: 0.8, want: 4},
		{name: "earlier splitpoint when trailing function call", contents: withFC, fraction: 0.99, want: 2},
		{name: "single item", contents: []Message{msg("user", "Message 1")}, fraction: 0.5, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := FindCompressSplitPoint(tt.contents, tt.fraction)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("split = %d, want %d", got, tt.want)
			}
		})
	}
}

type queuedSummarizer struct {
	responses []string
	calls     [][]Message
}

func (q *queuedSummarizer) fn(_ context.Context, contents []Message, _ string) (string, error) {
	q.calls = append(q.calls, contents)
	if len(q.responses) == 0 {
		return "", nil
	}
	r := q.responses[0]
	q.responses = q.responses[1:]
	return r, nil
}

func newCompressor(q *queuedSummarizer) *Compressor {
	return &Compressor{Summarize: q.fn, CompressionPrompt: DefaultCompressionPrompt}
}

func TestCompressNoOpEmptyHistory(t *testing.T) {
	t.Parallel()
	q := &queuedSummarizer{responses: []string{"x", "y"}}
	res, err := newCompressor(q).Compress(context.Background(), CompressOptions{
		History: nil, Threshold: 0.5, EffectiveLimit: 1000,
	})
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}
	if res.Info.Status != CompressionNoOp || res.NewHistory != nil {
		t.Errorf("status = %v, history = %v, want NoOp/nil", res.Info.Status, res.NewHistory)
	}
}

func TestCompressNoOpUnderThreshold(t *testing.T) {
	t.Parallel()
	q := &queuedSummarizer{responses: []string{"x", "y"}}
	res, err := newCompressor(q).Compress(context.Background(), CompressOptions{
		History:            []Message{msg("user", "hi")},
		OriginalTokenCount: 600,
		Threshold:          0.7,
		EffectiveLimit:     1000,
	})
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}
	if res.Info.Status != CompressionNoOp || res.NewHistory != nil {
		t.Errorf("status = %v, want NoOp", res.Info.Status)
	}
}

func TestCompressOverThresholdWithVerification(t *testing.T) {
	t.Parallel()
	q := &queuedSummarizer{responses: []string{"Initial Summary", "Verified Summary"}}
	history := []Message{
		msg("user", "msg1"), msg("model", "msg2"), msg("user", "msg3"), msg("model", "msg4"),
	}
	res, err := newCompressor(q).Compress(context.Background(), CompressOptions{
		History:            history,
		OriginalTokenCount: 600_000,
		Threshold:          0.5,
		EffectiveLimit:     1_000_000,
		PreserveFraction:   0.3,
	})
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}
	if res.Info.Status != Compressed {
		t.Fatalf("status = %v, want Compressed", res.Info.Status)
	}
	if res.NewHistory[0].Parts[0].Text != "Verified Summary" {
		t.Errorf("summary = %q, want Verified Summary", res.NewHistory[0].Parts[0].Text)
	}
	if len(q.calls) != 2 {
		t.Errorf("summarizer calls = %d, want 2", len(q.calls))
	}
}

func TestCompressFallsBackToInitialSummary(t *testing.T) {
	t.Parallel()
	q := &queuedSummarizer{responses: []string{"Initial Summary", "   "}}
	history := []Message{msg("user", "msg1"), msg("model", "msg2")}
	res, err := newCompressor(q).Compress(context.Background(), CompressOptions{
		History: history, OriginalTokenCount: 600_000, Threshold: 0.5, EffectiveLimit: 1_000_000, PreserveFraction: 0.3,
	})
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}
	if res.Info.Status != Compressed {
		t.Fatalf("status = %v, want Compressed", res.Info.Status)
	}
	if res.NewHistory[0].Parts[0].Text != "Initial Summary" {
		t.Errorf("summary = %q, want Initial Summary", res.NewHistory[0].Parts[0].Text)
	}
}

func TestCompressAnchoredInstructionWithSnapshot(t *testing.T) {
	t.Parallel()
	q := &queuedSummarizer{responses: []string{"Initial", "Verified"}}
	history := []Message{
		msg("user", "<state_snapshot>old</state_snapshot>"),
		msg("model", "msg2"), msg("user", "msg3"), msg("model", "msg4"),
	}
	_, err := newCompressor(q).Compress(context.Background(), CompressOptions{
		History: history, OriginalTokenCount: 800, Threshold: 0.5, EffectiveLimit: 1000, PreserveFraction: 0.3,
	})
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}
	first := q.calls[0]
	last := first[len(first)-1]
	if !strings.Contains(last.Parts[0].Text, "A previous <state_snapshot> exists") {
		t.Errorf("anchored instruction missing; got %q", last.Parts[0].Text)
	}
}

func TestCompressForceUnderThreshold(t *testing.T) {
	t.Parallel()
	q := &queuedSummarizer{responses: []string{"Initial", "Verified"}}
	history := []Message{
		msg("user", "msg1"), msg("model", "msg2"), msg("user", "msg3"), msg("model", "msg4"),
	}
	res, err := newCompressor(q).Compress(context.Background(), CompressOptions{
		History: history, Force: true, OriginalTokenCount: 100, Threshold: 0.5, EffectiveLimit: 1_000_000, PreserveFraction: 0.3,
	})
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}
	if res.Info.Status != Compressed {
		t.Errorf("status = %v, want Compressed", res.Info.Status)
	}
}

func TestCompressFailsOnInflatedTokenCount(t *testing.T) {
	t.Parallel()
	q := &queuedSummarizer{responses: []string{"Initial", strings.Repeat("a", 1000)}}
	c := newCompressor(q)
	c.CountRequestTokens = func([]Part) int { return 10_000 }
	history := []Message{msg("user", "msg1"), msg("model", "msg2")}
	res, err := c.Compress(context.Background(), CompressOptions{
		History: history, Force: true, OriginalTokenCount: 100, Threshold: 0.5, EffectiveLimit: 1_000_000, PreserveFraction: 0.3,
	})
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}
	if res.Info.Status != CompressionFailedInflatedTokenCount || res.NewHistory != nil {
		t.Errorf("status = %v, want FailedInflated", res.Info.Status)
	}
}

func TestCompressFailsOnEmptySummary(t *testing.T) {
	t.Parallel()
	q := &queuedSummarizer{responses: []string{"   ", "   "}}
	history := []Message{msg("user", "msg1"), msg("model", "msg2")}
	res, err := newCompressor(q).Compress(context.Background(), CompressOptions{
		History: history, OriginalTokenCount: 800, Threshold: 0.5, EffectiveLimit: 1000, PreserveFraction: 0.3,
	})
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}
	if res.Info.Status != CompressionFailedEmptySummary || res.NewHistory != nil {
		t.Errorf("status = %v, want FailedEmptySummary", res.Info.Status)
	}
}

func TestCompressTruncatesOversizedToolResponses(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	q := &queuedSummarizer{responses: []string{"Initial", "Verified"}}
	c := newCompressor(q)
	c.OutputDir = dir
	c.TruncateThreshold = DefaultTruncateToolOutputThreshold

	large := strings.Repeat("a", 170_000)
	history := []Message{
		msg("user", "old msg"),
		msg("model", "old resp"),
		msg("user", "msg 1"),
		grepMsg(large),
		msg("model", "resp 2"),
		grepMsg(large),
	}
	res, err := c.Compress(context.Background(), CompressOptions{
		History: history, Force: true, OriginalTokenCount: 600_000, Threshold: 0.5, EffectiveLimit: 1_000_000, PreserveFraction: 0.3,
	})
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}
	if res.Info.Status != Compressed {
		t.Fatalf("status = %v, want Compressed", res.Info.Status)
	}

	if !historyContains(res.NewHistory, "Output too large.") {
		t.Errorf("expected truncation marker in compressed history")
	}
	if !historyContains(res.NewHistory, "Showing first 8,000 and last 32,000 characters") {
		t.Errorf("expected head/tail character notice")
	}
	entries, derr := os.ReadDir(dirJoin(dir))
	if derr != nil || len(entries) == 0 {
		t.Errorf("expected offload file in %s (err=%v, files=%d)", dirJoin(dir), derr, len(entries))
	}
}

func historyContains(history []Message, substr string) bool {
	for i := range history {
		for j := range history[i].Parts {
			if history[i].Parts[j].FunctionResponse != nil {
				if v, ok := history[i].Parts[j].FunctionResponse.Response["output"].(string); ok && strings.Contains(v, substr) {
					return true
				}
			}
			if strings.Contains(history[i].Parts[j].Text, substr) {
				return true
			}
		}
	}
	return false
}

func dirJoin(base string) string {
	return base + string(os.PathSeparator) + ToolOutputsDir
}
