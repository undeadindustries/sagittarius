package tools

import (
	"context"
	"sync"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

type stdinProbeTool struct {
	sawStdin bool
}

func (t *stdinProbeTool) Name() string               { return ShellToolName }
func (t *stdinProbeTool) Description() string        { return "probe" }
func (t *stdinProbeTool) RequiresConfirmation() bool { return false }
func (t *stdinProbeTool) Declaration() provider.ToolDeclaration {
	return provider.ToolDeclaration{Name: ShellToolName}
}
func (t *stdinProbeTool) Execute(ctx context.Context, _ map[string]any) (map[string]any, error) {
	t.sawStdin = ptyStdinFrom(ctx) != nil
	return map[string]any{"output": "ok"}, nil
}
func (t *stdinProbeTool) ExecuteStream(ctx context.Context, _ map[string]any, _ ToolOutputSink) (map[string]any, error) {
	t.sawStdin = ptyStdinFrom(ctx) != nil
	return map[string]any{"output": "ok"}, nil
}

func TestSchedulerShellStdinRegistryInteractive(t *testing.T) {
	t.Parallel()

	probe := &stdinProbeTool{}
	registry := &Registry{
		byName:  map[string]Tool{ShellToolName: probe},
		aliases: map[string]string{},
	}

	var mu sync.Mutex
	registered := map[string]*PTYStdin{}
	unregistered := map[string]bool{}

	regHook := func(callID string, stdin *PTYStdin) func() {
		mu.Lock()
		registered[callID] = stdin
		mu.Unlock()
		return func() {
			mu.Lock()
			unregistered[callID] = true
			mu.Unlock()
		}
	}

	s := NewScheduler(registry, Policy{}, true, nil, nil,
		WithShellStdinRegistry(regHook),
	)

	calls := []provider.ToolCall{
		{ID: "call-shell-1", Name: ShellToolName, Args: map[string]any{ShellParamCommand: "echo 1"}},
	}

	emit := func(ev ui.StreamEvent) {
		if ev.Type == ui.StreamToolConfirm && ev.ConfirmReply != nil {
			ev.ConfirmReply <- ui.ConfirmOnce
		}
	}

	res, _ := s.Execute(context.Background(), calls, emit)
	if len(res) != 1 {
		t.Fatalf("results = %d, want 1", len(res))
	}
	if !probe.sawStdin {
		t.Fatal("probe did not see PTYStdin in context")
	}

	mu.Lock()
	defer mu.Unlock()
	if registered["call-shell-1"] == nil {
		t.Fatal("expected call-shell-1 to be registered in shellStdinRegistry")
	}
	if !unregistered["call-shell-1"] {
		t.Fatal("expected call-shell-1 to be unregistered after tool completion")
	}
}

func TestSchedulerShellStdinRegistryNonInteractive(t *testing.T) {
	t.Parallel()

	probe := &stdinProbeTool{}
	registry := &Registry{
		byName:  map[string]Tool{ShellToolName: probe},
		aliases: map[string]string{},
	}

	var mu sync.Mutex
	registered := map[string]*PTYStdin{}

	regHook := func(callID string, stdin *PTYStdin) func() {
		mu.Lock()
		registered[callID] = stdin
		mu.Unlock()
		return func() {}
	}

	s := NewScheduler(registry, Policy{}, false, nil, nil,
		WithShellStdinRegistry(regHook),
	)

	calls := []provider.ToolCall{
		{ID: "call-shell-2", Name: ShellToolName, Args: map[string]any{ShellParamCommand: "echo 2"}},
	}

	res, _ := s.Execute(context.Background(), calls, func(ui.StreamEvent) {})
	if len(res) != 1 {
		t.Fatalf("results = %d, want 1", len(res))
	}
	if probe.sawStdin {
		t.Fatal("probe should not see PTYStdin in non-interactive scheduler")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(registered) != 0 {
		t.Fatalf("registered = %v, want empty for non-interactive", registered)
	}
}
