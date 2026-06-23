package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

// TestInitForwardsSubmitPrompt verifies that /init creates AGENTS.md and that
// handleSlash forwards the submit-prompt turn into a single merged stream that
// terminates with exactly one StreamDone.
func TestInitForwardsSubmitPrompt(t *testing.T) {
	t.Parallel()

	gen := &fakeGenerator{batches: [][]provider.StreamResponse{{{TextDelta: "done"}, {Done: true}}}}
	dir := t.TempDir()
	runner, err := NewRunner(RunnerConfig{
		Generator:   gen,
		Model:       "test-model",
		WorkDir:     dir,
		Interactive: false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	app := NewApp(AppConfig{Runner: runner})
	events, err := app.HandleInput(testContext(t), "/init")
	if err != nil {
		t.Fatalf("HandleInput: %v", err)
	}
	got := collectEvents(t, events)

	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); err != nil {
		t.Fatalf("AGENTS.md not created: %v", err)
	}

	var sawAnalyzing, sawDelta bool
	doneCount := 0
	for _, ev := range got {
		switch ev.Type {
		case ui.StreamInfo:
			if strings.Contains(ev.Text, "Analyzing") {
				sawAnalyzing = true
			}
		case ui.StreamTextDelta:
			if strings.Contains(ev.Text, "done") {
				sawDelta = true
			}
		case ui.StreamDone:
			doneCount++
		}
	}

	if !sawAnalyzing {
		t.Error("missing StreamInfo with 'Analyzing'")
	}
	if !sawDelta {
		t.Error("missing forwarded StreamTextDelta 'done'")
	}
	if doneCount != 1 {
		t.Errorf("StreamDone count = %d, want 1", doneCount)
	}
	if len(got) == 0 || got[len(got)-1].Type != ui.StreamDone {
		t.Errorf("last event is not StreamDone: %+v", got)
	}
}
