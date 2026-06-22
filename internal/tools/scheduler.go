package tools

import (
	"context"
	"fmt"

	"github.com/undeadindustries/sagittarius/internal/modes"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

const MaxToolRounds = 10

// Scheduler executes tool calls from the agent loop.
type Scheduler struct {
	registry    *Registry
	policy      Policy
	interactive bool
	mode        func() modes.Mode
	workspace   *Workspace
	enforce     bool
	snapshotter Snapshotter
}

// SchedulerOption configures optional Scheduler behavior.
type SchedulerOption func(*Scheduler)

// WithProjectBoundary enables out-of-project mutation blocking (file writes and
// the shell heuristic). The protected-snapshot-path guard applies regardless.
func WithProjectBoundary(enforce bool) SchedulerOption {
	return func(s *Scheduler) { s.enforce = enforce }
}

// WithSnapshotter installs the snapshot hook fired around write_file. A nil
// snapshotter leaves snapshotting disabled.
func WithSnapshotter(snap Snapshotter) SchedulerOption {
	return func(s *Scheduler) { s.snapshotter = snap }
}

// NewScheduler constructs a scheduler for the given registry and policy.
// When interactive is false (headless), confirmations are auto-approved or denied per policy.
// mode and workspace enable interaction-mode tool restrictions (plan/ask read-only gates).
func NewScheduler(
	registry *Registry,
	policy Policy,
	interactive bool,
	mode func() modes.Mode,
	workspace *Workspace,
	opts ...SchedulerOption,
) *Scheduler {
	s := &Scheduler{
		registry:    registry,
		policy:      policy,
		interactive: interactive,
		mode:        mode,
		workspace:   workspace,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
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

	// Project-boundary gate runs before the interaction-mode gate so it applies
	// in every mode (the protected-snapshot guard is always active; out-of-root
	// blocking is gated on enforce).
	if allowed, reason := ProjectBoundaryAllow(s.enforce, name, args, s.workspace); !allowed {
		emit(ui.StreamEvent{Type: ui.StreamToolResult, ToolName: name, Text: reason})
		return errorResponse(name, reason), nil
	}

	if allowed, reason := s.interactionModeAllow(name, args); !allowed {
		emit(ui.StreamEvent{Type: ui.StreamToolResult, ToolName: name, Text: reason})
		return errorResponse(name, reason), nil
	}

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

	// Snapshot the prior state of a write target before the tool mutates it.
	snapAbs := s.snapshotTarget(name, args)
	if snapAbs != "" {
		s.snapshotter.CaptureWrite(snapAbs)
	}

	result, err := tool.Execute(ctx, args)
	if err != nil {
		errText := err.Error()
		emit(ui.StreamEvent{Type: ui.StreamToolResult, ToolName: name, Text: errText})
		return errorResponse(name, errText), nil
	}

	if snapAbs != "" {
		s.snapshotter.CommitWrite(snapAbs, canonicalToolName(name))
	}

	emit(ui.StreamEvent{Type: ui.StreamToolResult, ToolName: name, Text: "ok"})
	return &provider.FunctionResponse{Name: name, Response: result}, nil
}

// snapshotTarget returns the resolved absolute path a write_file call will
// mutate, or "" when snapshotting does not apply (no snapshotter, not a
// write_file, missing/invalid path, or path outside the workspace).
func (s *Scheduler) snapshotTarget(name string, args map[string]any) string {
	if s.snapshotter == nil || s.workspace == nil {
		return ""
	}
	if canonicalToolName(name) != WriteFileToolName {
		return ""
	}
	path, err := stringArg(args, ParamFilePath)
	if err != nil {
		return ""
	}
	abs, err := s.workspace.ResolvePath(path)
	if err != nil {
		return ""
	}
	return abs
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

func (s *Scheduler) interactionModeAllow(toolName string, args map[string]any) (bool, string) {
	if s.mode == nil {
		return true, ""
	}
	return InteractionModeAllow(s.mode(), toolName, args, s.workspace)
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
