package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAnthropicRequestBodyGolden(t *testing.T) {
	t.Parallel()

	temp := 0.5
	req := &GenerateRequest{
		Model:             "claude-opus-5-5",
		SystemInstruction: "You are Sagittarius.",
		Temperature:       &temp,
		IncludeThoughts:   true,
		Reasoning:         &ReasoningRequest{Enabled: true, Effort: "high"},
		Tools: []ToolDeclaration{{
			Name:        "read_file",
			Description: "Read a file",
			Parameters:  map[string]any{"type": "object", "properties": map[string]any{"file_path": map[string]any{"type": "string"}}},
		}},
		Messages: []Message{
			{Role: RoleUser, Parts: []Part{{Text: "earlier question"}}},
			{Role: RoleUser, Parts: []Part{{Text: "more context"}}},
			{Role: RoleModel, Parts: []Part{
				{Reasoning: "earlier thought", ThoughtSignature: []byte("sig-old")},
				{Text: "earlier answer"},
			}},
			{Role: RoleUser, Parts: []Part{{Text: "inspect main.go"}}},
			{Role: RoleModel, Parts: []Part{
				{Reasoning: "", ThoughtSignature: []byte("sig-omitted")},
				{RedactedThinking: "opaque-data"},
				{FunctionCall: &ToolCall{
					ID:   "toolu_1",
					Name: "read_file",
					Args: map[string]any{"file_path": "main.go", InternalCallIDArg: "toolu_1"},
				}},
			}},
			{Role: RoleUser, Parts: []Part{{FunctionResponse: &FunctionResponse{
				CallID:   "toolu_1",
				Name:     "read_file",
				Response: map[string]any{"error": "not found"},
			}}, {Text: "try again"}}},
		},
		KeepReasoningHistory: false, // earlier turn omitted; active tool loop preserved
	}

	plan := BuildAnthropicRequestPlan(req, AnthropicPlanOptions{
		ToolsEnabled:     true,
		ThinkingTypes:    []string{"adaptive"},
		SupportedEfforts: []string{"low", "medium", "high", "xhigh", "max"},
	})
	raw, err := json.Marshal(plan.Body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if _, hasTemp := got["temperature"]; hasTemp {
		t.Fatalf("temperature must be omitted on 2026-10-21, got %v", got["temperature"])
	}
	if int(got["max_tokens"].(float64)) != anthropicDefaultMaxTokens {
		t.Fatalf("max_tokens = %v, want %d", got["max_tokens"], anthropicDefaultMaxTokens)
	}
	if cc, ok := got["cache_control"].(map[string]any); !ok || cc["type"] != "ephemeral" {
		t.Fatalf("top-level cache_control = %#v, want ephemeral", got["cache_control"])
	}
	sysBlocks := got["system"].([]any)
	sys0 := sysBlocks[0].(map[string]any)
	if cc, ok := sys0["cache_control"].(map[string]any); !ok || cc["type"] != "ephemeral" {
		t.Fatalf("system[0].cache_control = %#v, want ephemeral", sys0["cache_control"])
	}
	thinking := got["thinking"].(map[string]any)
	if thinking["type"] != "adaptive" || thinking["display"] != "summarized" {
		t.Fatalf("thinking = %#v, want adaptive + summarized", thinking)
	}
	outputCfg := got["output_config"].(map[string]any)
	if outputCfg["effort"] != "high" {
		t.Fatalf("output_config = %#v, want effort=high", outputCfg)
	}

	// Verify messages:
	// 0: user ("earlier question")
	// 1: assistant ("earlier answer", earlier thinking dropped since KeepReasoningHistory=false)
	// 2: user ("inspect main.go")
	// 3: assistant (active turn: omitted thinking with explicit "", redacted_thinking, tool_use without _call_id)
	// 4: user (tool_result + "try again", with tool_result first and is_error=true)
	msgs := got["messages"].([]any)
	if len(msgs) != 5 {
		t.Fatalf("messages len = %d, want 5 (%s)", len(msgs), string(raw))
	}
	earlierAsst := msgs[1].(map[string]any)["content"].([]any)
	if len(earlierAsst) != 1 || earlierAsst[0].(map[string]any)["type"] != "text" {
		t.Fatalf("earlier assistant should omit thinking when KeepReasoningHistory=false: %#v", earlierAsst)
	}

	activeAsst := msgs[3].(map[string]any)["content"].([]any)
	if len(activeAsst) != 3 {
		t.Fatalf("active assistant blocks = %d, want 3: %#v", len(activeAsst), activeAsst)
	}
	thinkBlk := activeAsst[0].(map[string]any)
	if _, hasThinkingKey := thinkBlk["thinking"]; !hasThinkingKey || thinkBlk["thinking"] != "" || thinkBlk["signature"] != "sig-omitted" {
		t.Fatalf("omitted thinking block must keep empty string thinking and signature: %#v", thinkBlk)
	}
	if activeAsst[1].(map[string]any)["type"] != "redacted_thinking" || activeAsst[1].(map[string]any)["data"] != "opaque-data" {
		t.Fatalf("redacted_thinking block = %#v", activeAsst[1])
	}
	toolBlk := activeAsst[2].(map[string]any)
	toolInput := toolBlk["input"].(map[string]any)
	if _, leaked := toolInput[InternalCallIDArg]; leaked {
		t.Fatalf("_call_id leaked into tool_use input: %#v", toolInput)
	}

	mergedUser := msgs[4].(map[string]any)["content"].([]any)
	if len(mergedUser) != 2 {
		t.Fatalf("merged user blocks = %d, want 2: %#v", len(mergedUser), mergedUser)
	}
	if mergedUser[0].(map[string]any)["type"] != "tool_result" || mergedUser[0].(map[string]any)["is_error"] != true {
		t.Fatalf("first block must be tool_result with is_error=true: %#v", mergedUser[0])
	}
	if mergedUser[1].(map[string]any)["type"] != "text" || mergedUser[1].(map[string]any)["text"] != "try again" {
		t.Fatalf("second block = %#v, want text try again", mergedUser[1])
	}
}

func TestAnthropicBudgetMaxTokensRaisedAboveThinkingBudget(t *testing.T) {
	t.Parallel()
	plan := BuildAnthropicRequestPlan(&GenerateRequest{
		Model:                "claude-sonnet-4-5",
		ThinkingBudgetTokens: 32_000,
		Reasoning:            &ReasoningRequest{Enabled: true, Effort: "high"},
		Messages:             []Message{{Role: RoleUser, Parts: []Part{{Text: "hi"}}}},
	}, AnthropicPlanOptions{})
	if plan.Body.Thinking == nil || plan.Body.Thinking.Type != "enabled" || plan.Body.Thinking.BudgetTokens != 32_000 {
		t.Fatalf("thinking = %#v, want enabled with budget 32000", plan.Body.Thinking)
	}
	if plan.Body.MaxTokens <= plan.Body.Thinking.BudgetTokens {
		t.Fatalf("max_tokens = %d must exceed budget_tokens = %d", plan.Body.MaxTokens, plan.Body.Thinking.BudgetTokens)
	}
}

func TestAnthropicSSEReplayAndModelParts(t *testing.T) {
	t.Parallel()

	sse := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_1","model":"claude-opus-5-5","usage":{"input_tokens":100,"cache_read_input_tokens":400,"cache_creation_input_tokens":50,"output_tokens":1}}}`,
		``,
		`event: ping`,
		`data: {"type":"ping"}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"step 1"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig-1"}}`,
		`data: {"type":"content_block_stop","index":0}`,
		``,
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"redacted_thinking","data":"enc-1"}}`,
		`data: {"type":"content_block_stop","index":1}`,
		``,
		`data: {"type":"content_block_start","index":2,"content_block":{"type":"text","text":""}}`,
		`data: {"type":"content_block_delta","index":2,"delta":{"type":"text_delta","text":"Checking"}}`,
		`data: {"type":"content_block_stop","index":2}`,
		``,
		`data: {"type":"content_block_start","index":3,"content_block":{"type":"tool_use","id":"toolu_9","name":"read_file","input":{}}}`,
		`data: {"type":"content_block_delta","index":3,"delta":{"type":"input_json_delta","partial_json":"{\"file_path\":"}}`,
		`data: {"type":"content_block_delta","index":3,"delta":{"type":"input_json_delta","partial_json":"\"go.mod\"}"}}`,
		`data: {"type":"content_block_stop","index":3}`,
		``,
		`data: {"type":"future_event","x":1}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":42}}`,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")

	state := NewAnthropicSseMapperState()
	var chunks []StreamResponse
	err := scanAnthropicSSE(strings.NewReader(sse), state, nil, func(sr StreamResponse) bool {
		chunks = append(chunks, sr)
		return true
	})
	if err != nil {
		t.Fatalf("scanAnthropicSSE: %v", err)
	}

	var reasoning, text string
	var calls []ToolCall
	var usage *Usage
	var modelParts []Part
	var finish string
	var done bool
	for _, c := range chunks {
		reasoning += c.ReasoningDelta
		text += c.TextDelta
		calls = append(calls, c.ToolCalls...)
		if c.Usage != nil {
			usage = c.Usage
		}
		if len(c.ModelParts) > 0 {
			modelParts = c.ModelParts
		}
		if c.FinishReason != "" {
			finish = c.FinishReason
		}
		if c.Done {
			done = true
		}
	}

	if reasoning != "step 1" || text != "Checking" || !done || finish != "" {
		t.Fatalf("reasoning=%q text=%q finish=%q done=%v", reasoning, text, finish, done)
	}
	if len(calls) != 1 || calls[0].ID != "toolu_9" || calls[0].Args["file_path"] != "go.mod" {
		t.Fatalf("calls = %#v", calls)
	}
	if usage == nil || usage.InputTokens != 550 || usage.CachedTokens != 400 || usage.OutputTokens != 42 || !usage.CachedReported {
		t.Fatalf("usage = %#v, want Input=550 Cached=400 Output=42 CachedReported=true", usage)
	}
	if len(modelParts) != 4 {
		t.Fatalf("modelParts len = %d, want 4: %#v", len(modelParts), modelParts)
	}
	if modelParts[0].Reasoning != "step 1" || string(modelParts[0].ThoughtSignature) != "sig-1" {
		t.Fatalf("part[0] = %#v", modelParts[0])
	}
	if modelParts[1].RedactedThinking != "enc-1" {
		t.Fatalf("part[1] = %#v", modelParts[1])
	}
	if modelParts[2].Text != "Checking" {
		t.Fatalf("part[2] = %#v", modelParts[2])
	}
	if modelParts[3].FunctionCall == nil || modelParts[3].FunctionCall.ID != "toolu_9" {
		t.Fatalf("part[3] = %#v", modelParts[3])
	}
}

func TestAnthropicStopReasonsAndStreamErrors(t *testing.T) {
	t.Parallel()

	t.Run("max_tokens and refusal surface abnormal FinishReason", func(t *testing.T) {
		t.Parallel()
		for _, reason := range []string{"max_tokens", "refusal"} {
			state := NewAnthropicSseMapperState()
			_, _ = MapAnthropicSseEvent(AnthropicSseEvent{
				Type:  "message_delta",
				Delta: &anthropicSseDelta{StopReason: reason},
			}, state)
			chunks, err := MapAnthropicSseEvent(AnthropicSseEvent{Type: "message_stop"}, state)
			if err != nil {
				t.Fatalf("%s: unexpected err: %v", reason, err)
			}
			if chunks[0].FinishReason != reason {
				t.Fatalf("%s: FinishReason = %q, want %q", reason, chunks[0].FinishReason, reason)
			}
		}
	})

	t.Run("model_context_window_exceeded maps to ErrContextOverflow", func(t *testing.T) {
		t.Parallel()
		state := NewAnthropicSseMapperState()
		_, _ = MapAnthropicSseEvent(AnthropicSseEvent{
			Type:  "message_delta",
			Delta: &anthropicSseDelta{StopReason: "model_context_window_exceeded"},
		}, state)
		_, err := MapAnthropicSseEvent(AnthropicSseEvent{Type: "message_stop"}, state)
		if !errors.Is(err, ErrContextOverflow) {
			t.Fatalf("err = %v, want ErrContextOverflow", err)
		}
	})

	t.Run("overloaded_error maps to ErrProviderOverloaded", func(t *testing.T) {
		t.Parallel()
		_, err := MapAnthropicSseEvent(AnthropicSseEvent{
			Type:  "error",
			Error: &anthropicSseErrorObj{Type: "overloaded_error", Message: "Overloaded"},
		}, NewAnthropicSseMapperState())
		if !errors.Is(err, ErrProviderOverloaded) {
			t.Fatalf("err = %v, want ErrProviderOverloaded", err)
		}
	})
}

func TestAnthropicHeadersPreStreamRetryAndCancel(t *testing.T) {
	t.Parallel()

	t.Run("sends required headers and workspace id when set, retries 529 once", func(t *testing.T) {
		t.Parallel()
		var attempts atomic.Int32
		var gotAuth, gotVer, gotWS string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n := attempts.Add(1)
			gotAuth = r.Header.Get("Authorization")
			gotVer = r.Header.Get("anthropic-version")
			gotWS = r.Header.Get("anthropic-workspace-id")
			if n == 1 {
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(529)
				_, _ = w.Write([]byte(`{"error":{"type":"overloaded_error","message":"Overloaded"}}`))
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"ok\"}}\n\n" +
				"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":2}}\n\n" +
				"data: {\"type\":\"message_stop\"}\n\n"))
		}))
		t.Cleanup(srv.Close)

		gen, err := NewAnthropicGenerator(AnthropicConfig{
			BaseURL:     srv.URL + "/v1/messages",
			Model:       "claude-opus-5-5",
			Bearer:      "sk-ant-test",
			WorkspaceID: "wrkspc_01ABC",
			HTTPClient:  srv.Client(),
		})
		if err != nil {
			t.Fatalf("NewAnthropicGenerator: %v", err)
		}
		ch, err := gen.GenerateContentStream(testContext(t), &GenerateRequest{
			Messages: []Message{{Role: RoleUser, Parts: []Part{{Text: "hi"}}}},
		})
		if err != nil {
			t.Fatalf("GenerateContentStream: %v", err)
		}
		var text string
		for sr := range ch {
			if sr.Error != nil {
				t.Fatalf("stream error: %v", sr.Error)
			}
			text += sr.TextDelta
		}
		if text != "ok" || attempts.Load() != 2 {
			t.Fatalf("text=%q attempts=%d, want ok / 2", text, attempts.Load())
		}
		if gotAuth != "Bearer sk-ant-test" || gotVer != AnthropicAPIVersion || gotWS != "wrkspc_01ABC" {
			t.Fatalf("headers auth=%q ver=%q ws=%q", gotAuth, gotVer, gotWS)
		}
	})

	t.Run("cancel stops retry backoff promptly", func(t *testing.T) {
		t.Parallel()
		var attempts atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			attempts.Add(1)
			w.Header().Set("Retry-After", "10")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"type":"rate_limit_error","message":"slow down"}}`))
		}))
		t.Cleanup(srv.Close)

		gen, err := NewAnthropicGenerator(AnthropicConfig{
			BaseURL:    srv.URL + "/v1/messages",
			Model:      "claude-opus-5-5",
			Bearer:     "sk-ant-test",
			HTTPClient: srv.Client(),
		})
		if err != nil {
			t.Fatalf("NewAnthropicGenerator: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(80*time.Millisecond, cancel)
		ch, err := gen.GenerateContentStream(ctx, &GenerateRequest{
			Messages: []Message{{Role: RoleUser, Parts: []Part{{Text: "hi"}}}},
		})
		if err != nil {
			t.Fatalf("GenerateContentStream: %v", err)
		}
		start := time.Now()
		for range ch {
		}
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Fatalf("cancel took %s, expected fast exit", elapsed)
		}
		if attempts.Load() != 1 {
			t.Fatalf("attempts = %d, want 1", attempts.Load())
		}
	})

	t.Run("DebugWireRequest emits indented JSON", func(t *testing.T) {
		t.Parallel()
		gen, err := NewAnthropicGenerator(AnthropicConfig{
			BaseURL: "https://api.anthropic.com/v1/messages",
			Model:   "claude-opus-5-5",
		})
		if err != nil {
			t.Fatalf("NewAnthropicGenerator: %v", err)
		}
		out, err := gen.DebugWireRequest(&GenerateRequest{
			Messages: []Message{{Role: RoleUser, Parts: []Part{{Text: "hello"}}}},
		})
		if err != nil || !strings.Contains(string(out), "\n  \"model\"") {
			t.Fatalf("DebugWireRequest err=%v out=%s", err, out)
		}
	})

	t.Run("retries SSE overloaded_error before output, does not retry after output", func(t *testing.T) {
		t.Parallel()
		var preAttempts atomic.Int32
		srvPre := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			n := preAttempts.Add(1)
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Retry-After", "0")
			if n == 1 {
				_, _ = w.Write([]byte("data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude-opus-5-5\"}}\n\n" +
					"data: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"Overloaded\"}}\n\n"))
				return
			}
			_, _ = w.Write([]byte("data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"recovered\"}}\n\n" +
				"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":2}}\n\n" +
				"data: {\"type\":\"message_stop\"}\n\n"))
		}))
		t.Cleanup(srvPre.Close)

		genPre, err := NewAnthropicGenerator(AnthropicConfig{
			BaseURL:    srvPre.URL + "/v1/messages",
			Model:      "claude-opus-5-5",
			Bearer:     "sk-ant-test",
			HTTPClient: srvPre.Client(),
		})
		if err != nil {
			t.Fatalf("NewAnthropicGenerator: %v", err)
		}
		chPre, err := genPre.GenerateContentStream(testContext(t), &GenerateRequest{
			Messages: []Message{{Role: RoleUser, Parts: []Part{{Text: "hi"}}}},
		})
		if err != nil {
			t.Fatalf("GenerateContentStream: %v", err)
		}
		var text string
		for sr := range chPre {
			if sr.Error != nil {
				t.Fatalf("unexpected error after retry: %v", sr.Error)
			}
			text += sr.TextDelta
		}
		if text != "recovered" || preAttempts.Load() != 2 {
			t.Fatalf("text=%q attempts=%d, want recovered / 2", text, preAttempts.Load())
		}

		// Once visible text streamed, mid-stream overloaded_error must NOT retry.
		var midAttempts atomic.Int32
		srvMid := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			midAttempts.Add(1)
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"partial\"}}\n\n" +
				"data: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"Overloaded\"}}\n\n"))
		}))
		t.Cleanup(srvMid.Close)

		genMid, err := NewAnthropicGenerator(AnthropicConfig{
			BaseURL:    srvMid.URL + "/v1/messages",
			Model:      "claude-opus-5-5",
			Bearer:     "sk-ant-test",
			HTTPClient: srvMid.Client(),
		})
		if err != nil {
			t.Fatalf("NewAnthropicGenerator: %v", err)
		}
		chMid, err := genMid.GenerateContentStream(testContext(t), &GenerateRequest{
			Messages: []Message{{Role: RoleUser, Parts: []Part{{Text: "hi"}}}},
		})
		if err != nil {
			t.Fatalf("GenerateContentStream: %v", err)
		}
		var gotMidErr error
		for sr := range chMid {
			if sr.Error != nil {
				gotMidErr = sr.Error
			}
		}
		if !errors.Is(gotMidErr, ErrProviderOverloaded) || midAttempts.Load() != 1 {
			t.Fatalf("mid-stream err=%v attempts=%d, want ErrProviderOverloaded / 1", gotMidErr, midAttempts.Load())
		}
	})

	t.Run("malformed SSE JSON fails stream instead of emitting partial tool call", func(t *testing.T) {
		t.Parallel()
		sse := strings.Join([]string{
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"write_file","input":{}}}`,
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":TRUNCATED`,
			`data: {"type":"content_block_stop","index":0}`,
			``,
		}, "\n")
		var emittedCalls []ToolCall
		err := scanAnthropicSSE(strings.NewReader(sse), NewAnthropicSseMapperState(), nil, func(sr StreamResponse) bool {
			emittedCalls = append(emittedCalls, sr.ToolCalls...)
			return true
		})
		if err == nil || !strings.Contains(err.Error(), "decode anthropic SSE event") {
			t.Fatalf("err = %v, want decode anthropic SSE event error", err)
		}
		if len(emittedCalls) != 0 {
			t.Fatalf("emittedCalls = %#v, want none on corrupted stream", emittedCalls)
		}
	})
}
