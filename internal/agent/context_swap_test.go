package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/contextmgmt"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/tools"
)

// TestRunnerRegistrySwapConcurrent guards the registry/scheduler swap against a
// data race: SetRegistry (called from a slash-command handler) must be safe to
// run while a turn reads the registry and scheduler on its own goroutine. Run
// with -race to catch unsynchronized access.
func TestRunnerRegistrySwapConcurrent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	runner, err := NewRunner(RunnerConfig{
		Generator:   &fakeGenerator{},
		Model:       "test-model",
		WorkDir:     dir,
		Interactive: false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	ws, err := tools.NewWorkspace(dir)
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}

	const iterations = 200
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			runner.SetRegistry(tools.NewBuiltinRegistry(ws))
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = runner.buildGenerateRequest()
			_ = runner.toolScheduler()
		}
	}()

	wg.Wait()

	if runner.Registry() == nil {
		t.Fatal("registry should be non-nil after concurrent swaps")
	}
	if runner.toolScheduler() == nil {
		t.Fatal("scheduler should be non-nil after concurrent swaps")
	}
}

func toolOutputMsg(name, output string) provider.Message {
	return provider.Message{Role: provider.RoleUser, Parts: []provider.Part{{
		FunctionResponse: &provider.FunctionResponse{Name: name, Response: map[string]any{"output": output}},
	}}}
}

func writeFileCallMsg(filePath, content string) provider.Message {
	return provider.Message{Role: provider.RoleModel, Parts: []provider.Part{{
		FunctionCall: &provider.ToolCall{Name: "write_file", Args: map[string]any{"file_path": filePath, "content": content}},
	}}}
}

func writeFileCallContent(m provider.Message) string {
	if len(m.Parts) == 0 || m.Parts[0].FunctionCall == nil {
		return ""
	}
	if v, ok := m.Parts[0].FunctionCall.Args["content"].(string); ok {
		return v
	}
	return ""
}

// TestRunnerSwapsContextManager verifies a provider switch can swap the active
// context manager: the new manager governs PrepareTurn, and a nil manager (e.g.
// switching to gemini-native / openai-responses) is a pure pass-through.
func TestRunnerSwapsContextManager(t *testing.T) {
	t.Parallel()

	runner, err := NewRunner(RunnerConfig{
		Generator:   &fakeGenerator{},
		Model:       "test-model",
		WorkDir:     t.TempDir(),
		Interactive: false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	history := []provider.Message{
		toolOutputMsg("read_file", strings.Repeat("data ", 4_000)),
		{Role: provider.RoleUser, Parts: []provider.Part{{Text: "what did that file say?"}}},
	}
	before := contextmgmt.EstimateTokens(flattenPartsForTest(history))

	masking := contextmgmt.NewManager(contextmgmt.ManagerConfig{
		Enabled:                  true,
		ContextLimit:             20_000,
		MaskingEnabled:           true,
		MaskingProtectLatestTurn: true,
		OutputDir:                t.TempDir(),
	})

	runner.history = cloneMessages(history)
	runner.SetContextManager(masking)
	runner.prepareContext(context.Background())
	if after := contextmgmt.EstimateTokens(flattenPartsForTest(runner.history)); after >= before {
		t.Fatalf("masking manager should reduce tokens: before=%d after=%d", before, after)
	}

	runner.history = cloneMessages(history)
	runner.SetContextManager(nil)
	runner.prepareContext(context.Background())
	if after := contextmgmt.EstimateTokens(flattenPartsForTest(runner.history)); after != before {
		t.Fatalf("nil manager must pass through unchanged: before=%d after=%d", before, after)
	}
}

// TestNewContextManagerEjectionSkipsSmallPayloads guards the ejection min-tokens
// floor: stale write_file payloads below the threshold are preserved while large
// ones are ejected. Without the floor, trivial writes would be ejected too.
func TestNewContextManagerEjectionSkipsSmallPayloads(t *testing.T) {
	t.Parallel()

	settings := &config.Settings{Providers: &config.ProvidersSettings{Active: string(config.BuiltInOpenAI)}}
	mgr := NewContextManager(settings, nil, func() string { return "gpt-4o" }, "sess-test")
	if mgr == nil {
		t.Fatal("expected a context manager for the openai-chat provider")
	}

	big := strings.Repeat("x", 8_000)
	history := []provider.Message{
		{Role: provider.RoleUser, Parts: []provider.Part{{Text: "lead 1"}}},
		{Role: provider.RoleModel, Parts: []provider.Part{{Text: "lead 2"}}},
		writeFileCallMsg("/small.txt", "short"),
		writeFileCallMsg("/big.txt", big),
		{Role: provider.RoleModel, Parts: []provider.Part{{Text: "resp"}}},
		{Role: provider.RoleUser, Parts: []provider.Part{{Text: "next turn"}}},
	}

	got, err := mgr.PrepareTurn(context.Background(), history, 0)
	if err != nil {
		t.Fatalf("PrepareTurn: %v", err)
	}

	if c := writeFileCallContent(got[2]); c != "short" {
		t.Errorf("small payload should be preserved, got %q", c)
	}
	if c := writeFileCallContent(got[3]); c == big {
		t.Error("large stale payload should have been ejected")
	}
}

func flattenPartsForTest(history []provider.Message) []provider.Part {
	var parts []provider.Part
	for i := range history {
		parts = append(parts, history[i].Parts...)
	}
	return parts
}

func cloneMessages(history []provider.Message) []provider.Message {
	out := make([]provider.Message, len(history))
	copy(out, history)
	return out
}
