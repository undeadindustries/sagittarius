package tools

import (
	"path/filepath"
	"testing"
)

func newTestWorkspace(t *testing.T) *Workspace {
	t.Helper()
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	return ws
}

func TestProjectBoundaryAllow(t *testing.T) {
	ws := newTestWorkspace(t)
	outside := filepath.Join(filepath.Dir(ws.Root()), "escape.txt")
	protected := filepath.Join(ws.Root(), ".sagittarius", "snapshots", "x.jsonl")

	cases := []struct {
		name    string
		enforce bool
		tool    string
		args    map[string]any
		want    bool
	}{
		{"write inside, enforce on", true, WriteFileToolName,
			map[string]any{ParamFilePath: "in.txt"}, true},
		{"write absolute outside, enforce on", true, WriteFileToolName,
			map[string]any{ParamFilePath: outside}, false},
		{"write ../ outside, enforce on", true, WriteFileToolName,
			map[string]any{ParamFilePath: "../escape.txt"}, false},
		{"write outside, enforce off", false, WriteFileToolName,
			map[string]any{ParamFilePath: outside}, true},
		{"protected path always blocked, enforce off", false, WriteFileToolName,
			map[string]any{ParamFilePath: protected}, false},
		{"shell write outside, enforce on", true, ShellToolName,
			map[string]any{ShellParamCommand: "echo hi > /tmp/escape.txt"}, false},
		{"shell write inside, enforce on", true, ShellToolName,
			map[string]any{ShellParamCommand: "echo hi > notes.txt"}, true},
		{"shell read outside, enforce on", true, ShellToolName,
			map[string]any{ShellParamCommand: "cat /etc/hosts"}, true},
		{"read tool always allowed", true, ReadFileToolName,
			map[string]any{ParamFilePath: outside}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := ProjectBoundaryAllow(tc.enforce, tc.tool, tc.args, ws)
			if got != tc.want {
				t.Fatalf("allowed = %v (%q), want %v", got, reason, tc.want)
			}
		})
	}
}

func TestShellMutatesOutsideRoot(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		name    string
		command string
		want    bool
	}{
		{"redirect to absolute", "echo x > /tmp/out.txt", true},
		{"append redirect to absolute", "echo x >> /etc/hosts", true},
		{"fd redirect to absolute", "make 2> /var/log/build.log", true},
		{"redirect inside", "echo x > build.log", false},
		{"rm parent escape", "rm ../secret", true},
		{"rm inside", "rm build/output.o", false},
		{"mv dest outside", "mv local.txt /tmp/local.txt", true},
		{"cp source outside dest inside", "cp /etc/hosts ./hosts", false},
		{"mkdir absolute outside", "mkdir /opt/thing", true},
		{"tee outside", "echo x | tee /etc/motd", true},
		{"sed in place outside", "sed -i 's/a/b/' /etc/hosts", true},
		{"sed read only", "sed 's/a/b/' /etc/hosts", false},
		{"home escape", "rm ~/important", true},
		{"chained inside", "mkdir build && rm build/old.o", false},
		{"plain read", "cat README.md", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, target := ShellMutatesOutsideRoot(tc.command, root)
			if got != tc.want {
				t.Fatalf("ShellMutatesOutsideRoot(%q) = %v (%q), want %v",
					tc.command, got, target, tc.want)
			}
		})
	}
}

func TestIsProtectedWritePath(t *testing.T) {
	root := "/proj"
	cases := []struct {
		path string
		want bool
	}{
		{"/proj/.sagittarius/snapshots/s.jsonl", true},
		{"/proj/.sagittarius/snapshots", true},
		{"/proj/.sagittarius/settings.json", false},
		{"/proj/src/main.go", false},
	}
	for _, tc := range cases {
		if got := IsProtectedWritePath(root, tc.path); got != tc.want {
			t.Errorf("IsProtectedWritePath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}
