package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/undeadindustries/sagittarius/internal/diff"
	"github.com/undeadindustries/sagittarius/internal/modes"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/ui"
	"github.com/undeadindustries/sagittarius/internal/web"
)

// MaxToolRounds is the default maximum number of tool-call/response cycles per
// agent turn. It can be overridden via sagittarius.maxToolRounds in
// settings.json (0 = no cap). 100 matches gemini-cli's default; 10 was too low
// for real agentic tasks that write multiple files.
const MaxToolRounds = 100

// maxConcurrentSubagents bounds the scheduler fan-out. Children each hold a
// provider connection and a full agent loop, so this is a resource cap rather
// than a correctness one — write collisions are prevented by lease overlap
// denial, not by serialization.
const maxConcurrentSubagents = 8

// Scheduler executes tool calls from the agent loop.
type Scheduler struct {
	registry    *Registry
	policy      Policy
	interactive bool
	mode        func() modes.Mode
	workspace   *Workspace
	enforce     bool
	snapshotter Snapshotter
	// readOnlyPolicy, when non-nil and returning > PolicyNone, forces read-only tool gating
	// regardless of the active interaction mode (grill mode while
	// interrogating uses PolicyStrict; inspection gate uses PolicyInspect). ask_user is always exempt so the interrogation itself
	// can proceed.
	readOnlyPolicy func() ReadOnlyPolicy

	// lease, when non-nil, bounds every file mutation this scheduler executes
	// to the paths a coding subagent declared. nil means unleased (the parent).
	lease *WriteLease
	// agentID identifies this scheduler's agent to the shared file-state
	// registry. Empty disables read/write tracking for it.
	agentID string
	// fileState is shared with sibling schedulers so a write by one agent can
	// be seen as staleness by another. nil-safe.
	fileState *FileStateRegistry

	// sessionGrants records tools the user approved "for this session" so later
	// invocations of the same tool skip confirmation. Guarded by mu.
	mu            sync.Mutex
	sessionGrants map[string]bool
	grantRecorder func(string)

	beforeHook BeforeToolHookFunc
	afterHook  AfterToolHookFunc
}

// BeforeToolHookFunc is called before executing a tool.
type BeforeToolHookFunc func(ctx context.Context, toolName string, args map[string]any) (modifiedArgs map[string]any, deny bool, reason string, err error)

// AfterToolHookFunc is called after executing a tool.
type AfterToolHookFunc func(ctx context.Context, toolName string, args map[string]any, result map[string]any)

// WithHooks installs lifecycle hook callbacks for BeforeTool and AfterTool.
func WithHooks(before BeforeToolHookFunc, after AfterToolHookFunc) SchedulerOption {
	return func(s *Scheduler) {
		s.beforeHook = before
		s.afterHook = after
	}
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

// WithReadOnlyPolicy installs a signal that, while greater than PolicyNone, forces read-only tool
// gating regardless of the active interaction mode (used by grill mode and inspection gate).
func WithReadOnlyPolicy(fn func() ReadOnlyPolicy) SchedulerOption {
	return func(s *Scheduler) { s.readOnlyPolicy = fn }
}

// WithWriteLease bounds every file mutation to the leased paths. Installing a
// lease also denies tools whose writes cannot be bounded by a path (MCP tools,
// save_memory), because an unbounded write is exactly what the lease exists to
// prevent.
func WithWriteLease(lease WriteLease) SchedulerOption {
	return func(s *Scheduler) { s.lease = &lease }
}

// WithAgentID labels this scheduler's agent in the shared file-state registry.
func WithAgentID(id string) SchedulerOption {
	return func(s *Scheduler) { s.agentID = id }
}

// WithFileState shares a file-state registry across parent and children so
// concurrent reads and writes to the same path are visible to each other.
func WithFileState(reg *FileStateRegistry) SchedulerOption {
	return func(s *Scheduler) { s.fileState = reg }
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
	responses := make([]provider.FunctionResponse, len(calls))

	// Two coding subagents that claim the same path would race each other's
	// writes, so the whole batch is refused before any child is launched.
	conflict := leaseConflict(calls)

	var eg errgroup.Group
	eg.SetLimit(maxConcurrentSubagents)

	for i, call := range calls {
		i, call := i, call
		if !IsSubagentTool(call.Name) {
			continue
		}
		if conflict != "" && canonicalToolName(call.Name) == CodeTaskToolName {
			emit(ui.StreamEvent{
				Type: ui.StreamToolResult, ToolName: call.Name, ToolCallID: call.ID,
				Text: conflict, IsError: true,
			})
			responses[i] = *errorResponse(call, ErrCodeInvalidArgs, conflict)
			continue
		}
		eg.Go(func() error {
			resp, err := s.executeOne(ctx, call, emit)
			if err != nil {
				// A child that dies must not throw away the work its siblings
				// already committed to disk. Record the failure in that child's
				// own slot and let the rest of the batch finish. Cancellation
				// is different: nothing is running any more, so it propagates.
				if ctx.Err() != nil {
					return err
				}
				emit(ui.StreamEvent{
					Type: ui.StreamToolResult, ToolName: call.Name, ToolCallID: call.ID,
					Text: err.Error(), IsError: true,
				})
				responses[i] = *errorResponseFromErr(call, err)
				return nil
			}
			if resp != nil {
				responses[i] = *resp
			}
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return filterValidResponses(responses), err
	}

	for i, call := range calls {
		if IsSubagentTool(call.Name) {
			continue
		}
		resp, err := s.executeOne(ctx, call, emit)
		if err != nil {
			return filterValidResponses(responses), err
		}
		if resp != nil {
			responses[i] = *resp
		}
	}

	return filterValidResponses(responses), nil
}

// leaseConflict reports the first pair of code_task calls in a batch whose
// declared write paths could name the same file, as a model-facing message.
// A lease that will not parse is skipped: the tool itself reports that, with a
// better error than this function could produce.
func leaseConflict(calls []provider.ToolCall) string {
	type claim struct {
		label string
		lease WriteLease
	}
	var claims []claim
	for _, call := range calls {
		if canonicalToolName(call.Name) != CodeTaskToolName {
			continue
		}
		args := NormalizeToolArgs(call.Name, call.Args)
		lease, err := ParseWriteLease(args[CodeTaskParamWritePaths])
		if err != nil {
			continue
		}
		label, _ := args[TaskParamDescription].(string)
		if label == "" {
			label = call.ID
		}
		claims = append(claims, claim{label: label, lease: lease})
	}
	for i := range claims {
		for j := i + 1; j < len(claims); j++ {
			pair, overlaps := LeasesOverlap(claims[i].lease, claims[j].lease)
			if !overlaps {
				continue
			}
			return fmt.Sprintf(
				"overlapping write leases: %q and %q both claim %s. "+
					"Subagents run in parallel, so no two may write the same path. "+
					"Split the work so each lease is disjoint, or run them in separate turns.",
				claims[i].label, claims[j].label, pair)
		}
	}
	return ""
}

func filterValidResponses(in []provider.FunctionResponse) []provider.FunctionResponse {
	var out []provider.FunctionResponse
	for _, r := range in {
		if r.Name != "" {
			out = append(out, r)
		}
	}
	return out
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
	// Rename model-emitted aliases onto the schema keys before anything reads
	// them, so the boundary gate, mode gate, confirm card, hooks, snapshots,
	// and history all see one set of names.
	args = NormalizeToolArgs(name, args)
	call.Args = args

	emitErr := func(reason string) {
		emit(ui.StreamEvent{Type: ui.StreamToolResult, ToolName: name, ToolCallID: id, Text: reason, IsError: true})
	}

	if msg, bad := toolArgParseError(args); bad {
		emitErr(msg)
		return errorResponse(call, ErrCodeInvalidArgs, msg), nil
	}

	emit(ui.StreamEvent{Type: ui.StreamToolStart, ToolName: name, ToolCallID: id, Text: formatToolSummary(name, args)})

	// The lease gate runs on the normalized arguments so an aliased key
	// (AD-113) cannot smuggle a path past it.
	if code, reason, ok := s.leaseAllow(name, args); !ok {
		emitErr(reason)
		return errorResponse(call, code, reason), nil
	}

	// Project-boundary gate runs before the interaction-mode gate so it applies
	// in every mode (the protected-snapshot guard is always active; out-of-root
	// blocking is gated on enforce).
	if allowed, reason := ProjectBoundaryAllow(s.enforce, name, args, s.workspace); !allowed {
		emitErr(reason)
		return errorResponse(call, ErrCodeProjectBoundary, reason), nil
	}

	if allowed, reason := s.interactionModeAllow(name, args); !allowed {
		emitErr(reason)
		return errorResponse(call, ErrCodeModeRestriction, reason), nil
	}

	tool, ok := s.registry.Lookup(name)
	if !ok {
		errText := fmt.Sprintf("unknown tool %q", name)
		emitErr(errText)
		return errorResponse(call, ErrCodeUnknownTool, errText), nil
	}

	if canonicalToolName(name) == WriteFileToolName {
		if err := validateWriteFileArgs(args); err != nil {
			emitErr(err.Error())
			return errorResponse(call, ErrCodeInvalidArgs, err.Error()), nil
		}
	}

	next, code, reason, allowed := s.applyBeforeToolHook(ctx, name, args)
	if !allowed {
		emitErr(reason)
		return errorResponse(call, code, reason), nil
	}
	args = next
	call.Args = next

	needsConfirm := s.policy.NeedsConfirmation(tool)
	grantKey := tool.Name()
	isEscalation := false
	if s.readOnlyPolicy != nil && shellVerdictEscalates(s.readOnlyPolicy()) && canonicalToolName(name) == ShellToolName {
		if cmd, err := stringArg(args, ShellParamCommand); err == nil {
			verdict, _ := ClassifyShellReadOnly(cmd)
			if verdict == VerdictUnknown {
				needsConfirm = true
				isEscalation = true
				tokens := strings.Fields(cmd)
				if len(tokens) > 0 {
					base := commandBase(tokens[0])
					if base == "sudo" && len(tokens) > 1 {
						base = "sudo " + commandBase(tokens[1])
					}
					grantKey = "escalation:" + base
				}
			}
		}
	}

	if needsConfirm && !s.sessionGranted(grantKey) {
		approved, err := s.requestApproval(ctx, tool.Name(), grantKey, id, args, emit, isEscalation)
		if err != nil {
			return nil, err
		}
		if !approved {
			errText := "user denied tool execution"
			emitErr(errText)
			return errorResponse(call, ErrCodeUserDenied, errText), nil
		}
	}

	// Hold the target path for the whole read-modify-write region so a sibling
	// agent cannot land a write between the diff, the snapshot, and the edit.
	mutAbs := s.mutationTarget(name, args)
	staleBy := ""
	if mutAbs != "" {
		defer s.fileState.LockPath(mutAbs)()
		staleBy, _ = s.fileState.CheckStale(s.agentID, mutAbs)
	}

	// Compute the mutation diff (before -> after) before the tool mutates the
	// file so the result line can show exactly what changed.
	writeDiff := ""
	if IsFileMutatingTool(name) {
		writeDiff = s.mutationDiff(name, args)
	}

	// Snapshot the prior state of a write target before the tool mutates it.
	snapAbs := s.snapshotTarget(name, args)
	if snapAbs != "" {
		s.snapshotter.CaptureWrite(snapAbs)
	}

	var result map[string]any
	var err error

	switch t := tool.(type) {
	case BatchTool:
		result, err = t.ExecuteBatch(ctx, args, s.runNested)
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
		return errorResponseFromErr(call, err), nil
	}

	if snapAbs != "" {
		s.snapshotter.CommitWrite(snapAbs, canonicalToolName(name))
	}
	s.recordFileAccess(name, args, mutAbs, staleBy, result)

	resultText, exitCode, isErr := formatToolResult(name, result, writeDiff)
	emit(ui.StreamEvent{
		Type:       ui.StreamToolResult,
		ToolName:   name,
		ToolCallID: id,
		Text:       resultText,
		ExitCode:   exitCode,
		IsError:    isErr,
	})
	if s.afterHook != nil {
		s.afterHook(ctx, name, args, result)
	}
	return &provider.FunctionResponse{Name: name, CallID: call.ID, Response: result}, nil
}

// snapshotTarget returns the resolved absolute path a write_file call will
// mutate, or "" when snapshotting does not apply (no snapshotter, not a
// write_file, missing/invalid path, or path outside the workspace).
func (s *Scheduler) snapshotTarget(name string, args map[string]any) string {
	if s.snapshotter == nil {
		return ""
	}
	return s.mutationTarget(name, args)
}

// mutationTarget resolves the absolute path a file-mutating call will change,
// or "" when the call does not mutate one resolvable path.
func (s *Scheduler) mutationTarget(name string, args map[string]any) string {
	if s.workspace == nil || !IsFileMutatingTool(name) {
		return ""
	}
	return s.resolveArgPath(args, ParamFilePath)
}

func (s *Scheduler) resolveArgPath(args map[string]any, key string) string {
	if s.workspace == nil {
		return ""
	}
	p, err := stringArg(args, key)
	if err != nil {
		return ""
	}
	abs, err := s.workspace.ResolvePath(p)
	if err != nil {
		return ""
	}
	return abs
}

// leaseAllow enforces a coding subagent's write lease.
//
// Beyond the leased paths themselves it denies tools whose side effects a path
// lease cannot describe. Letting those through would leave the lease as
// decoration: a child could reach any file via an MCP server, or write global
// memory, while its write_file calls stayed dutifully in bounds.
func (s *Scheduler) leaseAllow(name string, args map[string]any) (ErrorCode, string, bool) {
	if s.lease == nil {
		return "", "", true
	}
	c := canonicalToolName(name)
	if !IsFileMutatingTool(c) {
		if c == SaveMemoryToolName || strings.HasPrefix(c, "mcp_") {
			return ErrCodeModeRestriction, fmt.Sprintf(
				"subagent: tool %q is not available because its effects cannot be bounded by a write lease; report what you need instead", name), false
		}
		if c == ProjectChecksToolName && projectChecksFixRequested(args) {
			return ErrCodeModeRestriction, "subagent: run_project_checks fix mode is not available because formatter/fix mutations cannot be bounded by a write lease; run check-only (fix=false) or have the parent run checks with fix", false
		}
		return "", "", true
	}
	raw, err := stringArg(args, ParamFilePath)
	if err != nil {
		return ErrCodeInvalidArgs, err.Error(), false
	}
	if s.workspace == nil {
		return ErrCodeModeRestriction, "write lease: no workspace is configured, so no path can be verified against the lease", false
	}
	// A path that will not resolve inside the workspace is outside the lease by
	// definition; reporting it as a lease denial keeps one message for one rule.
	rel, err := s.workspace.RelativePath(raw)
	if err != nil {
		rel = raw
	}
	if err != nil || !s.lease.Allows(rel) {
		return ErrCodeModeRestriction, fmt.Sprintf(
			"write lease: %s is outside this subagent's lease (%s). "+
				"Do not work around this — report the change the parent needs to make and stop.",
			rel, s.lease), false
	}
	return "", "", true
}

// recordFileAccess publishes this call's file access to the shared registry and
// attaches a staleness note when a sibling changed the file after this agent
// last read it. Staleness is a warning, not a failure: the write the user
// approved has already happened, and the model can usually reconcile.
func (s *Scheduler) recordFileAccess(name string, args map[string]any, mutAbs, staleBy string, result map[string]any) {
	if mutAbs != "" {
		s.fileState.RecordWrite(s.agentID, mutAbs)
		if staleBy != "" && result != nil {
			result["stale_warning"] = fmt.Sprintf(
				"%s was modified by %s after you last read it; re-read it before making further changes",
				filepath.Base(mutAbs), staleBy)
		}
		return
	}
	if canonicalToolName(name) == ReadFileToolName {
		s.fileState.RecordRead(s.agentID, s.resolveArgPath(args, ParamFilePath))
	}
}

func (s *Scheduler) requestApproval(
	ctx context.Context,
	toolName string,
	grantKey string,
	callID string,
	args map[string]any,
	emit func(ui.StreamEvent),
	isEscalation bool,
) (bool, error) {
	tool, ok := s.registry.Lookup(toolName)
	if !ok {
		return false, nil
	}

	if !s.interactive {
		if isEscalation {
			return false, nil // deny without prompting when headless
		}
		return s.policy.HeadlessApprove(tool), nil
	}

	replyCh := make(chan ui.ConfirmDecision, 1)
	emit(ui.StreamEvent{
		Type:         ui.StreamToolConfirm,
		ToolName:     toolName,
		ToolCallID:   callID,
		Text:         formatConfirmSummary(toolName, args),
		Diff:         s.mutationDiff(toolName, args),
		ConfirmReply: replyCh,
	})

	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case decision := <-replyCh:
		switch decision {
		case ui.ConfirmSession:
			s.grantSession(grantKey)
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
func (s *Scheduler) mutationDiff(name string, args map[string]any) string {
	if s.workspace == nil {
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
	before := ""
	if b, readErr := os.ReadFile(abs); readErr == nil {
		before = string(b)
	}

	var after string
	c := canonicalToolName(name)
	switch c {
	case WriteFileToolName:
		content, err := stringArg(args, WriteFileParamContent)
		if err != nil {
			return ""
		}
		after = content
	case EditToolName:
		oldStr, _ := args[EditParamOldString].(string)
		newStr, _ := args[EditParamNewString].(string)
		replaceAll, _ := args[EditParamReplaceAll].(bool)

		hasCRLF := strings.Contains(before, "\r\n")
		oldStr = normalizeLineEndings(oldStr, hasCRLF)
		newStr = normalizeLineEndings(newStr, hasCRLF)

		match, matchedOld, matchErr := findMatch(before, oldStr, replaceAll)
		if matchErr != nil || !match {
			// If match fails, return empty diff so we don't preview a broken edit
			return ""
		}
		if replaceAll {
			after = strings.ReplaceAll(before, matchedOld, newStr)
		} else {
			after = strings.Replace(before, matchedOld, newStr, 1)
		}
	default:
		return ""
	}

	return diff.UnifiedDiff(before, after, filepath.Base(path))
}

func errorResponse(call provider.ToolCall, code ErrorCode, message string) *provider.FunctionResponse {
	return &provider.FunctionResponse{
		Name:     call.Name,
		CallID:   call.ID,
		Response: (&ToolError{Code: code, Message: message}).ToResponse(),
	}
}

func errorResponseFromErr(call provider.ToolCall, err error) *provider.FunctionResponse {
	var te *ToolError
	if errors.As(err, &te) {
		return &provider.FunctionResponse{
			Name:     call.Name,
			CallID:   call.ID,
			Response: te.ToResponse(),
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return errorResponse(call, ErrCodeExecutionTimeout, err.Error())
	}
	return errorResponse(call, ErrCodeToolCrash, err.Error())
}

// validateHookRewrite re-applies the pre-execution gates to arguments a
// BeforeTool hook rewrote. Without this a hook could redirect a write outside
// the workspace, or past the read-only gate, since the original checks saw only
// the model's arguments.
func (s *Scheduler) applyBeforeToolHook(ctx context.Context, name string, args map[string]any) (map[string]any, ErrorCode, string, bool) {
	if s == nil || s.beforeHook == nil {
		return args, "", "", true
	}
	modArgs, deny, reason, hookErr := s.beforeHook(ctx, name, args)
	if hookErr != nil {
		slog.Warn("before tool hook error", "tool", name, "error", hookErr)
	}
	if deny {
		if reason == "" {
			reason = "tool execution denied by hook"
		}
		return args, ErrCodeHookDenied, reason, false
	}
	if len(modArgs) == 0 {
		return args, "", "", true
	}
	// Merge rather than replace: a hook that returns only the keys it
	// cares about must not silently drop the rest of the call.
	merged := make(map[string]any, len(args)+len(modArgs))
	for k, v := range args {
		merged[k] = v
	}
	for k, v := range modArgs {
		merged[k] = v
	}
	// The gates above ran against the pre-hook arguments, so a rewritten
	// path or content has not been checked yet. Re-run them.
	if code, reason, allowed := s.validateHookRewrite(name, merged); !allowed {
		return merged, code, reason, false
	}
	return merged, "", "", true
}

func (s *Scheduler) runNested(ctx context.Context, name string, args map[string]any) (map[string]any, error) {
	if args == nil {
		args = map[string]any{}
	}
	args = NormalizeToolArgs(name, args)
	if msg, bad := toolArgParseError(args); bad {
		return nil, &ToolError{Code: ErrCodeInvalidArgs, Message: msg}
	}
	canon := canonicalToolName(name)
	if canon == ScriptToolName || IsSubagentTool(canon) {
		return nil, &ToolError{
			Code:    ErrCodeModeRestriction,
			Message: fmt.Sprintf("tool %q is not allowed in scripts", canon),
		}
	}
	if code, reason, ok := s.leaseAllow(name, args); !ok {
		return nil, &ToolError{Code: code, Message: reason}
	}
	if allowed, reason := ProjectBoundaryAllow(s.enforce, name, args, s.workspace); !allowed {
		return nil, &ToolError{Code: ErrCodeProjectBoundary, Message: reason}
	}
	if allowed, reason := s.interactionModeAllow(name, args); !allowed {
		return nil, &ToolError{Code: ErrCodeModeRestriction, Message: reason}
	}
	tool, ok := s.registry.Lookup(name)
	if !ok {
		return nil, &ToolError{Code: ErrCodeUnknownTool, Message: fmt.Sprintf("unknown tool %q", name)}
	}
	if tool.RequiresConfirmation() {
		return nil, &ToolError{Code: ErrCodeUserDenied, Message: "cannot be confirmed inside a batch"}
	}
	next, code, reason, allowed := s.applyBeforeToolHook(ctx, name, args)
	if !allowed {
		return nil, &ToolError{Code: code, Message: reason}
	}
	result, err := tool.Execute(ctx, next)
	if err != nil {
		return nil, err
	}
	if s.afterHook != nil {
		s.afterHook(ctx, name, next, result)
	}
	return result, nil
}

func (s *Scheduler) validateHookRewrite(name string, args map[string]any) (ErrorCode, string, bool) {
	if code, reason, ok := s.leaseAllow(name, args); !ok {
		return code, reason, false
	}
	if allowed, reason := ProjectBoundaryAllow(s.enforce, name, args, s.workspace); !allowed {
		return ErrCodeProjectBoundary, reason, false
	}
	if allowed, reason := s.interactionModeAllow(name, args); !allowed {
		return ErrCodeModeRestriction, reason, false
	}
	if canonicalToolName(name) == WriteFileToolName {
		if err := validateWriteFileArgs(args); err != nil {
			return ErrCodeInvalidArgs, err.Error(), false
		}
	}
	return "", "", true
}

func (s *Scheduler) interactionModeAllow(toolName string, args map[string]any) (bool, string) {
	if s.readOnlyPolicy != nil {
		switch s.readOnlyPolicy() {
		case PolicyStrict:
			if allowed, reason := grillModeAllow(canonicalToolName(toolName), args); !allowed {
				return false, reason
			}
		case PolicyInspect:
			if allowed, reason := inspectModeAllow(canonicalToolName(toolName), args); !allowed {
				return false, reason
			}
		case PolicyShellInspect:
			if allowed, reason := shellInspectAllow(canonicalToolName(toolName), args); !allowed {
				return false, reason
			}
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
			return appendSpillHint(capLines(matches, toolResultMaxLines), result), nil, false
		}
	case FindSymbolToolName:
		return formatFindSymbolResult(result), nil, false
	case SaveMemoryToolName:
		if path, ok := result["path"].(string); ok {
			return fmt.Sprintf("Saved to %s", path), nil, false
		}
	case CodeTaskToolName:
		return formatCodeTaskResult(result), nil, false
	}

	// MCP tools (and any other tool) carry their payload under "result".
	if v, ok := result["result"]; ok {
		return capLines(stringifyResult(v), toolResultMaxLines), nil, false
	}
	return "ok", nil, false
}

// formatCodeTaskResult renders a coding subagent's card: what it wrote, any
// staleness warning, then its report. The file list leads because a lease that
// touched the wrong thing is the failure the user most needs to catch.
func formatCodeTaskResult(result map[string]any) string {
	var parts []string
	if written := stringSlice(result["files_written"]); len(written) > 0 {
		parts = append(parts, fmt.Sprintf("Wrote %d file(s): %s", len(written), strings.Join(written, ", ")))
	} else {
		parts = append(parts, "No files written")
	}
	if warn := asString(result["stale_warnings"]); warn != "" {
		parts = append(parts, warn)
	}
	if text := strings.TrimSpace(asString(result["result"])); text != "" {
		parts = append(parts, text)
	}
	return capLines(strings.Join(parts, "\n"), toolResultMaxLines)
}

// stringSlice reads a []string or a []any of strings, since a value can reach
// here either directly from a built-in tool or round-tripped through JSON.
func stringSlice(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
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
	return appendSpillHint(header+"\n"+capLines(matches, toolResultMaxLines), result)
}

// formatShellResult renders a run_shell_command result: the tail of the captured
// output plus a non-zero exit code as an error.
// FormatShellResult renders a run_shell_command result for a tool card.
func FormatShellResult(result map[string]any) (string, *int, bool) {
	return formatShellResult(result)
}

func formatShellResult(result map[string]any) (string, *int, bool) {
	output := strings.TrimSpace(asString(result["output"]))
	if output == "" {
		output = "(no output)"
	}
	output = appendSpillHint(capLines(output, toolResultMaxLines), result)
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
	case SaveMemoryToolName:
		if text, ok := args[SaveMemoryParamText].(string); ok {
			return fmt.Sprintf("remember: %s", text)
		}
	case CodeTaskToolName:
		// The lease is the whole decision the user is being asked to make, so it
		// goes in the summary rather than the description of the task.
		lease, err := ParseWriteLease(args[CodeTaskParamWritePaths])
		if err != nil {
			return fmt.Sprintf("%s (invalid write_paths)", toolName)
		}
		desc, _ := args[TaskParamDescription].(string)
		if desc == "" {
			desc = "coding subagent"
		}
		return fmt.Sprintf("%s — may write %s", desc, lease.String())
	}
	return toolName
}
