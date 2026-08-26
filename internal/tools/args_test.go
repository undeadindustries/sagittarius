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

func TestNormalizeToolArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tool string
		in   map[string]any
		want map[string]any
	}{
		{
			name: "qwen write_file shape",
			tool: WriteFileToolName,
			in:   map[string]any{"path": "a.txt", "text": "hi"},
			want: map[string]any{ParamFilePath: "a.txt", WriteFileParamContent: "hi"},
		},
		{
			name: "canonical content wins over alias",
			tool: WriteFileToolName,
			in:   map[string]any{ParamFilePath: "a.txt", WriteFileParamContent: "real", "text": "decoy"},
			want: map[string]any{ParamFilePath: "a.txt", WriteFileParamContent: "real"},
		},
		{
			name: "empty canonical content still wins",
			tool: WriteFileToolName,
			in:   map[string]any{ParamFilePath: "a.txt", WriteFileParamContent: "", "contents": "decoy"},
			want: map[string]any{ParamFilePath: "a.txt", WriteFileParamContent: ""},
		},
		{
			name: "first alias in precedence order wins",
			tool: WriteFileToolName,
			in:   map[string]any{ParamFilePath: "a.txt", "data": "second", "contents": "first"},
			want: map[string]any{ParamFilePath: "a.txt", WriteFileParamContent: "first"},
		},
		{
			name: "path is a file for write_file",
			tool: WriteFileToolName,
			in:   map[string]any{"path": "a.txt", "content": "hi"},
			want: map[string]any{ParamFilePath: "a.txt", WriteFileParamContent: "hi"},
		},
		{
			name: "path is a directory for list_directory",
			tool: ListDirectoryToolName,
			in:   map[string]any{"path": "internal"},
			want: map[string]any{ParamDirPath: "internal"},
		},
		{
			name: "shell cmd alias",
			tool: ShellToolName,
			in:   map[string]any{"cmd": "echo hi", ShellParamIsBackground: true},
			want: map[string]any{ShellParamCommand: "echo hi", ShellParamIsBackground: true},
		},
		{
			name: "edit old and new aliases",
			tool: EditToolName,
			in:   map[string]any{"filePath": "a.go", "old_text": "a", "new_text": "b"},
			want: map[string]any{ParamFilePath: "a.go", EditParamOldString: "a", EditParamNewString: "b"},
		},
		{
			name: "legacy tool-name alias resolves to the shell table",
			tool: "run_shell",
			in:   map[string]any{"shell_command": "ls"},
			want: map[string]any{ShellParamCommand: "ls"},
		},
		{
			name: "unknown keys are preserved",
			tool: ReadFileToolName,
			in:   map[string]any{"path": "a.txt", ReadFileParamStartLine: 3},
			want: map[string]any{ParamFilePath: "a.txt", ReadFileParamStartLine: 3},
		},
		{
			name: "mcp tool is left alone",
			tool: "mcp_mempalace_write_file",
			in:   map[string]any{"path": "a.txt", "text": "hi"},
			want: map[string]any{"path": "a.txt", "text": "hi"},
		},
		{
			name: "tool without a table is left alone",
			tool: GrepToolName,
			in:   map[string]any{"path": "internal", ParamPattern: "x"},
			want: map[string]any{"path": "internal", ParamPattern: "x"},
		},
		{
			name: "empty args",
			tool: WriteFileToolName,
			in:   map[string]any{},
			want: map[string]any{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := NormalizeToolArgs(tc.tool, tc.in)
			assertArgs(t, got, tc.want)

			// Idempotent: a second pass must be a no-op, so applying it at more
			// than one seam is safe.
			assertArgs(t, NormalizeToolArgs(tc.tool, got), tc.want)
		})
	}
}

// The caller's map is shared with history and the UI card, so normalization
// must copy rather than rename keys in place.
func TestNormalizeToolArgsDoesNotMutateInput(t *testing.T) {
	t.Parallel()
	in := map[string]any{"path": "a.txt", "text": "hi"}
	NormalizeToolArgs(WriteFileToolName, in)
	if _, ok := in["path"]; !ok {
		t.Fatal("input map was mutated: path removed")
	}
	if _, ok := in[ParamFilePath]; ok {
		t.Fatal("input map was mutated: file_path added")
	}
}

// NormalizeToolArgs iterates the per-tool table as a map, so two canonical keys
// sharing an alias would resolve nondeterministically. Keep the lists disjoint.
func TestBuiltinArgAliasesAreDisjointPerTool(t *testing.T) {
	t.Parallel()
	for tool, table := range builtinArgAliases {
		owner := map[string]string{}
		for canonical, aliases := range table {
			for _, alias := range aliases {
				if alias == canonical {
					t.Errorf("%s: alias %q duplicates its own canonical key", tool, alias)
				}
				if prev, dup := owner[alias]; dup {
					t.Errorf("%s: alias %q claimed by both %q and %q", tool, alias, prev, canonical)
				}
				owner[alias] = canonical
			}
		}
		for canonical := range table {
			if other, clash := owner[canonical]; clash {
				t.Errorf("%s: canonical key %q is also an alias of %q", tool, canonical, other)
			}
		}
	}
}

// A missing parameter must name what the model actually sent so it can correct
// the next call instead of retrying the same JSON.
func TestMissingParamErrorListsReceivedKeys(t *testing.T) {
	t.Parallel()
	_, err := stringArg(map[string]any{"target": "a.txt", "blob": "hi"}, ParamFilePath)
	if err == nil {
		t.Fatal("expected an error")
	}
	want := `missing required parameter "file_path". received: [blob target]. schema expects: "file_path"`
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestMissingParamErrorWithNoArgs(t *testing.T) {
	t.Parallel()
	_, err := presentStringArg(map[string]any{}, WriteFileParamContent)
	if err == nil {
		t.Fatal("expected an error")
	}
	want := `missing required parameter "content". received: []. schema expects: "content"`
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

// End to end through the scheduler: the gates, confirm preview, and the tool
// itself all read canonical keys, so a Qwen-shaped call must write the file.
func TestSchedulerWritesFileFromAliasedArgs(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	scheduler := NewScheduler(NewBuiltinRegistry(ws), Policy{Mode: ApprovalYolo}, false, nil, ws)

	calls := []provider.ToolCall{
		{Name: WriteFileToolName, Args: map[string]any{"path": "aliased.txt", "text": "from qwen\n"}},
	}
	responses, err := scheduler.Execute(context.Background(), calls, func(ui.StreamEvent) {})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(responses) != 1 {
		t.Fatalf("responses = %d, want 1", len(responses))
	}
	if errText, bad := responses[0].Response["error"]; bad {
		t.Fatalf("tool errored: %v", errText)
	}
	got, err := os.ReadFile(filepath.Join(root, "aliased.txt"))
	if err != nil {
		t.Fatalf("aliased write missing: %v", err)
	}
	if string(got) != "from qwen\n" {
		t.Fatalf("content = %q, want %q", string(got), "from qwen\n")
	}
}

// The normalized args must reach the recorded call so history, snapshots, and
// the tool card show schema names rather than whatever the model invented.
func TestSchedulerShellRunsFromCmdAlias(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	scheduler := NewScheduler(NewBuiltinRegistry(ws), Policy{Mode: ApprovalYolo}, false, nil, ws)

	var summary string
	emit := func(ev ui.StreamEvent) {
		if ev.Type == ui.StreamToolStart {
			summary = ev.Text
		}
	}
	calls := []provider.ToolCall{
		{Name: ShellToolName, Args: map[string]any{"cmd": "echo aliased-shell"}},
	}
	responses, err := scheduler.Execute(context.Background(), calls, emit)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(responses) != 1 {
		t.Fatalf("responses = %d, want 1", len(responses))
	}
	out, _ := responses[0].Response["output"].(string)
	if !strings.Contains(out, "aliased-shell") {
		t.Fatalf("output = %q, want it to contain %q", out, "aliased-shell")
	}
	if !strings.Contains(summary, "echo aliased-shell") {
		t.Fatalf("tool start summary = %q, want the normalized command", summary)
	}
}

// Without a content key under any name the call is unrecoverable, so the error
// has to be the diagnostic one.
func TestSchedulerMissingContentReportsReceivedKeys(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	scheduler := NewScheduler(NewBuiltinRegistry(ws), Policy{Mode: ApprovalYolo}, false, nil, ws)

	calls := []provider.ToolCall{
		{Name: WriteFileToolName, Args: map[string]any{"path": "a.txt", "note": "oops"}},
	}
	responses, err := scheduler.Execute(context.Background(), calls, func(ui.StreamEvent) {})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	errText, _ := responses[0].Response["error"].(string)
	if !strings.Contains(errText, `received: [file_path note]`) {
		t.Fatalf("error = %q, want it to list the received keys", errText)
	}
	if code, _ := responses[0].Response["code"].(string); code != string(ErrCodeInvalidArgs) {
		t.Fatalf("code = %q, want %q", code, ErrCodeInvalidArgs)
	}
}

// write_file legitimately creates empty files; only a missing key is an error.
func TestSchedulerWritesEmptyContent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	scheduler := NewScheduler(NewBuiltinRegistry(ws), Policy{Mode: ApprovalYolo}, false, nil, ws)

	calls := []provider.ToolCall{
		{Name: WriteFileToolName, Args: map[string]any{ParamFilePath: "empty.txt", WriteFileParamContent: ""}},
	}
	responses, err := scheduler.Execute(context.Background(), calls, func(ui.StreamEvent) {})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if errText, bad := responses[0].Response["error"]; bad {
		t.Fatalf("tool errored: %v", errText)
	}
	got, err := os.ReadFile(filepath.Join(root, "empty.txt"))
	if err != nil {
		t.Fatalf("empty write missing: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("content = %q, want empty", string(got))
	}
}

// A streamed write_file payload with literal newlines inside the JSON string
// used to decode as {} and die as "received: []". Salvage must recover it.
func TestSchedulerWritesFileFromRawNewlineJSON(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	scheduler := NewScheduler(NewBuiltinRegistry(ws), Policy{Mode: ApprovalYolo}, false, nil, ws)

	raw := "{\"file_path\":\"recovered.py\",\"content\":\"def foo():\n    return 1\n\"}"
	calls := []provider.ToolCall{
		{Name: WriteFileToolName, Args: provider.UnmarshalToolArguments(raw)},
	}
	responses, err := scheduler.Execute(context.Background(), calls, func(ui.StreamEvent) {})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if errText, bad := responses[0].Response["error"]; bad {
		t.Fatalf("tool errored: %v", errText)
	}
	got, err := os.ReadFile(filepath.Join(root, "recovered.py"))
	if err != nil {
		t.Fatalf("write missing: %v", err)
	}
	want := "def foo():\n    return 1\n"
	if string(got) != want {
		t.Fatalf("content = %q, want %q", string(got), want)
	}
}

func TestSchedulerReportsJSONParseError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	scheduler := NewScheduler(NewBuiltinRegistry(ws), Policy{Mode: ApprovalYolo}, false, nil, ws)

	args := provider.UnmarshalToolArguments(`{"file_path":"a.txt","content":"hi"`)
	calls := []provider.ToolCall{{Name: WriteFileToolName, Args: args}}
	responses, err := scheduler.Execute(context.Background(), calls, func(ui.StreamEvent) {})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	errText, _ := responses[0].Response["error"].(string)
	if !strings.Contains(errText, "failed to parse tool arguments as JSON") {
		t.Fatalf("error = %q, want a JSON parse diagnostic", errText)
	}
	if !strings.Contains(errText, "raw arguments:") {
		t.Fatalf("error = %q, want a raw-arguments preview", errText)
	}
	if strings.Contains(errText, `received: []`) {
		t.Fatalf("error = %q, must not pretend the model sent no keys", errText)
	}
	if code, _ := responses[0].Response["code"].(string); code != string(ErrCodeInvalidArgs) {
		t.Fatalf("code = %q, want %q", code, ErrCodeInvalidArgs)
	}
}

func assertArgs(t *testing.T, got, want map[string]any) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("args = %v, want %v", got, want)
	}
	for k, wantV := range want {
		gotV, ok := got[k]
		if !ok {
			t.Fatalf("args = %v, missing key %q", got, k)
		}
		if gotV != wantV {
			t.Fatalf("args[%q] = %v, want %v", k, gotV, wantV)
		}
	}
}
