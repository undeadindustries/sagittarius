package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/tools"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

// TestEmptyReplyAfterToolWork covers a model that does its tool work and then
// sends an empty closing reply, which Gemini does routinely. That must not be
// reported as a context-window error: the runner asks once for a summary and,
// if the model stays silent, ends the turn normally with a note.
func TestEmptyReplyAfterToolWork(t *testing.T) {
	t.Parallel()

	readRound := []provider.StreamResponse{
		{ToolCalls: []provider.ToolCall{{
			Name: tools.ReadFileToolName,
			Args: map[string]any{tools.ParamFilePath: "data.txt"},
		}}},
		{Done: true},
	}
	empty := []provider.StreamResponse{{Done: true}}

	tests := []struct {
		name       string
		batches    [][]provider.StreamResponse
		wantText   string
		wantNotice bool
		wantCalls  int
	}{
		{
			name:      "nudge produces the summary",
			batches:   [][]provider.StreamResponse{readRound, empty, {{TextDelta: "read data.txt"}, {Done: true}}},
			wantText:  "read data.txt",
			wantCalls: 3,
		},
		{
			name:       "silent after nudge ends with a notice",
			batches:    [][]provider.StreamResponse{readRound, empty, empty},
			wantNotice: true,
			wantCalls:  3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "data.txt"), []byte("payload"), 0o644); err != nil {
				t.Fatal(err)
			}
			gen := &fakeGenerator{batches: tt.batches}
			runner, err := NewRunner(RunnerConfig{
				Generator:    gen,
				Model:        "test-model",
				WorkDir:      root,
				ApprovalMode: ApprovalYolo,
				Interactive:  false,
			})
			if err != nil {
				t.Fatalf("NewRunner: %v", err)
			}
			events, err := runner.RunTurn(testContext(t), "read data.txt")
			if err != nil {
				t.Fatalf("RunTurn: %v", err)
			}
			var text strings.Builder
			notice, done := false, false
			for _, ev := range collectEvents(t, events) {
				switch ev.Type {
				case ui.StreamError:
					t.Fatalf("unexpected error event: %q %v", ev.Text, ev.Err)
				case ui.StreamTextDelta:
					text.WriteString(ev.Text)
				case ui.StreamInfo:
					if ev.Text == emptySummaryNotice {
						notice = true
					}
				case ui.StreamDone:
					done = true
				}
			}
			if !done {
				t.Fatal("turn ended without StreamDone")
			}
			if notice != tt.wantNotice {
				t.Fatalf("notice = %v, want %v", notice, tt.wantNotice)
			}
			if tt.wantText != "" && !strings.Contains(text.String(), tt.wantText) {
				t.Fatalf("text = %q, want it to contain %q", text.String(), tt.wantText)
			}
			if got := gen.calls(); got != tt.wantCalls {
				t.Fatalf("generator calls = %d, want %d (one nudge at most)", got, tt.wantCalls)
			}
			last := gen.lastRequest().Messages
			if final := last[len(last)-1]; final.Role != provider.RoleUser || final.Parts[0].Text != emptySummaryNudge {
				t.Fatalf("last request ends on %+v, want the summary nudge", final)
			}
		})
	}
}
