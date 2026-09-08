package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/tools"
)

func scratchpadToolRunner(t *testing.T, enabled *bool) *Runner {
	t.Helper()
	settings := &config.Settings{Sagittarius: &config.SagittariusSettings{ScratchpadEnabled: enabled}}
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
	return runner
}

func TestScratchpadToolRegisteredByDefault(t *testing.T) {
	t.Parallel()

	runner := scratchpadToolRunner(t, nil)
	if _, ok := runner.Registry().Lookup(tools.UpdateScratchpadToolName); !ok {
		t.Fatal("update_scratchpad is not registered with default settings; it should default on")
	}
}

func TestScratchpadToolNotRegisteredWhenDisabled(t *testing.T) {
	t.Parallel()

	off := false
	runner := scratchpadToolRunner(t, &off)
	if _, ok := runner.Registry().Lookup(tools.UpdateScratchpadToolName); ok {
		t.Fatal("update_scratchpad is registered with scratchpadEnabled=false")
	}
}

func TestScratchpadToolExecute(t *testing.T) {
	t.Parallel()

	runner := scratchpadToolRunner(t, nil)
	tool, ok := runner.Registry().Lookup(tools.UpdateScratchpadToolName)
	if !ok {
		t.Fatal("update_scratchpad not registered")
	}
	if tool.RequiresConfirmation() {
		t.Error("update_scratchpad requires confirmation; it must run unattended")
	}

	const note = "Step 2 of 4: backfill is running, resume at offset 41200"
	res, err := tool.Execute(context.Background(), map[string]any{tools.ScratchpadParamContent: note})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := res["content"]; got != note {
		t.Errorf("result content = %v, want %q", got, note)
	}
	if truncated, _ := res["truncated"].(bool); truncated {
		t.Error("short note reported as truncated")
	}
	if runner.Scratchpad() != note {
		t.Errorf("Scratchpad() = %q, want %q", runner.Scratchpad(), note)
	}
}

// TestScratchpadToolClearsOnEmptyString is why Execute uses presentStringArg:
// an empty string is a legitimate clear, and the strict stringArg helper would
// reject it as a missing parameter, leaving the model no way to drop a stale note.
func TestScratchpadToolClearsOnEmptyString(t *testing.T) {
	t.Parallel()

	runner := scratchpadToolRunner(t, nil)
	if err := runner.SetScratchpad("stale note"); err != nil {
		t.Fatalf("SetScratchpad: %v", err)
	}
	tool, _ := runner.Registry().Lookup(tools.UpdateScratchpadToolName)

	if _, err := tool.Execute(context.Background(), map[string]any{tools.ScratchpadParamContent: ""}); err != nil {
		t.Fatalf("Execute(clear): %v", err)
	}
	if got := runner.Scratchpad(); got != "" {
		t.Errorf("Scratchpad() = %q after clearing, want empty", got)
	}
}

func TestScratchpadToolReportsTruncation(t *testing.T) {
	t.Parallel()

	runner := scratchpadToolRunner(t, nil)
	tool, _ := runner.Registry().Lookup(tools.UpdateScratchpadToolName)

	res, err := tool.Execute(context.Background(), map[string]any{
		tools.ScratchpadParamContent: strings.Repeat("x", ScratchpadMaxRunes+100),
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if truncated, _ := res["truncated"].(bool); !truncated {
		t.Error("over-cap note not reported as truncated; the model would assume it all landed")
	}
	if n, _ := res["runes"].(int); n != ScratchpadMaxRunes {
		t.Errorf("result runes = %v, want %d", res["runes"], ScratchpadMaxRunes)
	}
}

// TestScratchpadToolMissingArgErrors covers the third arg case: absent is an
// error even though empty is not.
func TestScratchpadToolMissingArgErrors(t *testing.T) {
	t.Parallel()

	runner := scratchpadToolRunner(t, nil)
	tool, _ := runner.Registry().Lookup(tools.UpdateScratchpadToolName)

	if _, err := tool.Execute(context.Background(), map[string]any{}); err == nil {
		t.Fatal("Execute with no content argument succeeded; want an error")
	}
}

// TestWorkingMemoryToolsAbsentFromSubagent verifies that children (subagents)
// do not receive working memory tools (update_scratchpad or search_session).
func TestWorkingMemoryToolsAbsentFromSubagent(t *testing.T) {
	t.Parallel()

	runner, err := NewRunner(RunnerConfig{
		Generator:   &fakeGenerator{},
		Model:       "test-model",
		WorkDir:     t.TempDir(),
		Interactive: false,
		AgentID:     "subagent-child-1",
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	if _, ok := runner.Registry().Lookup(tools.UpdateScratchpadToolName); ok {
		t.Errorf("%s should not be registered on subagents", tools.UpdateScratchpadToolName)
	}
	if _, ok := runner.Registry().Lookup(tools.SearchSessionToolName); ok {
		t.Errorf("%s should not be registered on subagents", tools.SearchSessionToolName)
	}
}
