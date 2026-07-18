package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/undeadindustries/sagittarius/internal/diff"
	"github.com/undeadindustries/sagittarius/internal/modes"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/ui"
	"github.com/undeadindustries/sagittarius/internal/web"
)

// MaxToolRounds is the default maximum number of tool-call/response cycles per
// agent turn. It can be overridden via sagittarius.maxToolRounds in
// settings.json. 100 matches gemini-cli's default; 10 was too low for real
// agentic tasks that write multiple files.
const MaxToolRounds = 100

// Scheduler executes tool calls from the agent loop.
type Scheduler struct {
	registry    *Registry
	policy      Policy
	interactive bool
	mode        func() modes.Mode
	workspace   *Workspace
	enforce     bool
	snapshotter Snapshotter
	// readOnly, when non-nil and returning true, forces read-only tool gating
	// regardless of the active interaction mode (grill mode while
	// interrogating). ask_user is always exempt so the interrogation itself
	// can proceed.
	readOnly func() bool

	// sessionGrants records tools the user approved "for this session" so later
	// invocations of the same tool skip confirmation. Guarded by mu.
	mu            sync.Mutex
	sessionGrants map[string]bool
	grantRecorder func(string)
}

// SchedulerOption configures optional Scheduler behavior.
type SchedulerOption func(*Scheduler)

// WithSessionGrants pre-populates tools already approved for the session.
func WithSessionGrants(grants []string) SchedulerOption {
	return func(s *Scheduler) {
		if s.sessionGrants == nil {
			s.sessionGrants = make(map[string]bool)
		}
		for _, g := range grants {
			s.sessionGrants[canonicalToolName(g)] = true
		}
	}
}

// WithSessionGrantRecorder registers a callback invoked when a tool is approved
// for the session.
func WithSessionGrantRecorder(cb func(string)) SchedulerOption {
	return func(s *Scheduler) { s.grantRecorder = cb }
}

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

// WithReadOnlyGate installs a signal that, while true, forces read-only tool
// gating regardless of the active interaction mode (used by grill mode).
func WithReadOnlyGate(fn func() bool) SchedulerOption {
	return func(s *Scheduler) { s.readOnly = fn }
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
	id := call.ID
	args := call.Args
	if args == nil {
		args = map[string]any{}
	}

	emitErr := func(reason string) {
		emit(ui.StreamEvent{Type: ui.StreamToolResult, ToolName: name, ToolCallID: id, Text: reason, IsError: true})
	}

	emit(ui.StreamEvent{Type: ui.StreamToolStart, ToolName: name, ToolCallID: id, Text: formatToolSummary(name, args)})

	// Project-boundary gate runs before the interaction-mode gate so it applies
	// in every mode (the protected-snapshot guard is always active; out-of-root
	// blocking is gated on enforce).
	if allowed, reason := ProjectBoundaryAllow(s.enforce, name, args, s.workspace); !allowed {
		emitErr(reason)
		return errorResponse(call, reason), nil
	}

	if allowed, reason := s.interactionModeAllow(name, args); !allowed {
		emitErr(reason)
		return errorResponse(call, reason), nil
	}

	tool, ok := s.registry.Lookup(name)
	if !ok {
		errText := fmt.Sprintf("unknown tool %q", name)
		emitErr(errText)
		return errorResponse(call, errText), nil
	}

	if canonicalToolName(name) == WriteFileToolName {
		if err := validateWriteFileArgs(args); err != nil {
			emitErr(err.Error())
			return errorResponse(call, err.Error()), nil
		}
	}

	if s.policy.NeedsConfirmation(tool) && !s.sessionGranted(tool.Name()) {
		approved, err := s.requestApproval(ctx, tool.Name(), id, args, emit)
		if err != nil {
			return nil, err
		}
		if !approved {
			errText := "user denied tool execution"
			emitErr(errText)
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

	var result map[string]any
	var err error

	switch t := tool.(type) {
	case InteractiveTool:
		wrappedEmit := func(se ui.StreamEvent) {
			if se.ToolCallID == "" {
				se.ToolCallID = id
			}
			if se.ToolName == "" {
				se.ToolName = name
			}
			emit(se)
		}
		result, err = t.ExecuteInteractive(ctx, args, s.interactive, wrappedEmit)
	case StreamingTool:
		sink := func(text string) {
			emit(ui.StreamEvent{Type: ui.StreamToolOutput, ToolName: name, ToolCallID: id, Text: text})
		}
		result, err = t.ExecuteStream(ctx, args, sink)
	default:
		result, err = tool.Execute(ctx, args)
	}

	if err != nil {
		emitErr(err.Error())
		return errorResponse(call, err.Error()), nil
	}

	if snapAbs != "" {
		s.snapshotter.CommitWrite(snapAbs, canonicalToolName(name))
	}

	resultText, exitCode, isErr := formatToolResult(name, result, writeDiff)
	emit(ui.StreamEvent{
		Type:       ui.StreamToolResult,
		ToolName:   name,
		ToolCallID: id,
		Text:       resultText,
		ExitCode:   exitCode,
		IsError:    isErr,
	})
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
	callID string,
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
		ToolCallID:   callID,
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

// SessionGrants returns a list of tools granted for this session.
func (s *Scheduler) SessionGrants() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	grants := make([]string, 0, len(s.sessionGrants))
	for g := range s.sessionGrants {
		grants = append(grants, g)
	}
	return grants
}

// grantSession records a session-wide approval for the named tool.
func (s *Scheduler) grantSession(toolName string) {
	s.mu.Lock()
	if s.sessionGrants == nil {
		s.sessionGrants = make(map[string]bool)
	}
	name := canonicalToolName(toolName)
	granted := s.sessionGrants[name]
	s.sessionGrants[name] = true
	cb := s.grantRecorder
	s.mu.Unlock()

	if !granted && cb != nil {
		cb(name)
	}
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
	if s.readOnly != nil && s.readOnly() {
		if allowed, reason := grillModeAllow(canonicalToolName(toolName), args); !allowed {
			return false, reason
		}
	}
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

// toolResultMaxLines caps how many lines of a tool's textual result are carried
// to the UI card (the full result still goes to the model in the function
// response). The card renderer applies its own visual cap on top of this.
const toolResultMaxLines = 40

// formatToolResult builds the human-readable result text for a completed tool,
// plus the shell exit code (when applicable) and whether the result is an error.
// The write_file diff, when present, is used verbatim so the card shows the
// change. Other tools get a concise summary derived from their result map; an
// "error" key always wins. An empty result falls back to "ok".
func formatToolResult(name string, result map[string]any, writeDiff string) (text string, exitCode *int, isErr bool) {
	if writeDiff != "" {
		return writeDiff, nil, false
	}
	if result == nil {
		return "ok", nil, false
	}
	if errText, ok := result["error"].(string); ok && errText != "" {
		return capLines(errText, toolResultMaxLines), nil, true
	}

	switch canonicalToolName(name) {
	case ShellToolName:
		return formatShellResult(result)
	case ReadFileToolName:
		if path, ok := result["file_path"].(string); ok {
			n := lineCount(asString(result["content"]))
			return fmt.Sprintf("Read %s (%d lines)", path, n), nil, false
		}
	case ListDirectoryToolName:
		if names, ok := result["entries"].([]string); ok {
			return fmt.Sprintf("%d entries", len(names)), nil, false
		}
	case GrepToolName:
		if matches, ok := result["matches"].(string); ok {
			return capLines(matches, toolResultMaxLines), nil, false
		}
	case FindSymbolToolName:
		return formatFindSymbolResult(result), nil, false
	}

	// MCP tools (and any other tool) carry their payload under "result".
	if v, ok := result["result"]; ok {
		return capLines(stringifyResult(v), toolResultMaxLines), nil, false
	}
	return "ok", nil, false
}

// formatFindSymbolResult renders a find_symbol result: a one-line count header
// (definitions/references) followed by the capped match list.
func formatFindSymbolResult(result map[string]any) string {
	count, _ := intValue(result["count"])
	if count == 0 {
		return "no symbols found"
	}
	defs, _ := intValue(result["definitions"])
	refs, _ := intValue(result["references"])
	header := fmt.Sprintf("%d symbols (%d defs, %d refs)", count, defs, refs)
	if truncated, ok := result["truncated"].(bool); ok && truncated {
		header += " [truncated]"
	}
	matches := asString(result["matches"])
	if matches == "" {
		return header
	}
	return header + "\n" + capLines(matches, toolResultMaxLines)
}

// formatShellResult renders a run_shell_command result: the tail of the captured
// output plus a non-zero exit code as an error.
func formatShellResult(result map[string]any) (string, *int, bool) {
	output := strings.TrimSpace(asString(result["output"]))
	if output == "" {
		output = "(no output)"
	}
	output = capLines(output, toolResultMaxLines)
	if code, ok := intValue(result["exit_code"]); ok {
		c := code
		return output, &c, c != 0
	}
	return output, nil, false
}

// stringifyResult renders an MCP/structured result value as text: strings pass
// through; everything else is JSON-encoded for a compact preview.
func stringifyResult(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if b, err := json.MarshalIndent(v, "", "  "); err == nil {
		return string(b)
	}
	return fmt.Sprintf("%v", v)
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

// intValue extracts an int from the common numeric types that survive a
// map[string]any round-trip.
func intValue(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}

func lineCount(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

// capLines trims s to the last max lines, prefixing a "… N more lines" note when
// truncated, so a card shows the most recent (most relevant) output.
func capLines(s string, max int) string {
	s = strings.TrimRight(s, "\n")
	if max <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= max {
		return s
	}
	hidden := len(lines) - max
	tail := lines[len(lines)-max:]
	return fmt.Sprintf("… %d more lines\n%s", hidden, strings.Join(tail, "\n"))
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
	case WebFetchToolName:
		if prompt, ok := args[ParamPrompt].(string); ok {
			valid, _ := web.ParsePrompt(prompt)
			if len(valid) > 0 {
				return fmt.Sprintf("fetch %s", valid[0])
			}
		} else if u, ok := args[ParamURL].(string); ok {
			return fmt.Sprintf("fetch %s", u)
		}
	}
	return toolName
}
