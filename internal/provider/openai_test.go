package provider

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/credentials"
)

func TestBuildOpenAIChatRequestTemperature(t *testing.T) {
	t.Parallel()
	base := &GenerateRequest{Messages: []Message{{Role: RoleUser, Parts: []Part{{Text: "hi"}}}}}
	dt := 0.9

	// An explicit request temperature wins over the generator default.
	reqT := *base
	rt := 0.2
	reqT.Temperature = &rt
	if body := BuildOpenAIChatRequest(&reqT, "gpt-4o", config.ToolCallParsingLenient, &dt); body.Temperature == nil || *body.Temperature != 0.2 {
		t.Fatalf("request temperature should win: %v", body.Temperature)
	}

	// No request temperature: fall back to the resolved default.
	if body := BuildOpenAIChatRequest(base, "gpt-4o", config.ToolCallParsingLenient, &dt); body.Temperature == nil || *body.Temperature != 0.9 {
		t.Fatalf("default temperature should apply: %v", body.Temperature)
	}

	// Omit-family models resolve to a nil default and send no temperature.
	if body := BuildOpenAIChatRequest(base, "gemini-2.5-flash", config.ToolCallParsingLenient, nil); body.Temperature != nil {
		t.Fatalf("temperature should be omitted: %v", body.Temperature)
	}
}

func sseResponse(chunks ...string) string {
	var b strings.Builder
	for _, chunk := range chunks {
		b.WriteString("data: ")
		b.WriteString(chunk)
		b.WriteString("\n\n")
	}
	b.WriteString("data: [DONE]\n\n")
	return b.String()
}

func TestOpenAIChatStream(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		sseBody  string
		wantText string
		wantDone bool
		wantErr  bool
	}{
		{
			name: "text deltas",
			sseBody: sseResponse(
				`{"id":"1","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`,
				`{"id":"1","choices":[{"index":0,"delta":{"content":" world"},"finish_reason":"stop"}]}`,
			),
			wantText: "Hello world",
			wantDone: true,
		},
		{
			name: "structured tool calls",
			sseBody: sseResponse(
				`{"id":"1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"get_weather","arguments":"{\"city\":"}}]},"finish_reason":null}]}`,
				`{"id":"1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"Paris\"}"}}]},"finish_reason":"tool_calls"}]}`,
			),
			wantDone: true,
		},
		{
			name:    "http error",
			sseBody: "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status := http.StatusOK
			if tt.wantErr {
				status = http.StatusUnauthorized
			}

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/chat/completions" {
					t.Errorf("path = %q, want /v1/chat/completions", r.URL.Path)
				}
				if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
					t.Errorf("Authorization = %q, want Bearer test-key", auth)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(status)
				if status == http.StatusOK {
					_, _ = w.Write([]byte(tt.sseBody))
				} else {
					_, _ = w.Write([]byte(`{"error":"invalid key"}`))
				}
			}))
			t.Cleanup(srv.Close)

			gen, err := NewOpenAIChatGenerator(OpenAIChatConfig{
				BaseURL:    srv.URL + "/v1/chat/completions",
				Model:      "gpt-4o-mini",
				Bearer:     "test-key",
				HTTPClient: srv.Client(),
			})
			if err != nil {
				t.Fatalf("NewOpenAIChatGenerator: %v", err)
			}

			ch, err := gen.GenerateContentStream(testContext(t), &GenerateRequest{
				Messages: []Message{{Role: RoleUser, Parts: []Part{{Text: "hi"}}}},
			})
			if err != nil {
				t.Fatalf("GenerateContentStream: %v", err)
			}

			chunks := collectStream(t, ch)
			var gotText string
			var gotDone bool
			var gotErr error
			var gotTools int
			for _, chunk := range chunks {
				gotText += chunk.TextDelta
				if chunk.Done {
					gotDone = true
				}
				if chunk.Error != nil {
					gotErr = chunk.Error
				}
				gotTools += len(chunk.ToolCalls)
			}

			if tt.wantErr {
				if gotErr == nil {
					t.Fatal("expected stream error")
				}
				if !errors.Is(gotErr, ErrInvalidAPIKey) {
					t.Errorf("error = %v, want ErrInvalidAPIKey", gotErr)
				}
				return
			}
			if gotText != tt.wantText {
				t.Errorf("text = %q, want %q", gotText, tt.wantText)
			}
			if gotDone != tt.wantDone {
				t.Errorf("done = %v, want %v", gotDone, tt.wantDone)
			}
			if tt.name == "structured tool calls" && gotTools != 1 {
				t.Errorf("tool calls = %d, want 1", gotTools)
			}
		})
	}
}

func TestXmlToolCallFallback(t *testing.T) {
	t.Parallel()

	wrapped := `<tool_call>
<function=write_file>
<parameter=file_path>/tmp/a.js</parameter>
<parameter=content>console.log(1);</parameter>
</function>
</tool_call>`

	bareOrphan := `<function=write_file>
<parameter=file_path>/tmp/weather-app.js</parameter>
<parameter=content>launch</parameter>
</function>
</tool_call>`

	tests := []struct {
		name     string
		content  string
		mode     config.ToolCallParsingMode
		wantLen  int
		wantName string
	}{
		{"wrapped lenient", wrapped, config.ToolCallParsingLenient, 1, "write_file"},
		{"bare with orphan lenient", bareOrphan, config.ToolCallParsingLenient, 1, "write_file"},
		{"bare no orphan strict", `<function=read_file><parameter=path>/etc/hosts</parameter></function>`, config.ToolCallParsingStrict, 0, ""},
		{"bare no orphan lenient", `<function=read_file><parameter=path>/etc/hosts</parameter></function>`, config.ToolCallParsingLenient, 0, ""},
		{"bare loose", `<function=read_file><parameter=path>/etc/hosts</parameter></function>`, config.ToolCallParsingLoose, 1, "read_file"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			calls := ParseXMLToolCalls(tt.content, tt.mode)
			if len(calls) != tt.wantLen {
				t.Fatalf("calls len = %d, want %d", len(calls), tt.wantLen)
			}
			if tt.wantLen > 0 && calls[0].Function.Name != tt.wantName {
				t.Errorf("name = %q, want %q", calls[0].Function.Name, tt.wantName)
			}
		})
	}
}

func TestCustomProviderLoad(t *testing.T) {
	// Not parallel: withEmptyCredentials overrides the process-global credential
	// store factory. Running in parallel lets a sibling's ResetForTesting cleanup
	// restore the real keychain mid-test (see TestOpenRouterAsCustom).
	ctx := testContext(t)
	withEmptyCredentials(t)

	settings := &config.Settings{
		Providers: &config.ProvidersSettings{
			Active: "local-vllm",
			Custom: map[string]config.CustomProviderDefinition{
				"local-vllm": {
					DisplayName:  "Local vLLM",
					BaseURL:      "http://127.0.0.1:8000/v1",
					DefaultModel: "local-model",
					WireFormat:   config.WireFormatOpenAIChat,
				},
			},
		},
	}

	endpoint, err := ResolveEndpointConfig(settings)
	if err != nil {
		t.Fatalf("ResolveEndpointConfig: %v", err)
	}
	if endpoint.BaseURL != "http://127.0.0.1:8000/v1/chat/completions" {
		t.Errorf("BaseURL = %q", endpoint.BaseURL)
	}
	if endpoint.WireFormat != config.WireFormatOpenAIChat {
		t.Errorf("WireFormat = %q", endpoint.WireFormat)
	}

	gen, err := NewContentGenerator(ctx, settings)
	if err != nil {
		t.Fatalf("NewContentGenerator: %v", err)
	}
	if _, ok := gen.(*OpenAIChatGenerator); !ok {
		t.Fatalf("generator type = %T, want *OpenAIChatGenerator", gen)
	}
}

func TestOpenRouterAsCustom(t *testing.T) {
	// MUST NOT be parallel: this test calls the real SetProviderAPIKey. With
	// t.Parallel(), a sibling test's ResetForTesting cleanup could restore the
	// real credential backend between withEmptyCredentials and the write below,
	// clobbering the user's actual stored key with "or-key". Keep it serial so
	// the in-memory factory override is guaranteed to be active during the write.
	ctx := testContext(t)
	withEmptyCredentials(t)
	if err := credentials.SetProviderAPIKey(ctx, "openrouter", "or-key"); err != nil {
		t.Fatalf("SetProviderAPIKey: %v", err)
	}

	settings := &config.Settings{
		Providers: &config.ProvidersSettings{
			Active: "openrouter",
			Custom: map[string]config.CustomProviderDefinition{
				"openrouter": {
					DisplayName:  "OpenRouter",
					BaseURL:      "https://openrouter.ai/api/v1/chat/completions",
					DefaultModel: "qwen/qwen-2.5-coder-32b-instruct",
					APIKeyEnvVar: "OPENROUTER_API_KEY",
					WireFormat:   config.WireFormatOpenAIChat,
				},
			},
		},
	}

	endpoint, err := ResolveEndpointConfig(settings)
	if err != nil {
		t.Fatalf("ResolveEndpointConfig: %v", err)
	}
	if endpoint.RequiresAPIKey != true {
		t.Error("RequiresAPIKey should be true when apiKeyEnvVar is set")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer or-key" {
			t.Errorf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseResponse(`{"id":"1","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}]}`)))
	}))
	t.Cleanup(srv.Close)

	// Override base URL to mock server for streaming test.
	settings.Providers.Extra = map[string]json.RawMessage{
		"openrouter": mustMarshalJSON(t, map[string]string{
			"baseUrl": srv.URL + "/v1/chat/completions",
		}),
	}

	gen, err := NewContentGenerator(ctx, settings)
	if err != nil {
		t.Fatalf("NewContentGenerator: %v", err)
	}
	oaGen, ok := gen.(*OpenAIChatGenerator)
	if !ok {
		t.Fatalf("generator type = %T", gen)
	}
	if oaGen.url != srv.URL+"/v1/chat/completions" {
		t.Errorf("url = %q", oaGen.url)
	}
}

func TestModelDiscoveryEmptyOnFailure(t *testing.T) {
	t.Parallel()

	ctx := testContext(t)

	t.Run("404 returns empty", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		t.Cleanup(srv.Close)

		models := DiscoverModels(ctx, srv.URL, "", srv.Client())
		if len(models) != 0 {
			t.Fatalf("models = %#v, want empty", models)
		}
	})

	t.Run("success filters non-chat models", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]string{
					{"id": "gpt-4o-mini"},
					{"id": "text-embedding-3-small"},
					{"id": "whisper-1"},
				},
			})
		}))
		t.Cleanup(srv.Close)

		models := DiscoverModels(ctx, srv.URL+"/v1/chat/completions", "", srv.Client())
		if len(models) != 1 || models[0].ID != "gpt-4o-mini" {
			t.Fatalf("models = %#v, want gpt-4o-mini only", models)
		}
	})

	t.Run("success returns models sorted alphabetically", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]string{
					{"id": "qwen3"},
					{"id": "llama3"},
					{"id": "mistral"},
				},
			})
		}))
		t.Cleanup(srv.Close)

		models := DiscoverModels(ctx, srv.URL, "", srv.Client())
		if len(models) != 3 {
			t.Fatalf("models = %#v, want 3 entries", models)
		}
		for i, want := range []string{"llama3", "mistral", "qwen3"} {
			if models[i].ID != want {
				t.Fatalf("models[%d].ID = %q, want %q (full=%v)", i, models[i].ID, want, models)
			}
		}
	})
}

func TestFactorySelectsOpenAI(t *testing.T) {
	t.Parallel()

	ctx := testContext(t)
	withEnv(t, "OPENAI_API_KEY", "sk-test")

	settings := &config.Settings{
		Providers: &config.ProvidersSettings{
			Active: string(config.BuiltInOpenAI),
			OpenAI: &config.ProviderInstanceConfig{
				Model: "gpt-4o-mini",
			},
		},
	}

	gen, err := NewContentGenerator(ctx, settings)
	if err != nil {
		t.Fatalf("NewContentGenerator: %v", err)
	}
	if _, ok := gen.(*OpenAIChatGenerator); !ok {
		t.Fatalf("generator type = %T, want *OpenAIChatGenerator", gen)
	}
}

func TestIsOpenAIChatMode(t *testing.T) {
	t.Parallel()

	openAISettings := &config.Settings{
		Providers: &config.ProvidersSettings{Active: string(config.BuiltInOpenAI)},
	}
	if !IsOpenAIChatMode(openAISettings) {
		t.Error("expected true for openai active provider")
	}

	geminiSettings := &config.Settings{
		Providers: &config.ProvidersSettings{Active: geminiProviderID},
	}
	if IsOpenAIChatMode(geminiSettings) {
		t.Error("expected false for gemini active provider")
	}
}

func TestSetActiveProvider(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := dir + "/settings.json"
	loader, err := config.NewLoader(config.WithSettingsPath(path))
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}

	settings := &config.Settings{
		Providers: &config.ProvidersSettings{
			Active: geminiProviderID,
			Custom: map[string]config.CustomProviderDefinition{
				"local-vllm": {
					DisplayName: "Local vLLM",
					BaseURL:     "http://127.0.0.1:8000/v1/chat/completions",
				},
			},
		},
	}

	if err := SaveActiveProvider(loader, settings, "local-vllm"); err != nil {
		t.Fatalf("SaveActiveProvider: %v", err)
	}
	if settings.ActiveProvider() != "local-vllm" {
		t.Errorf("active = %q", settings.ActiveProvider())
	}

	reloaded, err := loader.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reloaded.ActiveProvider() != "local-vllm" {
		t.Errorf("reloaded active = %q", reloaded.ActiveProvider())
	}
}

func TestChatCompletionsURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{"https://api.openai.com/v1/chat/completions", "https://api.openai.com/v1/chat/completions"},
		{"https://openrouter.ai/api/v1", "https://openrouter.ai/api/v1/chat/completions"},
		{"http://127.0.0.1:8000", "http://127.0.0.1:8000/v1/chat/completions"},
	}
	for _, tt := range tests {
		if got := ChatCompletionsURL(tt.in); got != tt.want {
			t.Errorf("ChatCompletionsURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func mustMarshalJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestOpenAIFactoryMissingKey(t *testing.T) {
	// Not parallel: withEmptyCredentials overrides the process-global credential
	// store factory; a raced sibling cleanup could expose the real keychain.
	ctx := testContext(t)
	withEmptyCredentials(t)
	withoutEnv(t, "OPENAI_API_KEY")

	settings := &config.Settings{
		Providers: &config.ProvidersSettings{
			Active: string(config.BuiltInOpenAI),
		},
	}

	_, err := NewContentGenerator(ctx, settings)
	if err == nil {
		t.Fatal("expected missing key error")
	}
	if !errors.Is(err, credentials.ErrAPIKeyMissing) {
		t.Errorf("error = %v, want ErrAPIKeyMissing", err)
	}
}

func TestBuildNonStreamRetryBody(t *testing.T) {
	t.Parallel()

	out := BuildNonStreamRetryBody(map[string]any{
		"stream":         true,
		"stream_options": map[string]any{"include_usage": true},
		"model":          "m",
	})
	if out["stream"] != false {
		t.Errorf("stream = %v", out["stream"])
	}
	if _, ok := out["stream_options"]; ok {
		t.Error("stream_options should be removed")
	}
}
