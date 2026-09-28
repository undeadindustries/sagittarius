package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/modes"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

// TestSubagentPromptSkipsMentionExpansion is the benchmark failure: a parent
// writing a test-suite prompt includes a fenced code sample with the
// @pytest.fixture decorator, and the child's turn used to abort with
// "@pytest.fixture: no such file" before running a single tool call. Subagent
// prompts are model-generated, so composer @-mention expansion must not run.
func TestSubagentPromptSkipsMentionExpansion(t *testing.T) {
	t.Parallel()

	h := newSubagentHarness(t, openAISettingsWithModelPins(nil))
	// Give the child a generator that actually answers: an empty stream is a
	// StreamError (AD-126), which would read as a turn failure and mask the
	// mention behavior under test.
	h.parent.newSubagentGenerator = func(context.Context, *config.Settings) (provider.ContentGenerator, error) {
		return &fakeGenerator{batches: [][]provider.StreamResponse{{{TextDelta: "ok", Done: true}}}}, nil
	}
	child, err := h.parent.newSubagent(t.Context(), subagentSpec{
		description: "mention-crash",
		mode:        modes.ModeAsk,
		class:       config.SubagentResearch,
		approval:    ApprovalYolo,
	})
	if err != nil {
		t.Fatalf("newSubagent: %v", err)
	}

	prompt := "Create tests with a fixture:\n```python\n@pytest.fixture\ndef conn():\n    pass\n```\n"
	text, err := child.run(t.Context(), prompt, func(string) {})
	if err != nil {
		t.Fatalf("child turn must survive a decorator in the prompt: %v", err)
	}
	if text != "ok" {
		t.Errorf("final text = %q, want ok", text)
	}
}

// TestParentPromptStillExpandsMentions guards the other direction: the
// composer's @path affordance must keep working (and failing) for real users.
func TestParentPromptStillExpandsMentions(t *testing.T) {
	t.Parallel()

	h := newSubagentHarness(t, openAISettingsWithModelPins(nil))
	stream, err := h.parent.RunTurn(t.Context(), "read @no/such/file.txt please")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	var sawErr bool
	for ev := range stream {
		if ev.Type == ui.StreamError && ev.Err != nil && strings.Contains(ev.Err.Error(), "no such file") {
			sawErr = true
		}
	}
	if !sawErr {
		t.Error("parent turn with a bad @mention must still fail with the mention error")
	}
}
