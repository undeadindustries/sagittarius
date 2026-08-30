package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

func leaseFor(t *testing.T, patterns ...string) WriteLease {
	t.Helper()
	l, err := ParseWriteLease(patterns)
	if err != nil {
		t.Fatalf("ParseWriteLease(%v): %v", patterns, err)
	}
	return l
}

func writeCall(path, content string) provider.ToolCall {
	return provider.ToolCall{
		Name: WriteFileToolName,
		Args: map[string]any{ParamFilePath: path, WriteFileParamContent: content},
	}
}

func runOne(t *testing.T, s *Scheduler, call provider.ToolCall) map[string]any {
	t.Helper()
	resp, err := s.Execute(context.Background(), []provider.ToolCall{call}, func(ui.StreamEvent) {})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(resp) != 1 {
		t.Fatalf("expected one response, got %d", len(resp))
	}
	return resp[0].Response
}

func TestLeaseGateAllowsInsideAndDeniesOutside(t *testing.T) {
	ws := newTestWorkspace(t)
	sched := NewScheduler(NewBuiltinRegistry(ws), Policy{Mode: ApprovalYolo}, false, nil, ws,
		WithWriteLease(leaseFor(t, "owned/**")))

	if got := runOne(t, sched, writeCall("owned/a.go", "package a")); got["error"] != nil {
		t.Fatalf("leased write was denied: %v", got)
	}
	if _, err := os.Stat(filepath.Join(ws.Root(), "owned/a.go")); err != nil {
		t.Fatalf("leased write did not land: %v", err)
	}

	got := runOne(t, sched, writeCall("other/b.go", "package b"))
	errText, _ := got["error"].(string)
	if !strings.Contains(errText, "outside this subagent's lease") {
		t.Fatalf("expected lease denial, got %v", got)
	}
	if got["code"] != string(ErrCodeModeRestriction) {
		t.Fatalf("code = %v, want MODE_RESTRICTION", got["code"])
	}
	if _, err := os.Stat(filepath.Join(ws.Root(), "other/b.go")); !os.IsNotExist(err) {
		t.Fatal("unleased write reached disk")
	}
}

// TestLeaseGateSeesAliasedArguments pins the ordering against AD-113: the gate
// must run on normalized arguments or a model emitting {path, text} would slip
// an unleased write past it.
func TestLeaseGateSeesAliasedArguments(t *testing.T) {
	ws := newTestWorkspace(t)
	sched := NewScheduler(NewBuiltinRegistry(ws), Policy{Mode: ApprovalYolo}, false, nil, ws,
		WithWriteLease(leaseFor(t, "owned/**")))

	got := runOne(t, sched, provider.ToolCall{
		Name: WriteFileToolName,
		Args: map[string]any{"path": "other/b.go", "text": "package b"},
	})
	if errText, _ := got["error"].(string); !strings.Contains(errText, "lease") {
		t.Fatalf("aliased unleased write was not denied: %v", got)
	}
}

func TestLeaseGateDeniesEscapingAndUnleasableTools(t *testing.T) {
	ws := newTestWorkspace(t)
	sched := NewScheduler(NewBuiltinRegistry(ws), Policy{Mode: ApprovalYolo}, false, nil, ws,
		WithWriteLease(leaseFor(t, "owned/**")))

	outside := filepath.Join(filepath.Dir(ws.Root()), "escape.txt")
	got := runOne(t, sched, writeCall(outside, "x"))
	if errText, _ := got["error"].(string); !strings.Contains(errText, "lease") {
		t.Fatalf("out-of-workspace write was not denied by the lease gate: %v", got)
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Fatal("out-of-workspace write reached disk")
	}

	// save_memory writes outside any path lease, so a leased agent cannot call it.
	got = runOne(t, sched, provider.ToolCall{
		Name: SaveMemoryToolName,
		Args: map[string]any{SaveMemoryParamText: "remember"},
	})
	if errText, _ := got["error"].(string); !strings.Contains(errText, "write lease") {
		t.Fatalf("save_memory was not denied under a lease: %v", got)
	}
}

func TestUnleasedSchedulerIsUnrestricted(t *testing.T) {
	ws := newTestWorkspace(t)
	sched := NewScheduler(NewBuiltinRegistry(ws), Policy{Mode: ApprovalYolo}, false, nil, ws)

	if got := runOne(t, sched, writeCall("anywhere/a.go", "x")); got["error"] != nil {
		t.Fatalf("unleased scheduler denied a write: %v", got)
	}
}

func TestLeaseConflictMessage(t *testing.T) {
	t.Parallel()

	codeTask := func(desc string, paths ...string) provider.ToolCall {
		anyPaths := make([]any, len(paths))
		for i, p := range paths {
			anyPaths[i] = p
		}
		return provider.ToolCall{
			Name: CodeTaskToolName,
			Args: map[string]any{
				TaskParamDescription:    desc,
				TaskParamPrompt:         "do it",
				CodeTaskParamWritePaths: anyPaths,
			},
		}
	}

	tests := []struct {
		name     string
		calls    []provider.ToolCall
		conflict bool
	}{
		{
			name:  "disjoint leases pass",
			calls: []provider.ToolCall{codeTask("a", "pkg/a/**"), codeTask("b", "pkg/b/**")},
		},
		{
			name:     "identical leases collide",
			calls:    []provider.ToolCall{codeTask("a", "pkg/a/**"), codeTask("b", "pkg/a/**")},
			conflict: true,
		},
		{
			name:     "nested lease collides",
			calls:    []provider.ToolCall{codeTask("a", "pkg/**"), codeTask("b", "pkg/a/x.go")},
			conflict: true,
		},
		{
			name:  "single call never collides",
			calls: []provider.ToolCall{codeTask("a", "pkg/**")},
		},
		{
			name:  "unparseable lease is left to the tool",
			calls: []provider.ToolCall{codeTask("a"), codeTask("b", "pkg/**")},
		},
		{
			name:  "non-subagent calls are ignored",
			calls: []provider.ToolCall{writeCall("a.go", "x"), writeCall("a.go", "y")},
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			msg := leaseConflict(tc.calls)
			if (msg != "") != tc.conflict {
				t.Fatalf("leaseConflict = %q, want conflict=%v", msg, tc.conflict)
			}
			if tc.conflict && !strings.Contains(msg, "overlapping write leases") {
				t.Fatalf("conflict message does not name the problem: %q", msg)
			}
		})
	}
}

func TestShellInspectAllowsReadOnlyDeniesMutating(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cmd     string
		allowed bool
	}{
		{name: "read-only inspection", cmd: "git status", allowed: true},
		{name: "mutating redirect", cmd: "echo hi > /etc/hosts"},
		{name: "in-place sed", cmd: "sed -i s/a/b/ file.go"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			allowed, reason := shellInspectAllow(ShellToolName, map[string]any{ShellParamCommand: tc.cmd})
			if allowed != tc.allowed {
				t.Fatalf("shellInspectAllow(%q) = %v (%s), want %v", tc.cmd, allowed, reason, tc.allowed)
			}
		})
	}

	// Writes are the lease's job, not this policy's.
	if allowed, reason := shellInspectAllow(WriteFileToolName, nil); !allowed {
		t.Fatalf("shell-inspect blocked write_file: %s", reason)
	}
}

func TestLeaseGateDeniesProjectChecksFix(t *testing.T) {
	ws := newTestWorkspace(t)
	sched := NewScheduler(NewBuiltinRegistry(ws, WithAllowFix(true)), Policy{Mode: ApprovalYolo}, false, nil, ws,
		WithWriteLease(leaseFor(t, "owned/**")))

	// check-only is read-only and allowed under lease
	gotCheck := runOne(t, sched, provider.ToolCall{
		Name: ProjectChecksToolName,
		Args: map[string]any{ProjectChecksParamFix: false},
	})
	if gotCheck["error"] != nil {
		t.Fatalf("check-only project checks was denied under lease: %v", gotCheck)
	}

	// fix=true is mutating and cannot be bounded by lease, so it must be denied
	gotFix := runOne(t, sched, provider.ToolCall{
		Name: ProjectChecksToolName,
		Args: map[string]any{ProjectChecksParamFix: true},
	})
	errText, _ := gotFix["error"].(string)
	if !strings.Contains(errText, "run_project_checks fix mode is not available") {
		t.Fatalf("project checks fix=true was not denied under lease: %v", gotFix)
	}
	if gotFix["code"] != string(ErrCodeModeRestriction) {
		t.Fatalf("code = %v, want MODE_RESTRICTION", gotFix["code"])
	}
}

func TestLeaseGateSymlinkResolution(t *testing.T) {
	ws := newTestWorkspace(t)
	root := ws.Root()

	targetDir := filepath.Join(root, "beta")
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		t.Fatal(err)
	}
	linkDir := filepath.Join(root, "alpha")
	if err := os.Symlink(targetDir, linkDir); err != nil {
		t.Fatal(err)
	}

	// Lease is for "alpha/**". The actual write goes to beta/new.txt via symlink,
	// which is outside "alpha/**", so it must be denied!
	schedAlpha := NewScheduler(NewBuiltinRegistry(ws), Policy{Mode: ApprovalYolo}, false, nil, ws,
		WithWriteLease(leaseFor(t, "alpha/**")))

	got := runOne(t, schedAlpha, writeCall("alpha/new.txt", "content"))
	errText, _ := got["error"].(string)
	if !strings.Contains(errText, "outside this subagent's lease") {
		t.Fatalf("write through symlink outside lease was not denied: %v", got)
	}
	if _, err := os.Stat(filepath.Join(targetDir, "new.txt")); !os.IsNotExist(err) {
		t.Fatal("unleased symlink write reached disk")
	}

	// Lease is for "beta/**". The write to "alpha/new.txt" resolves to "beta/new.txt"
	// which is inside "beta/**", so it must be allowed.
	schedBeta := NewScheduler(NewBuiltinRegistry(ws), Policy{Mode: ApprovalYolo}, false, nil, ws,
		WithWriteLease(leaseFor(t, "beta/**")))

	gotAllowed := runOne(t, schedBeta, writeCall("alpha/new.txt", "content"))
	if gotAllowed["error"] != nil {
		t.Fatalf("leased write via symlink was denied: %v", gotAllowed)
	}
	if _, err := os.Stat(filepath.Join(targetDir, "new.txt")); err != nil {
		t.Fatalf("leased write via symlink did not reach disk: %v", err)
	}
}

func TestValidateHookRewriteEnforcesLease(t *testing.T) {
	ws := newTestWorkspace(t)
	before := func(_ context.Context, name string, _ map[string]any) (map[string]any, bool, string, error) {
		if name == WriteFileToolName {
			// Hook tries to rewrite target path outside the lease
			return map[string]any{ParamFilePath: "unowned/rewrite.go", WriteFileParamContent: "package unowned"}, false, "", nil
		}
		return nil, false, "", nil
	}

	sched := NewScheduler(NewBuiltinRegistry(ws), Policy{Mode: ApprovalYolo}, false, nil, ws,
		WithWriteLease(leaseFor(t, "owned/**")),
		WithHooks(before, nil))

	got := runOne(t, sched, writeCall("owned/valid.go", "package owned"))
	errText, _ := got["error"].(string)
	if !strings.Contains(errText, "outside this subagent's lease") {
		t.Fatalf("hook rewrite outside lease was not denied: %v", got)
	}
	if _, err := os.Stat(filepath.Join(ws.Root(), "unowned/rewrite.go")); !os.IsNotExist(err) {
		t.Fatal("hook rewritten write reached disk outside lease")
	}
}
