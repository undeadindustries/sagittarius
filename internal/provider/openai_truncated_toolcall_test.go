package provider

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func writeFileDecl() ToolDeclaration {
	return ToolDeclaration{
		Name: "write_file",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"file_path": map[string]any{"type": "string"},
				"content":   map[string]any{"type": "string"},
			},
			"required": []string{"file_path", "content"},
		},
	}
}

func readFileDecl() ToolDeclaration {
	return ToolDeclaration{
		Name: "read_file",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"file_path": map[string]any{"type": "string"}},
			"required":   []string{"file_path"},
		},
	}
}

// nameOnlyToolCallSSE is the shape vLLM's qwen3_coder streaming parser produces
// when it drops a large multi-line parameter: the function name arrives, no
// argument delta ever does.
func nameOnlyToolCallSSE(content string) string {
	chunks := []string{}
	if content != "" {
		body, _ := json.Marshal(content)
		chunks = append(chunks,
			`{"id":"1","choices":[{"index":0,"delta":{"content":`+string(body)+`},"finish_reason":null}]}`)
	}
	return sseResponse(append(chunks,
		`{"id":"1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"write_file","arguments":""}}]},"finish_reason":"tool_calls"}]}`,
	)...)
}

// recoveryServer serves streamSSE to the streaming request and nonStreamJSON to
// the non-streaming retry, counting each.
func recoveryServer(t *testing.T, streamSSE, nonStreamJSON string) (*httptest.Server, *int32, *int32) {
	t.Helper()
	var streamed, retried int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		if stream, _ := body["stream"].(bool); stream {
			atomic.AddInt32(&streamed, 1)
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(streamSSE))
			return
		}
		atomic.AddInt32(&retried, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(nonStreamJSON))
	}))
	t.Cleanup(srv.Close)
	return srv, &streamed, &retried
}

func streamToolCalls(t *testing.T, srv *httptest.Server, tools []ToolDeclaration) []ToolCall {
	t.Helper()
	gen, err := NewOpenAIChatGenerator(OpenAIChatConfig{
		BaseURL:    srv.URL + "/v1/chat/completions",
		Model:      "local",
		Bearer:     "test-key",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewOpenAIChatGenerator: %v", err)
	}
	ch, err := gen.GenerateContentStream(testContext(t), &GenerateRequest{
		Messages: []Message{{Role: RoleUser, Parts: []Part{{Text: "hi"}}}},
		Tools:    tools,
	})
	if err != nil {
		t.Fatalf("GenerateContentStream: %v", err)
	}
	var calls []ToolCall
	for _, chunk := range collectStream(t, ch) {
		if chunk.Error != nil {
			t.Fatalf("stream error: %v", chunk.Error)
		}
		calls = append(calls, chunk.ToolCalls...)
	}
	return calls
}

// A tool call that names write_file but carries no arguments is a truncated
// stream, not an empty-argument call. It must be recovered by re-asking without
// streaming rather than handed to the scheduler as an unusable call.
func TestNameOnlyToolCallRetriesWithoutStreaming(t *testing.T) {
	t.Parallel()

	nonStream := `{"id":"1","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"write_file","arguments":"{\"file_path\":\"a.py\",\"content\":\"print(1)\"}"}}]}}]}`
	srv, streamed, retried := recoveryServer(t, nameOnlyToolCallSSE("Let me build this out:"), nonStream)

	calls := streamToolCalls(t, srv, []ToolDeclaration{writeFileDecl()})

	if *retried != 1 {
		t.Fatalf("non-stream retries = %d, want 1 (streamed %d)", *retried, *streamed)
	}
	if len(calls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(calls))
	}
	if got, _ := calls[0].Args["content"].(string); got != "print(1)" {
		t.Errorf("content = %q, want print(1)", got)
	}
	if got, _ := calls[0].Args["file_path"].(string); got != "a.py" {
		t.Errorf("file_path = %q, want a.py", got)
	}
}

// When the engine passes the model's raw tool-call markup through as content,
// the arguments are already in hand and a regeneration would be wasted.
func TestNameOnlyToolCallSalvagesXMLFromContent(t *testing.T) {
	t.Parallel()

	markup := "<tool_call>\n<function=write_file>\n<parameter=file_path>a.py</parameter>\n" +
		"<parameter=content>print(1)</parameter>\n</function>\n</tool_call>"
	srv, _, retried := recoveryServer(t, nameOnlyToolCallSSE(markup), `{"choices":[]}`)

	calls := streamToolCalls(t, srv, []ToolDeclaration{writeFileDecl()})

	if *retried != 0 {
		t.Fatalf("non-stream retries = %d, want 0 (content already carried the call)", *retried)
	}
	if len(calls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(calls))
	}
	if got, _ := calls[0].Args["content"].(string); got != "print(1)" {
		t.Errorf("content = %q, want print(1)", got)
	}
}

// A turn can carry one intact tool call alongside a truncated one. Salvaging the
// truncated one must not discard the intact one.
func TestPartialSalvageKeepsCompleteParallelToolCall(t *testing.T) {
	t.Parallel()

	markup := "<tool_call>\n<function=write_file>\n<parameter=file_path>b.py</parameter>\n" +
		"<parameter=content>print(2)</parameter>\n</function>\n</tool_call>"
	body, _ := json.Marshal(markup)
	sse := sseResponse(
		`{"id":"1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_0","function":{"name":"read_file","arguments":"{\"file_path\":\"a.py\"}"}}]},"finish_reason":null}]}`,
		`{"id":"1","choices":[{"index":0,"delta":{"content":`+string(body)+`},"finish_reason":null}]}`,
		`{"id":"1","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_1","function":{"name":"write_file","arguments":""}}]},"finish_reason":"tool_calls"}]}`,
	)
	srv, _, retried := recoveryServer(t, sse, `{"choices":[]}`)

	calls := streamToolCalls(t, srv, []ToolDeclaration{writeFileDecl(), readFileDecl()})

	if *retried != 0 {
		t.Fatalf("non-stream retries = %d, want 0 (content carried the truncated call)", *retried)
	}
	if len(calls) != 2 {
		t.Fatalf("tool calls = %+v, want 2 (the intact read_file and the salvaged write_file)", calls)
	}
	if calls[0].Name != "read_file" {
		t.Fatalf("calls[0].Name = %q, want read_file", calls[0].Name)
	}
	if got, _ := calls[0].Args["file_path"].(string); got != "a.py" {
		t.Errorf("intact call file_path = %q, want a.py", got)
	}
	if calls[0].ID != "call_0" || calls[1].ID != "call_1" {
		t.Errorf("call ids = %q/%q, want the server-assigned call_0/call_1", calls[0].ID, calls[1].ID)
	}
	if got, _ := calls[1].Args["content"].(string); got != "print(2)" {
		t.Errorf("salvaged content = %q, want print(2)", got)
	}
}

// Markup for a different tool than the truncated one is not the truncated call's
// arguments; it may be prose the model quoted. It must not be emitted as a call,
// and the truncated call still needs a regeneration.
func TestUnrelatedXMLMarkupDoesNotSatisfyTruncatedCall(t *testing.T) {
	t.Parallel()

	markup := "<tool_call>\n<function=read_file>\n<parameter=file_path>elsewhere.py</parameter>\n" +
		"</function>\n</tool_call>"
	nonStream := `{"id":"1","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"write_file","arguments":"{\"file_path\":\"a.py\",\"content\":\"print(1)\"}"}}]}}]}`
	srv, _, retried := recoveryServer(t, nameOnlyToolCallSSE(markup), nonStream)

	calls := streamToolCalls(t, srv, []ToolDeclaration{writeFileDecl(), readFileDecl()})

	if *retried != 1 {
		t.Fatalf("non-stream retries = %d, want 1", *retried)
	}
	if len(calls) != 1 {
		t.Fatalf("tool calls = %+v, want only the regenerated write_file", calls)
	}
	if calls[0].Name != "write_file" {
		t.Errorf("calls[0].Name = %q, want write_file (never the quoted read_file)", calls[0].Name)
	}
}

func TestSalvageMissingToolArgs(t *testing.T) {
	t.Parallel()

	requireArgs := map[string]bool{"write_file": true}
	filled := map[string]any{"file_path": "a.py", "content": "x"}

	t.Run("no incomplete call leaves the slice untouched", func(t *testing.T) {
		t.Parallel()
		calls := []ToolCall{{ID: "c0", Name: "write_file", Args: filled}}
		if !salvageMissingToolArgs(calls, nil, requireArgs) {
			t.Fatal("salvage = false, want true when nothing is missing")
		}
		if len(calls) != 1 || calls[0].Args["content"] != "x" {
			t.Fatalf("calls = %+v, want the original call unchanged", calls)
		}
	})

	t.Run("two truncated calls consume two distinct salvages", func(t *testing.T) {
		t.Parallel()
		calls := []ToolCall{
			{ID: "c0", Name: "write_file"},
			{ID: "c1", Name: "write_file"},
		}
		parsed := []ToolCall{
			{Name: "write_file", Args: map[string]any{"file_path": "a.py", "content": "1"}},
			{Name: "write_file", Args: map[string]any{"file_path": "b.py", "content": "2"}},
		}
		if !salvageMissingToolArgs(calls, parsed, requireArgs) {
			t.Fatal("salvage = false, want true")
		}
		if got, _ := calls[0].Args["file_path"].(string); got != "a.py" {
			t.Errorf("calls[0] file_path = %q, want a.py", got)
		}
		if got, _ := calls[1].Args["file_path"].(string); got != "b.py" {
			t.Errorf("calls[1] file_path = %q, want b.py", got)
		}
	})

	t.Run("one salvage cannot fill two truncated calls", func(t *testing.T) {
		t.Parallel()
		calls := []ToolCall{
			{ID: "c0", Name: "write_file"},
			{ID: "c1", Name: "write_file"},
		}
		parsed := []ToolCall{{Name: "write_file", Args: filled}}
		if salvageMissingToolArgs(calls, parsed, requireArgs) {
			t.Fatal("salvage = true, want false so the caller regenerates")
		}
	})

	t.Run("an empty-argument salvage is not a salvage", func(t *testing.T) {
		t.Parallel()
		calls := []ToolCall{{ID: "c0", Name: "write_file"}}
		parsed := []ToolCall{{Name: "write_file", Args: map[string]any{}}}
		if salvageMissingToolArgs(calls, parsed, requireArgs) {
			t.Fatal("salvage = true, want false for an argument-less parse")
		}
	})

	t.Run("a call needing no arguments is never salvaged over", func(t *testing.T) {
		t.Parallel()
		calls := []ToolCall{{ID: "c0", Name: "get_goal"}}
		parsed := []ToolCall{{Name: "get_goal", Args: filled}}
		if !salvageMissingToolArgs(calls, parsed, requireArgs) {
			t.Fatal("salvage = false, want true")
		}
		if len(calls[0].Args) != 0 {
			t.Fatalf("args = %v, want them left empty", calls[0].Args)
		}
	})
}

// A tool with no required parameters is legitimately called with {}. Retrying
// those would burn a full generation on every such call.
func TestEmptyArgsToolCallWithoutRequiredParamsIsNotRetried(t *testing.T) {
	t.Parallel()

	sse := sseResponse(
		`{"id":"1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"get_goal","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`,
	)
	srv, _, retried := recoveryServer(t, sse, `{"choices":[]}`)

	calls := streamToolCalls(t, srv, []ToolDeclaration{{
		Name:       "get_goal",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{}},
	}})

	if *retried != 0 {
		t.Fatalf("non-stream retries = %d, want 0", *retried)
	}
	if len(calls) != 1 || calls[0].Name != "get_goal" {
		t.Fatalf("calls = %+v, want one get_goal call", calls)
	}
}

// Arguments that arrived but would not decode are AD-114's diagnostic case. The
// model gets a parse error naming the payload, which is more actionable than a
// silent regeneration.
func TestUndecodableToolArgsAreNotRetried(t *testing.T) {
	t.Parallel()

	sse := sseResponse(
		`{"id":"1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"write_file","arguments":"not json at all"}}]},"finish_reason":"tool_calls"}]}`,
	)
	srv, _, retried := recoveryServer(t, sse, `{"choices":[]}`)

	calls := streamToolCalls(t, srv, []ToolDeclaration{writeFileDecl()})

	if *retried != 0 {
		t.Fatalf("non-stream retries = %d, want 0", *retried)
	}
	if len(calls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(calls))
	}
	if _, tagged := calls[0].Args[ToolArgParseErrorKey]; !tagged {
		t.Errorf("args = %v, want a parse-error tag", calls[0].Args)
	}
}

func TestToolsRequiringArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		schema map[string]any
		want   bool
	}{
		{"builtin string slice", map[string]any{"required": []string{"file_path"}}, true},
		{"mcp decoded any slice", map[string]any{"required": []any{"url"}}, true},
		{"empty required", map[string]any{"required": []string{}}, false},
		{"absent required", map[string]any{"type": "object"}, false},
		{"nil schema", nil, false},
		{"unexpected type", map[string]any{"required": "file_path"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := toolsRequiringArgs([]ToolDeclaration{{Name: "t", Parameters: tt.schema}})
			if got["t"] != tt.want {
				t.Errorf("requires args = %v, want %v", got["t"], tt.want)
			}
		})
	}
}
