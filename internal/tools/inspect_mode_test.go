package tools

import (
	"strings"
	"testing"
)

// TestInspectDenialsNameTheExit asserts every deny the read-only posture causes
// tells the user how to lift it. A bare denial gives the model nothing to relay,
// and it has been observed inventing remedies that do not work (restart the
// process, switch interaction mode) — see AD-107.
func TestInspectDenialsNameTheExit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		tool string
		args map[string]any
	}{
		{name: "write_file", tool: WriteFileToolName, args: map[string]any{"file_path": "a.txt", "content": "x"}},
		{name: "edit", tool: EditToolName, args: map[string]any{"file_path": "a.txt"}},
		{name: "save_memory", tool: SaveMemoryToolName, args: map[string]any{SaveMemoryParamText: "x"}},
		{name: "mutating shell", tool: ShellToolName, args: map[string]any{ShellParamCommand: "rm -rf /tmp/x"}},
		{name: "project checks fix", tool: ProjectChecksToolName, args: map[string]any{"fix": true}},
		{name: "mcp tool", tool: "mcp_server_do_thing", args: nil},
		{name: "unknown tool", tool: "some_other_tool", args: nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			allowed, reason := inspectModeAllow(tc.tool, tc.args, nil)
			if allowed {
				t.Fatalf("%s allowed under the inspect gate", tc.tool)
			}
			if !strings.Contains(reason, "/readonly off") {
				t.Fatalf("%s deny reason = %q, want it to name /readonly off", tc.tool, reason)
			}
		})
	}
}

// TestInspectAllowsReadOnlyWork guards the other half: the posture is an
// inspection mode, not a lockout. Read-only tools and read-only shell commands
// must still run, or the gate is just a broken agent.
func TestInspectAllowsReadOnlyWork(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		tool string
		args map[string]any
	}{
		{name: "read_file", tool: ReadFileToolName, args: map[string]any{"file_path": "a.txt"}},
		{name: "grep_search", tool: GrepToolName, args: map[string]any{"pattern": "x"}},
		{name: "read-only shell", tool: ShellToolName, args: map[string]any{ShellParamCommand: "systemctl status nginx"}},
		{name: "check-only project checks", tool: ProjectChecksToolName, args: map[string]any{"fix": false}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if allowed, reason := inspectModeAllow(tc.tool, tc.args, nil); !allowed {
				t.Fatalf("%s denied under the inspect gate: %s", tc.tool, reason)
			}
		})
	}
}
