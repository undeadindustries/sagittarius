package contextmgmt

import (
	"encoding/json"
	"strings"
	"testing"
)

const writeFileTool = "write_file"

func bigContent() string { return strings.Repeat("x", 8_000) }

func textEntry(role string, text string) Message {
	r := RoleUser
	if role == "model" {
		r = RoleModel
	}
	return Message{Role: r, Parts: []Part{{Text: text}}}
}

func writeFileCall(filePath, content string) Message {
	return Message{Role: RoleModel, Parts: []Part{{FunctionCall: &ToolCall{
		ID:   "call-1",
		Name: writeFileTool,
		Args: map[string]any{"file_path": filePath, "content": content},
	}}}}
}

func defaultEjectionOpts() WriteFileEjectionOptions {
	return WriteFileEjectionOptions{
		WriteFileToolName: writeFileTool,
		ExemptTools:       map[string]bool{},
		ProtectLatestTurn: true,
		MinAgeTurns:       1,
		MinTokensPerCall:  100,
	}
}

func callContent(m Message) string {
	if len(m.Parts) == 0 || m.Parts[0].FunctionCall == nil {
		return ""
	}
	if v, ok := m.Parts[0].FunctionCall.Args["content"].(string); ok {
		return v
	}
	return ""
}

func TestEjectPreservesLeadingEntries(t *testing.T) {
	t.Parallel()
	history := []Message{
		writeFileCall("/leading.txt", bigContent()),
		writeFileCall("/leading2.txt", bigContent()),
		textEntry("user", "something"),
		textEntry("model", "something else"),
		textEntry("user", "and another"),
		textEntry("model", "final"),
	}
	res := EjectStaleWriteFileContent(history, defaultEjectionOpts())
	if got := callContent(res.NewHistory[0]); got != bigContent() {
		t.Errorf("leading[0] content modified")
	}
	if got := callContent(res.NewHistory[1]); got != bigContent() {
		t.Errorf("leading[1] content modified")
	}
}

func TestEjectStaleContent(t *testing.T) {
	t.Parallel()
	history := []Message{
		textEntry("user", "lead 1"),
		textEntry("model", "lead 2"),
		writeFileCall("/foo.ts", bigContent()),
		textEntry("model", "response"),
		textEntry("user", "next turn"),
		textEntry("model", "latest turn"),
	}
	res := EjectStaleWriteFileContent(history, defaultEjectionOpts())
	if res.EjectedCount != 1 {
		t.Fatalf("EjectedCount = %d, want 1", res.EjectedCount)
	}
	// The content arg is dropped entirely (not replaced with a marker) so the
	// model has no value to copy into its next write_file call.
	if _, present := res.NewHistory[2].Parts[0].FunctionCall.Args["content"]; present {
		t.Errorf("content arg still present after ejection; want it dropped")
	}
	if path := res.NewHistory[2].Parts[0].FunctionCall.Args["file_path"]; path != "/foo.ts" {
		t.Errorf("file_path = %v, want /foo.ts", path)
	}
	if res.TokensSaved <= 0 {
		t.Errorf("TokensSaved = %d, want > 0", res.TokensSaved)
	}
}

func TestEjectProtectsLatestTurn(t *testing.T) {
	t.Parallel()
	history := []Message{
		textEntry("user", "lead 1"),
		textEntry("model", "lead 2"),
		writeFileCall("/late.ts", bigContent()),
	}
	res := EjectStaleWriteFileContent(history, defaultEjectionOpts())
	if res.EjectedCount != 0 {
		t.Fatalf("EjectedCount = %d, want 0", res.EjectedCount)
	}
	if got := callContent(res.NewHistory[2]); got != bigContent() {
		t.Errorf("latest turn content modified")
	}
}

func TestEjectSkipsSmallPayloads(t *testing.T) {
	t.Parallel()
	history := []Message{
		textEntry("user", "lead 1"),
		textEntry("model", "lead 2"),
		writeFileCall("/small.ts", "short"),
		textEntry("model", "response"),
		textEntry("user", "next turn"),
	}
	opts := defaultEjectionOpts()
	opts.MinTokensPerCall = 1_000
	res := EjectStaleWriteFileContent(history, opts)
	if res.EjectedCount != 0 {
		t.Errorf("EjectedCount = %d, want 0", res.EjectedCount)
	}
}

func TestEjectIdempotent(t *testing.T) {
	t.Parallel()
	history := []Message{
		textEntry("user", "lead 1"),
		textEntry("model", "lead 2"),
		writeFileCall("/foo.ts", bigContent()),
		textEntry("model", "response"),
		textEntry("user", "next turn"),
		textEntry("model", "latest"),
	}
	first := EjectStaleWriteFileContent(history, defaultEjectionOpts())
	if first.EjectedCount != 1 {
		t.Fatalf("first EjectedCount = %d, want 1", first.EjectedCount)
	}
	second := EjectStaleWriteFileContent(first.NewHistory, defaultEjectionOpts())
	if second.EjectedCount != 0 {
		t.Errorf("second EjectedCount = %d, want 0", second.EjectedCount)
	}
}

func TestEjectRespectsExemptTools(t *testing.T) {
	t.Parallel()
	history := []Message{
		textEntry("user", "lead 1"),
		textEntry("model", "lead 2"),
		writeFileCall("/foo.ts", bigContent()),
		textEntry("model", "response"),
		textEntry("user", "next turn"),
	}
	opts := defaultEjectionOpts()
	opts.ExemptTools = map[string]bool{writeFileTool: true}
	res := EjectStaleWriteFileContent(history, opts)
	if res.EjectedCount != 0 {
		t.Errorf("EjectedCount = %d, want 0", res.EjectedCount)
	}
}

func TestEjectDoesNotMutateInput(t *testing.T) {
	t.Parallel()
	history := []Message{
		textEntry("user", "lead 1"),
		textEntry("model", "lead 2"),
		writeFileCall("/foo.ts", bigContent()),
		textEntry("model", "response"),
		textEntry("user", "next turn"),
	}
	before, _ := json.Marshal(history)
	EjectStaleWriteFileContent(history, defaultEjectionOpts())
	after, _ := json.Marshal(history)
	if string(before) != string(after) {
		t.Errorf("input history was mutated")
	}
}
