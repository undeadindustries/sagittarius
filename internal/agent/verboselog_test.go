package agent

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/tools"
)

// nopWriteCloser adapts a bytes.Buffer (which has no Close) to io.WriteCloser
// so tests can inspect the transcript after the run without touching disk.
type nopWriteCloser struct {
	*bytes.Buffer
}

func (nopWriteCloser) Close() error { return nil }

// TestVerboseLogRoundTrip drives a full request/tool-call/response turn with
// --log-verbose enabled and asserts the transcript captures the request, the
// model's tool call, the tool result, and the final answer text — the
// information a bug report needs to reproduce an issue.
func TestVerboseLogRoundTrip(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "data.txt"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}

	gen := &fakeGenerator{
		batches: [][]provider.StreamResponse{
			{
				{ToolCalls: []provider.ToolCall{{
					Name: tools.ReadFileToolName,
					Args: map[string]any{tools.ParamFilePath: "data.txt"},
				}}},
				{Done: true},
			},
			{
				{TextDelta: "read ok"},
				{Done: true},
			},
		},
	}

	var buf bytes.Buffer
	runner, err := NewRunner(RunnerConfig{
		Generator:    gen,
		Model:        "test-model",
		WorkDir:      root,
		ApprovalMode: ApprovalYolo,
		Interactive:  false,
		VerboseLog:   nopWriteCloser{&buf},
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	collectEvents(t, mustRunTurn(t, runner, "read the file"))

	transcript := buf.String()
	for _, want := range []string{
		"IN  user message",
		"read the file",
		"OUT request (round 0)",
		"IN  model response (round 0)",
		tools.ReadFileToolName,
		"OUT tool_result " + tools.ReadFileToolName,
		"IN  model response (round 1)",
		"read ok",
	} {
		if !strings.Contains(transcript, want) {
			t.Errorf("verbose transcript missing %q:\n%s", want, transcript)
		}
	}

	if err := runner.Close(); err != nil {
		t.Fatalf("runner.Close: %v", err)
	}
}

// TestVerboseLogDisabledIsNilSafe asserts that every verboseLog method (and
// Runner.Close) is a safe no-op when --log-verbose is off, i.e. VerboseLog is
// left nil — this is the default, hot-path case and must not panic or block.
func TestVerboseLogDisabledIsNilSafe(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	gen := &fakeGenerator{batches: [][]provider.StreamResponse{{{TextDelta: "ok"}, {Done: true}}}}

	runner, err := NewRunner(RunnerConfig{
		Generator:   gen,
		Model:       "test-model",
		WorkDir:     root,
		Interactive: false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	collectEvents(t, mustRunTurn(t, runner, "hi"))

	if err := runner.Close(); err != nil {
		t.Fatalf("runner.Close on disabled verbose log: %v", err)
	}
}

// TestVerboseLogNilMethodsNoop exercises every verboseLog method directly on
// a nil receiver to document and enforce the nil-safety contract call sites
// in runAgentLoop rely on.
func TestVerboseLogNilMethodsNoop(t *testing.T) {
	t.Parallel()

	var v *verboseLog
	v.LogTurnStart("hi")
	v.LogRequest(0, []byte("{}"), nil)
	v.LogRequest(0, nil, errors.New("boom"))
	v.LogResponse(0, "text", nil, nil)
	v.LogToolResult(provider.FunctionResponse{Name: "tool"})
	v.LogError(errors.New("boom"))
	v.LogInfo("note")
	if err := v.Close(); err != nil {
		t.Fatalf("nil verboseLog.Close() = %v, want nil", err)
	}
}
