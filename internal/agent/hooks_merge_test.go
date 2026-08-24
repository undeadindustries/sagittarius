package agent

import (
	"testing"

	"github.com/undeadindustries/sagittarius/internal/hooks"
)

func TestMergeBeforeToolResultsCombinesRewrites(t *testing.T) {
	t.Parallel()
	results := []hooks.ExecutionResult{
		{Output: &hooks.HookOutput{HookSpecificOutput: map[string]any{
			"tool_input": map[string]any{"file_path": "redirected.txt"},
		}}},
		{Output: &hooks.HookOutput{HookSpecificOutput: map[string]any{
			"tool_input": map[string]any{"content": "from hook 2\n"},
		}}},
	}
	got, deny, reason := mergeBeforeToolResults(results)
	if deny {
		t.Fatalf("unexpected deny: %s", reason)
	}
	if got["file_path"] != "redirected.txt" {
		t.Fatalf("file_path dropped: %v", got)
	}
	if got["content"] != "from hook 2\n" {
		t.Fatalf("content dropped: %v", got)
	}
}

func TestMergeBeforeToolResultsDenyShortCircuits(t *testing.T) {
	t.Parallel()
	results := []hooks.ExecutionResult{
		{Output: &hooks.HookOutput{HookSpecificOutput: map[string]any{
			"tool_input": map[string]any{"file_path": "x.txt"},
		}}},
		{Output: &hooks.HookOutput{Decision: hooks.DecisionDeny, Reason: "blocked"}},
	}
	got, deny, reason := mergeBeforeToolResults(results)
	if !deny {
		t.Fatal("expected deny")
	}
	if reason != "blocked" {
		t.Fatalf("reason = %q", reason)
	}
	if got != nil {
		t.Fatalf("mods should be nil on deny, got %v", got)
	}
}

func TestMergeBeforeToolResultsLaterKeyWins(t *testing.T) {
	t.Parallel()
	results := []hooks.ExecutionResult{
		{Output: &hooks.HookOutput{HookSpecificOutput: map[string]any{
			"tool_input": map[string]any{"file_path": "first.txt", "content": "a"},
		}}},
		{Output: &hooks.HookOutput{HookSpecificOutput: map[string]any{
			"tool_input": map[string]any{"file_path": "second.txt"},
		}}},
	}
	got, deny, _ := mergeBeforeToolResults(results)
	if deny {
		t.Fatal("deny")
	}
	if got["file_path"] != "second.txt" {
		t.Fatalf("file_path = %v", got["file_path"])
	}
	if got["content"] != "a" {
		t.Fatalf("content should survive overlay: %v", got)
	}
}
