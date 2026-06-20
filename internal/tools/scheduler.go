package tools

import (
	"context"
	"fmt"

	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

const MaxToolRounds = 10

// Scheduler executes tool calls from the agent loop.
type Scheduler struct {
	registry    *Registry
	policy      Policy
	interactive bool
}

// NewScheduler constructs a scheduler for the given registry and policy.
// When interactive is false (headless), confirmations are auto-approved or denied per policy.
func NewScheduler(registry *Registry, policy Policy, interactive bool) *Scheduler {
	return &Scheduler{
		registry:    registry,
		policy:      policy,
		interactive: interactive,
	}
}

// Execute runs tool calls and returns function responses plus UI events.
func (s *Scheduler) Execute(
	ctx context.Context,
	calls []provider.ToolCall,
	emit func(ui.StreamEvent),
) ([]provider.FunctionResponse, error) {
	responses := make([]provider.FunctionResponse, 0, len(calls))
	for _, call := range calls {
		resp, err := s.executeOne(ctx, call, emit)
		if err != nil {
			return responses, err
		}
		if resp != nil {
			responses = append(responses, *resp)
		}
	}
	return responses, nil
}

func (s *Scheduler) executeOne(
	ctx context.Context,
	call provider.ToolCall,
	emit func(ui.StreamEvent),
) (*provider.FunctionResponse, error) {
	name := call.Name
	args := call.Args
	if args == nil {
		args = map[string]any{}
	}

	emit(ui.StreamEvent{Type: ui.StreamToolStart, ToolName: name})

	tool, ok := s.registry.Lookup(name)
	if !ok {
		errText := fmt.Sprintf("unknown tool %q", name)
		emit(ui.StreamEvent{Type: ui.StreamToolResult, ToolName: name, Text: errText})
		return errorResponse(name, errText), nil
	}

	if s.policy.NeedsConfirmation(tool) {
		approved, err := s.requestApproval(ctx, tool.Name(), args, emit)
		if err != nil {
			return nil, err
		}
		if !approved {
			errText := "user denied tool execution"
			emit(ui.StreamEvent{Type: ui.StreamToolResult, ToolName: name, Text: errText})
			return errorResponse(name, errText), nil
		}
	}

	result, err := tool.Execute(ctx, args)
	if err != nil {
		errText := err.Error()
		emit(ui.StreamEvent{Type: ui.StreamToolResult, ToolName: name, Text: errText})
		return errorResponse(name, errText), nil
	}

	emit(ui.StreamEvent{Type: ui.StreamToolResult, ToolName: name, Text: "ok"})
	return &provider.FunctionResponse{Name: name, Response: result}, nil
}

func (s *Scheduler) requestApproval(
	ctx context.Context,
	toolName string,
	args map[string]any,
	emit func(ui.StreamEvent),
) (bool, error) {
	tool, ok := s.registry.Lookup(toolName)
	if !ok {
		return false, nil
	}

	if !s.interactive {
		return s.policy.HeadlessApprove(tool), nil
	}

	replyCh := make(chan bool, 1)
	emit(ui.StreamEvent{
		Type:         ui.StreamToolConfirm,
		ToolName:     toolName,
		Text:         formatConfirmSummary(toolName, args),
		ConfirmReply: replyCh,
	})

	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case approved := <-replyCh:
		return approved, nil
	}
}

func errorResponse(name, message string) *provider.FunctionResponse {
	return &provider.FunctionResponse{
		Name: name,
		Response: map[string]any{
			"error": message,
		},
	}
}

func formatConfirmSummary(toolName string, args map[string]any) string {
	switch toolName {
	case WriteFileToolName:
		if path, ok := args[ParamFilePath]; ok {
			return fmt.Sprintf("write %v", path)
		}
	case ShellToolName:
		if cmd, ok := args[ShellParamCommand]; ok {
			return fmt.Sprintf("run %v", cmd)
		}
	}
	return toolName
}
