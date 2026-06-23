package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/undeadindustries/sagittarius/internal/diff"
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

	// sessionGrants records tools the user approved "for this session" so later
	// invocations of the same tool skip confirmation. Guarded by mu.
	mu            sync.Mutex
	sessionGrants map[string]bool
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

	emit(ui.StreamEvent{Type: ui.StreamToolStart, ToolName: name, Text: formatToolSummary(name, args)})

	// Project-boundary gate runs before the interaction-mode gate so it applies
	// in every mode (the protected-snapshot guard is always active; out-of-root
	// blocking is gated on enforce).
	if allowed, reason := ProjectBoundaryAllow(s.enforce, name, args, s.workspace); !allowed {
		emit(ui.StreamEvent{Type: ui.StreamToolResult, ToolName: name, Text: reason})
		return errorResponse(call, reason), nil
	}

	if allowed, reason := s.interactionModeAllow(name, args); !allowed {
		emit(ui.StreamEvent{Type: ui.StreamToolResult, ToolName: name, Text: reason})
		return errorResponse(call, reason), nil
	}

	tool, ok := s.registry.Lookup(name)
	if !ok {
		errText := fmt.Sprintf("unknown tool %q", name)
		emit(ui.StreamEvent{Type: ui.StreamToolResult, ToolName: name, Text: errText})
		return errorResponse(call, errText), nil
	}

	if s.policy.NeedsConfirmation(tool) && !s.sessionGranted(name) {
		approved, err := s.requestApproval(ctx, tool.Name(), args, emit)
		if err != nil {
			return nil, err
		}
		if !approved {
			errText := "user denied tool execution"
			emit(ui.StreamEvent{Type: ui.StreamToolResult, ToolName: name, Text: errText})
			return errorResponse(call, errText), nil
		}
	}

	// Compute the write_file diff (before -> after) before the tool mutates the
	// file so the result line can show exactly what changed.
	writeDiff := ""
	if canonicalToolName(name) == WriteFileToolName {
		writeDiff = s.writeFileDiff(args)
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
		return errorResponse(call, errText), nil
	}

	if snapAbs != "" {
		s.snapshotter.CommitWrite(snapAbs, canonicalToolName(name))
	}

	resultText := "ok"
	if writeDiff != "" {
		resultText = writeDiff
	}
	emit(ui.StreamEvent{Type: ui.StreamToolResult, ToolName: name, Text: resultText})
	return &provider.FunctionResponse{Name: name, CallID: call.ID, Response: result}, nil
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

	replyCh := make(chan ui.ConfirmDecision, 1)
	emit(ui.StreamEvent{
		Type:         ui.StreamToolConfirm,
		ToolName:     toolName,
		Text:         formatConfirmSummary(toolName, args),
		Diff:         s.writeFileDiff(args),
		ConfirmReply: replyCh,
	})

	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case decision := <-replyCh:
		switch decision {
		case ui.ConfirmSession:
			s.grantSession(toolName)
			return true, nil
		case ui.ConfirmOnce:
			return true, nil
		default:
			return false, nil
		}
	}
}

// sessionGranted reports whether the user approved this tool "for this session".
func (s *Scheduler) sessionGranted(toolName string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessionGrants[canonicalToolName(toolName)]
}

// grantSession records a session-wide approval for the named tool.
func (s *Scheduler) grantSession(toolName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessionGrants == nil {
		s.sessionGrants = make(map[string]bool)
	}
	s.sessionGrants[canonicalToolName(toolName)] = true
}

// writeFileDiff returns a git-style unified diff of a write_file call's pending
// change (current on-disk content vs. the new content). It returns "" for
// non-write_file calls, when the workspace is unavailable, or when the change
// is a no-op.
func (s *Scheduler) writeFileDiff(args map[string]any) string {
	if s.workspace == nil {
		return ""
	}
	path, err := stringArg(args, ParamFilePath)
	if err != nil {
		return ""
	}
	content, err := stringArg(args, WriteFileParamContent)
	if err != nil {
		return ""
	}
	abs, err := s.workspace.ResolvePath(path)
	if err != nil {
		return ""
	}
	before := ""
	if b, readErr := os.ReadFile(abs); readErr == nil {
		before = string(b)
	}
	return diff.UnifiedDiff(before, content, filepath.Base(path))
}

func errorResponse(call provider.ToolCall, message string) *provider.FunctionResponse {
	return &provider.FunctionResponse{
		Name:   call.Name,
		CallID: call.ID,
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

// formatToolSummary returns a short, single-line argument detail for a tool
// invocation (e.g. the target path or the shell command), used to label the
// tool-start line in the scrollback. It returns "" when there is no concise
// detail to show.
func formatToolSummary(toolName string, args map[string]any) string {
	switch canonicalToolName(toolName) {
	case WriteFileToolName:
		if path, err := stringArg(args, ParamFilePath); err == nil {
			return path
		}
	case ShellToolName:
		if cmd, err := stringArg(args, ShellParamCommand); err == nil {
			return truncateOneLine(cmd, 72)
		}
	}
	return ""
}

// truncateOneLine collapses s to its first line and caps it at max runes,
// appending an ellipsis when truncated.
func truncateOneLine(s string, max int) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
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
