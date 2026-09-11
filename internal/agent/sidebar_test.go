package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/tools"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

func TestSidebarFlushPreservesToolPairing(t *testing.T) {
	t.Parallel()
	runner, err := NewRunner(RunnerConfig{
		Generator:   &fakeGenerator{},
		Model:       "test",
		WorkDir:     t.TempDir(),
		Interactive: false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	call := provider.ToolCall{
		ID:   "call-wait-1",
		Name: tools.WaitUntilToolName,
		Args: map[string]any{tools.WaitUntilParamCommand: "test -f done"},
	}
	runner.historyMu.Lock()
	runner.history = []provider.Message{
		{Role: provider.RoleUser, Parts: []provider.Part{{Text: "start the download"}}},
		{Role: provider.RoleModel, Parts: []provider.Part{{FunctionCall: &call}}},
	}
	runner.historyMu.Unlock()

	runner.bufferSidebar("what are we waiting for?", "the tarball in /tmp/done")

	if runner.pendingSidebarCount() != 1 {
		t.Fatalf("pending = %d, want 1", runner.pendingSidebarCount())
	}
	if got := historyText(runner.History()); strings.Contains(got, "what are we waiting for?") {
		t.Fatal("inject-on-arrival: sidebar question landed in history before tool results; that breaks AD-052 pairing")
	}

	runner.appendFunctionResponses([]provider.FunctionResponse{{
		Name:   tools.WaitUntilToolName,
		CallID: call.ID,
		Response: map[string]any{
			"status": "ready",
		},
	}})
	runner.flushPendingSidebar()

	hist := runner.History()
	if len(hist) != 5 {
		t.Fatalf("history len = %d, want 5 (user, tool_calls, tool_result, sidebar user, sidebar model)", len(hist))
	}
	if hist[1].Role != provider.RoleModel || hist[1].Parts[0].FunctionCall == nil {
		t.Fatalf("hist[1] should be assistant tool_calls, got %+v", hist[1])
	}
	if hist[2].Role != provider.RoleUser || hist[2].Parts[0].FunctionResponse == nil {
		t.Fatalf("hist[2] should be tool results, got %+v", hist[2])
	}
	if hist[2].Parts[0].FunctionResponse.CallID != call.ID {
		t.Fatalf("tool result CallID = %q, want %q", hist[2].Parts[0].FunctionResponse.CallID, call.ID)
	}
	if hist[3].Role != provider.RoleUser || hist[3].Parts[0].Text != "what are we waiting for?" {
		t.Fatalf("hist[3] should be sidebar question, got %+v", hist[3])
	}
	if hist[4].Role != provider.RoleModel || hist[4].Parts[0].Text != "the tarball in /tmp/done" {
		t.Fatalf("hist[4] should be sidebar answer, got %+v", hist[4])
	}
}

func TestStripUnpairedTrailingToolCalls(t *testing.T) {
	t.Parallel()
	hist := []provider.Message{
		{Role: provider.RoleUser, Parts: []provider.Part{{Text: "hi"}}},
		{Role: provider.RoleModel, Parts: []provider.Part{{
			FunctionCall: &provider.ToolCall{ID: "1", Name: tools.WaitUntilToolName},
		}}},
	}
	got := stripUnpairedTrailingToolCalls(hist)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	complete := append(hist, provider.Message{
		Role: provider.RoleUser,
		Parts: []provider.Part{{FunctionResponse: &provider.FunctionResponse{
			Name: tools.WaitUntilToolName, CallID: "1",
		}}},
	})
	if got := stripUnpairedTrailingToolCalls(complete); len(got) != 3 {
		t.Fatalf("complete history stripped to %d, want 3", len(got))
	}
}

func TestAskSidebarBuffersUntilFlush(t *testing.T) {
	t.Parallel()
	childGen := &fakeGenerator{batches: [][]provider.StreamResponse{
		{{TextDelta: "still waiting on /tmp/done", Done: true}},
	}}
	runner, err := NewRunner(RunnerConfig{
		Generator:   &fakeGenerator{},
		Model:       "test",
		WorkDir:     t.TempDir(),
		Interactive: false,
		SubagentGenerator: func(context.Context, *config.Settings) (provider.ContentGenerator, error) {
			return childGen, nil
		},
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	events, err := runner.AskSidebar(testContext(t), "is it done yet?")
	if err != nil {
		t.Fatalf("AskSidebar: %v", err)
	}
	var answer strings.Builder
	for ev := range events {
		if ev.Type == ui.StreamTextDelta {
			answer.WriteString(ev.Text)
		}
	}
	if !strings.Contains(answer.String(), "still waiting") {
		t.Fatalf("sidebar answer = %q", answer.String())
	}
	if runner.pendingSidebarCount() != 1 {
		t.Fatalf("pending = %d, want 1", runner.pendingSidebarCount())
	}
	if got := historyText(runner.History()); strings.Contains(got, "is it done yet?") {
		t.Fatal("AskSidebar wrote the question into parent history before flush")
	}

	runner.flushPendingSidebar()
	hist := runner.History()
	if len(hist) < 2 {
		t.Fatalf("history after flush = %d messages", len(hist))
	}
	if hist[len(hist)-2].Parts[0].Text != "is it done yet?" {
		t.Fatalf("flushed question = %q", hist[len(hist)-2].Parts[0].Text)
	}
}

func TestSidebarFlushRace(t *testing.T) {
	t.Parallel()
	runner, err := NewRunner(RunnerConfig{
		Generator:   &fakeGenerator{},
		Model:       "test",
		WorkDir:     t.TempDir(),
		Interactive: false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			runner.bufferSidebar("q", "a")
			runner.flushPendingSidebar()
		}
	}()
	for i := 0; i < 50; i++ {
		_ = runner.History()
		_ = runner.pendingSidebarCount()
	}
	<-done
}

func historyText(hist []provider.Message) string {
	var b strings.Builder
	for _, msg := range hist {
		for _, p := range msg.Parts {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}
