package agent

import (
	"errors"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/modes"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/tools"
)

func resultTestChild(t *testing.T, class config.SubagentClass) (*subagentHarness, *subagent) {
	t.Helper()
	on := true
	settings := openAISettingsWithModelPins(map[string]int{"parent-model": 100_000})
	settings.Sagittarius.Subagents = &config.SagittariusSubagents{
		Research: &config.SagittariusSubagentClass{Enabled: &on},
		Coding:   &config.SagittariusSubagentClass{Enabled: &on},
	}
	h := newSubagentHarness(t, settings)
	mode := modes.ModeAsk
	if class == config.SubagentCoding {
		mode = modes.ModeAgent
	}
	child, err := h.parent.newSubagent(t.Context(), subagentSpec{
		description: "result-test",
		mode:        mode,
		class:       class,
		approval:    ApprovalYolo,
	})
	if err != nil {
		t.Fatalf("newSubagent: %v", err)
	}
	return h, child
}

func checksResponseMsg(ok bool) provider.Message {
	return provider.Message{
		Role: provider.RoleUser,
		Parts: []provider.Part{{
			FunctionResponse: &provider.FunctionResponse{
				Name:   tools.ProjectChecksToolName,
				CallID: "c-checks",
				Response: map[string]any{
					"stack":  "go",
					"all_ok": ok,
				},
			},
		}},
	}
}

func toolCallMsg(name string) provider.Message {
	return provider.Message{
		Role: provider.RoleModel,
		Parts: []provider.Part{{
			FunctionCall: &provider.ToolCall{ID: "c-1", Name: name, Args: map[string]any{}},
		}},
	}
}

func TestBuildSubagentResultCompletedCoding(t *testing.T) {
	t.Parallel()

	h, child := resultTestChild(t, config.SubagentCoding)
	h.parent.fileState.RecordWrite(child.id, h.parent.workspace.Root()+"/internal/foo/bar.go")
	child.runner.ReplaceHistory([]provider.Message{
		{Role: provider.RoleUser, Parts: []provider.Part{{Text: "add retry"}}},
		toolCallMsg("read_file"),
		checksResponseMsg(true),
		{Role: provider.RoleModel, Parts: []provider.Part{{Text: "added retry with backoff"}}},
	}, nil)

	got := buildSubagentResult(child, config.SubagentCoding, nil, 1, 0)
	if got["status"] != subagentStatusCompleted {
		t.Errorf("status = %v, want completed", got["status"])
	}
	if got["summary"] != "added retry with backoff" {
		t.Errorf("summary = %v", got["summary"])
	}
	files, _ := got["files_changed"].([]string)
	if len(files) != 1 {
		t.Fatalf("files_changed = %v, want one entry", got["files_changed"])
	}
	checks, _ := got["checks"].(map[string]any)
	if checks["ran"] != true || checks["ok"] != true {
		t.Errorf("checks = %v, want ran+ok", got["checks"])
	}
	if got["tool_calls"] != 1 {
		t.Errorf("tool_calls = %v, want 1", got["tool_calls"])
	}
	if got["provider"] != child.runner.ActiveProviderID() || got["model"] != child.runner.Model() {
		t.Errorf("pair = %v/%v", got["provider"], got["model"])
	}
	if got["attempt"] != 1 {
		t.Errorf("attempt = %v, want 1", got["attempt"])
	}
	next, _ := got["next_step"].(string)
	if next == "" {
		t.Error("coding result with changed files must carry next_step")
	}
	// Aliases for existing consumers.
	if got["result"] != got["summary"] {
		t.Error("result alias must equal summary")
	}
	if fw, _ := got["files_written"].([]string); len(fw) != 1 {
		t.Errorf("files_written alias = %v", got["files_written"])
	}
}

func TestBuildSubagentResultFailedFallsBackToError(t *testing.T) {
	t.Parallel()

	_, child := resultTestChild(t, config.SubagentResearch)
	runErr := errors.New("boom")
	got := buildSubagentResult(child, config.SubagentResearch, runErr, 1, 0)
	if got["status"] != subagentStatusFailed {
		t.Errorf("status = %v, want failed", got["status"])
	}
	if got["summary"] != "boom" {
		t.Errorf("summary = %v, want the error text", got["summary"])
	}
	if got["error"] != "boom" {
		t.Errorf("error = %v", got["error"])
	}
	if _, ok := got["next_step"]; ok {
		t.Error("research result must not carry next_step")
	}
}

func TestBuildSubagentResultIncompleteOnRoundCap(t *testing.T) {
	t.Parallel()

	h, child := resultTestChild(t, config.SubagentCoding)
	h.parent.fileState.RecordWrite(child.id, h.parent.workspace.Root()+"/internal/foo/bar.go")
	child.runner.ReplaceHistory([]provider.Message{
		{Role: provider.RoleUser, Parts: []provider.Part{{Text: "big task"}}},
		toolCallMsg("read_file"),
		checksResponseMsg(false),
	}, nil)
	child.roundCapped = true

	got := buildSubagentResult(child, config.SubagentCoding, errors.New("subagent hit max tool rounds"), 2, 2)
	if got["status"] != subagentStatusIncomplete {
		t.Errorf("status = %v, want incomplete", got["status"])
	}
	if _, ok := got["error"]; ok {
		t.Errorf("incomplete hand-off must omit error so the card renders the schema, got %v", got["error"])
	}
	files, _ := got["files_changed"].([]string)
	if len(files) != 1 {
		t.Errorf("incomplete hand-off must keep files_changed, got %v", got["files_changed"])
	}
	checks, _ := got["checks"].(map[string]any)
	if checks["ran"] != true || checks["ok"] != false {
		t.Errorf("checks = %v, want ran+not-ok", got["checks"])
	}
	next, _ := got["next_step"].(string)
	if next == "" {
		t.Error("incomplete coding hand-off must tell the parent to finish the task")
	}
}

func TestLastChecksOutcomePicksLatest(t *testing.T) {
	t.Parallel()

	history := []provider.Message{
		checksResponseMsg(false),
		checksResponseMsg(true),
	}
	if ran, ok := lastChecksOutcome(history); !ran || !ok {
		t.Errorf("lastChecksOutcome = %v/%v, want true/true", ran, ok)
	}
	if ran, _ := lastChecksOutcome(nil); ran {
		t.Error("empty history must not report checks")
	}
}
