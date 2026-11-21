package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
)

func TestDiscoverModelsWithErrorSurfacesHTTPFailure(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`))
	}))
	t.Cleanup(srv.Close)

	infos, err := DiscoverModelsWithError(context.Background(), srv.URL+"/v1/chat/completions", "bad", srv.Client())
	if err == nil {
		t.Fatal("expected error for HTTP 401")
	}
	if len(infos) != 0 {
		t.Fatalf("infos = %#v, want nil", infos)
	}
	if !strings.Contains(err.Error(), "HTTP 401") || !strings.Contains(err.Error(), "invalid x-api-key") {
		t.Fatalf("err = %q, want HTTP 401 and invalid x-api-key", err.Error())
	}

	// Best-effort wrapper keeps returning nil on failure.
	if got := DiscoverModels(context.Background(), srv.URL+"/v1/chat/completions", "bad", srv.Client()); got != nil {
		t.Fatalf("DiscoverModels = %#v, want nil", got)
	}
}

func TestDiscoverModelsParsesContextLength(t *testing.T) {
	t.Parallel()
	// OpenRouter-style payload: context_length per model.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[
			{"id":"anthropic/claude-3.5","context_length":200000},
			{"id":"meta/llama-3","max_model_len":8192},
			{"id":"no-limit/model"}
		]}`))
	}))
	t.Cleanup(srv.Close)

	infos := DiscoverModels(context.Background(), srv.URL+"/v1/chat/completions", "", srv.Client())
	got := map[string]int{}
	for _, m := range infos {
		got[m.ID] = m.ContextLimit
	}
	if got["anthropic/claude-3.5"] != 200000 {
		t.Errorf("context_length not parsed: %d", got["anthropic/claude-3.5"])
	}
	if got["meta/llama-3"] != 8192 {
		t.Errorf("max_model_len not parsed: %d", got["meta/llama-3"])
	}
	if got["no-limit/model"] != 0 {
		t.Errorf("missing limit should be 0: %d", got["no-limit/model"])
	}
}

func TestContextLimitForModel(t *testing.T) {
	t.Parallel()
	models := []ModelInfo{{ID: "qwen/coder", ContextLimit: 32768}, {ID: "x", ContextLimit: 0}}
	if got := ContextLimitForModel(models, "qwen/coder"); got != 32768 {
		t.Errorf("discovered limit = %d, want 32768", got)
	}
	// Falls back to the static table for OpenAI-direct ids.
	if got := ContextLimitForModel(nil, "gpt-4o"); got != 128_000 {
		t.Errorf("static gpt-4o = %d, want 128000", got)
	}
	if got := ContextLimitForModel(nil, "openai/gpt-4o-2024-08-06"); got != 128_000 {
		t.Errorf("prefix gpt-4o = %d, want 128000", got)
	}
	if got := ContextLimitForModel(nil, "claude-opus-5-5"); got != 1_000_000 {
		t.Errorf("static claude-opus-5-5 = %d, want 1000000", got)
	}
	if got := ContextLimitForModel(nil, "claude-sonnet-4-5-20250929"); got != 200_000 {
		t.Errorf("static claude-sonnet-4-5 = %d, want 200000", got)
	}
	if got := ContextLimitForModel(nil, "totally-unknown"); got != 0 {
		t.Errorf("unknown = %d, want 0", got)
	}
}

func TestDiscoverAnthropicModelsPaginationAndCapabilities(t *testing.T) {
	t.Parallel()

	page := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("anthropic-version") != AnthropicAPIVersion {
			t.Errorf("anthropic-version = %q, want %q", r.Header.Get("anthropic-version"), AnthropicAPIVersion)
		}
		if r.Header.Get("anthropic-workspace-id") != "wrkspc_01XYZ" {
			t.Errorf("anthropic-workspace-id = %q, want wrkspc_01XYZ", r.Header.Get("anthropic-workspace-id"))
		}
		page++
		w.Header().Set("Content-Type", "application/json")
		if page == 1 {
			_, _ = w.Write([]byte(`{
				"data": [{
					"id": "claude-opus-5-5",
					"max_input_tokens": 1000000,
					"max_tokens": 128000,
					"capabilities": {
						"effort": {"supported": true, "low": {"supported": true}, "medium": {"supported": true}, "high": {"supported": true}, "xhigh": {"supported": true}, "max": {"supported": true}},
						"thinking": {"supported": true, "types": {"adaptive": {"supported": true}}}
					}
				}],
				"has_more": true,
				"last_id": "claude-opus-5-5"
			}`))
			return
		}
		if r.URL.Query().Get("after_id") != "claude-opus-5-5" {
			t.Errorf("after_id = %q, want claude-opus-5-5", r.URL.Query().Get("after_id"))
		}
		_, _ = w.Write([]byte(`{
			"data": [{
				"id": "claude-haiku-5-5",
				"max_input_tokens": 1000000,
				"max_tokens": 128000,
				"capabilities": {
					"effort": {"supported": true, "low": {"supported": true}, "high": {"supported": true}},
					"thinking": {"supported": true, "types": {"adaptive": {"supported": true}, "disabled": {"supported": true}}}
				}
			}],
			"has_more": false,
			"last_id": "claude-haiku-5-5"
		}`))
	}))
	t.Cleanup(srv.Close)

	infos, err := DiscoverAnthropicModels(context.Background(), srv.URL+"/v1/messages", "sk-ant-test", "wrkspc_01XYZ", srv.Client())
	if err != nil {
		t.Fatalf("DiscoverAnthropicModels: %v", err)
	}
	if len(infos) != 2 {
		t.Fatalf("infos len = %d, want 2", len(infos))
	}
	if infos[0].ID != "claude-haiku-5-5" || infos[0].Reasoning.Mandatory {
		t.Fatalf("haiku info = %#v, want non-mandatory", infos[0])
	}
	if infos[1].ID != "claude-opus-5-5" || infos[1].ContextLimit != 1_000_000 || infos[1].MaxOutputTokens != 128_000 || !infos[1].Reasoning.Mandatory {
		t.Fatalf("opus info = %#v", infos[1])
	}
}

func TestDiscoverModelsParsesReasoningObject(t *testing.T) {
	t.Parallel()
	// OpenRouter-style payload: per-model reasoning capability object.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[
			{"id":"anthropic/claude-4","context_length":200000,"reasoning":{"supported_efforts":["low","medium","high"],"default_effort":"medium","default_enabled":true,"mandatory":false}},
			{"id":"meta/llama-3","max_model_len":8192}
		]}`))
	}))
	t.Cleanup(srv.Close)

	infos := DiscoverModels(context.Background(), srv.URL+"/v1/chat/completions", "", srv.Client())

	reasoning := ReasoningInfoForModel(infos, "anthropic/claude-4")
	if reasoning == nil {
		t.Fatal("expected reasoning info for anthropic/claude-4")
	}
	if !reasoning.DefaultEnabled {
		t.Error("DefaultEnabled = false, want true")
	}
	if reasoning.DefaultEffort != "medium" {
		t.Errorf("DefaultEffort = %q, want medium", reasoning.DefaultEffort)
	}
	if reasoning.Mandatory {
		t.Error("Mandatory = true, want false")
	}
	if len(reasoning.SupportedEfforts) != 3 {
		t.Errorf("SupportedEfforts = %v, want 3 entries", reasoning.SupportedEfforts)
	}

	if got := ReasoningInfoForModel(infos, "meta/llama-3"); got != nil {
		t.Errorf("expected nil reasoning info for meta/llama-3, got %#v", got)
	}
	if got := ReasoningInfoForModel(infos, "unknown/model"); got != nil {
		t.Errorf("expected nil reasoning info for unknown model, got %#v", got)
	}
}

func TestMaybeSetReasoningCapability(t *testing.T) {
	t.Parallel()

	t.Run("caches discovered capability", func(t *testing.T) {
		t.Parallel()
		s := &config.Settings{Providers: &config.ProvidersSettings{Active: string(config.BuiltInOpenAI), OpenAI: &config.ProviderInstanceConfig{}}}
		info := &ModelReasoningInfo{
			DefaultEnabled:   true,
			Mandatory:        false,
			DefaultEffort:    "medium",
			SupportedEfforts: []string{"low", "medium", "high"},
		}
		changed, err := MaybeSetReasoningCapability(s, string(config.BuiltInOpenAI), "some/model", info)
		if err != nil || !changed {
			t.Fatalf("expected change, got changed=%v err=%v", changed, err)
		}
		mc, ok := config.LookupModelConfig(s.Providers.OpenAI, "some/model")
		if !ok || mc.ReasoningSupported == nil || !*mc.ReasoningSupported {
			t.Fatalf("ReasoningSupported not cached: %+v", mc)
		}
		if mc.ReasoningMandatory == nil || *mc.ReasoningMandatory {
			t.Fatalf("ReasoningMandatory not cached correctly: %+v", mc)
		}
		if len(mc.ReasoningEfforts) != 3 || mc.ReasoningEfforts[0] != "low" {
			t.Fatalf("ReasoningEfforts not cached: %+v", mc.ReasoningEfforts)
		}
		if mc.ReasoningDefaultEffort != "medium" {
			t.Fatalf("ReasoningDefaultEffort = %q, want medium", mc.ReasoningDefaultEffort)
		}
	})

	t.Run("does not overwrite a user pin", func(t *testing.T) {
		t.Parallel()
		s := &config.Settings{Providers: &config.ProvidersSettings{Active: string(config.BuiltInOpenAI), OpenAI: &config.ProviderInstanceConfig{
			Models: map[string]config.ProviderModelConfig{"some/model": {ReasoningEffort: "high"}},
		}}}
		info := &ModelReasoningInfo{DefaultEnabled: true}
		changed, err := MaybeSetReasoningCapability(s, string(config.BuiltInOpenAI), "some/model", info)
		if err != nil || changed {
			t.Fatalf("expected no change (pinned), got changed=%v err=%v", changed, err)
		}
		mc, _ := config.LookupModelConfig(s.Providers.OpenAI, "some/model")
		if mc.ReasoningSupported != nil {
			t.Fatalf("ReasoningSupported should remain unset when pinned: %+v", mc)
		}
	})

	t.Run("nil info is a no-op", func(t *testing.T) {
		t.Parallel()
		s := &config.Settings{Providers: &config.ProvidersSettings{Active: string(config.BuiltInOpenAI), OpenAI: &config.ProviderInstanceConfig{}}}
		changed, err := MaybeSetReasoningCapability(s, string(config.BuiltInOpenAI), "some/model", nil)
		if err != nil || changed {
			t.Fatalf("expected no change for nil info, got changed=%v err=%v", changed, err)
		}
	})

	t.Run("idempotent on repeat identical discovery", func(t *testing.T) {
		t.Parallel()
		s := &config.Settings{Providers: &config.ProvidersSettings{Active: string(config.BuiltInOpenAI), OpenAI: &config.ProviderInstanceConfig{}}}
		info := &ModelReasoningInfo{DefaultEnabled: true}
		if _, err := MaybeSetReasoningCapability(s, string(config.BuiltInOpenAI), "some/model", info); err != nil {
			t.Fatalf("first call: %v", err)
		}
		changed, err := MaybeSetReasoningCapability(s, string(config.BuiltInOpenAI), "some/model", info)
		if err != nil || changed {
			t.Fatalf("expected no change on identical repeat, got changed=%v err=%v", changed, err)
		}
	})
}

func TestReasoningCapabilityKnown(t *testing.T) {
	t.Parallel()
	s := &config.Settings{Providers: &config.ProvidersSettings{Active: string(config.BuiltInOpenAI), OpenAI: &config.ProviderInstanceConfig{}}}
	if ReasoningCapabilityKnown(s, string(config.BuiltInOpenAI), "some/model") {
		t.Fatal("expected unknown before discovery runs")
	}
	if _, err := MaybeSetReasoningCapability(s, string(config.BuiltInOpenAI), "some/model", &ModelReasoningInfo{DefaultEnabled: true}); err != nil {
		t.Fatalf("MaybeSetReasoningCapability: %v", err)
	}
	if !ReasoningCapabilityKnown(s, string(config.BuiltInOpenAI), "some/model") {
		t.Fatal("expected known after discovery caches it")
	}
}

func TestMarkReasoningProbed(t *testing.T) {
	t.Parallel()

	t.Run("sets probe flag and makes capability known", func(t *testing.T) {
		t.Parallel()
		s := &config.Settings{Providers: &config.ProvidersSettings{Active: string(config.BuiltInOpenAI), OpenAI: &config.ProviderInstanceConfig{}}}
		changed, err := MarkReasoningProbed(s, string(config.BuiltInOpenAI), "local/qwen")
		if err != nil || !changed {
			t.Fatalf("expected change, got changed=%v err=%v", changed, err)
		}
		mc, ok := config.LookupModelConfig(s.Providers.OpenAI, "local/qwen")
		if !ok || !mc.ReasoningProbed {
			t.Fatalf("ReasoningProbed not set: %+v", mc)
		}
		if mc.ReasoningSupported != nil {
			t.Fatalf("ReasoningSupported should stay nil for probed-only: %+v", mc)
		}
		if !ReasoningCapabilityKnown(s, string(config.BuiltInOpenAI), "local/qwen") {
			t.Fatal("probed model should suppress re-discovery")
		}
	})

	t.Run("no-ops when user pinned reasoningEffort", func(t *testing.T) {
		t.Parallel()
		s := &config.Settings{Providers: &config.ProvidersSettings{Active: string(config.BuiltInOpenAI), OpenAI: &config.ProviderInstanceConfig{
			Models: map[string]config.ProviderModelConfig{"local/qwen": {ReasoningEffort: "high"}},
		}}}
		changed, err := MarkReasoningProbed(s, string(config.BuiltInOpenAI), "local/qwen")
		if err != nil || changed {
			t.Fatalf("expected no change when pinned, got changed=%v err=%v", changed, err)
		}
	})

	t.Run("no-ops when ReasoningSupported already set", func(t *testing.T) {
		t.Parallel()
		tru := true
		s := &config.Settings{Providers: &config.ProvidersSettings{Active: string(config.BuiltInOpenAI), OpenAI: &config.ProviderInstanceConfig{
			Models: map[string]config.ProviderModelConfig{"some/model": {ReasoningSupported: &tru}},
		}}}
		changed, err := MarkReasoningProbed(s, string(config.BuiltInOpenAI), "some/model")
		if err != nil || changed {
			t.Fatalf("expected no change when supported cached, got changed=%v err=%v", changed, err)
		}
	})

	t.Run("idempotent on repeat", func(t *testing.T) {
		t.Parallel()
		s := &config.Settings{Providers: &config.ProvidersSettings{Active: string(config.BuiltInOpenAI), OpenAI: &config.ProviderInstanceConfig{}}}
		if _, err := MarkReasoningProbed(s, string(config.BuiltInOpenAI), "local/qwen"); err != nil {
			t.Fatalf("first: %v", err)
		}
		changed, err := MarkReasoningProbed(s, string(config.BuiltInOpenAI), "local/qwen")
		if err != nil || changed {
			t.Fatalf("expected no change on repeat, got changed=%v err=%v", changed, err)
		}
	})
}

func TestMaybeSetContextLimitRespectsPin(t *testing.T) {
	t.Parallel()
	// Unpinned: auto-discovery sets the limit.
	s := &config.Settings{Providers: &config.ProvidersSettings{Active: string(config.BuiltInOpenAI), OpenAI: &config.ProviderInstanceConfig{}}}
	changed, err := MaybeSetContextLimit(s, string(config.BuiltInOpenAI), 128000)
	if err != nil || !changed {
		t.Fatalf("expected change, got changed=%v err=%v", changed, err)
	}
	if s.Providers.OpenAI.ContextLimit == nil || *s.Providers.OpenAI.ContextLimit != 128000 {
		t.Fatalf("contextLimit not set: %+v", s.Providers.OpenAI.ContextLimit)
	}

	// User-pinned: auto-discovery leaves it alone.
	pinned := true
	pin := 4096
	s2 := &config.Settings{Providers: &config.ProvidersSettings{Active: string(config.BuiltInOpenAI), OpenAI: &config.ProviderInstanceConfig{ContextLimit: &pin, ContextLimitUserSet: &pinned}}}
	changed, err = MaybeSetContextLimit(s2, string(config.BuiltInOpenAI), 128000)
	if err != nil || changed {
		t.Fatalf("pinned limit should be untouched, got changed=%v err=%v", changed, err)
	}
	if *s2.Providers.OpenAI.ContextLimit != 4096 {
		t.Fatalf("pinned contextLimit changed: %d", *s2.Providers.OpenAI.ContextLimit)
	}
}
func TestMaybeSetModelContextLimitWritesPerModel(t *testing.T) {
	t.Parallel()
	s := &config.Settings{Providers: &config.ProvidersSettings{Active: string(config.BuiltInOpenAI), OpenAI: &config.ProviderInstanceConfig{}}}
	changed, err := MaybeSetModelContextLimit(s, string(config.BuiltInOpenAI), "moonshotai/kimi-k3", 131072)
	if err != nil || !changed {
		t.Fatalf("expected change, got changed=%v err=%v", changed, err)
	}
	if s.Providers.OpenAI.ContextLimit != nil {
		t.Fatalf("provider-wide contextLimit should stay unset, got %v", *s.Providers.OpenAI.ContextLimit)
	}
	mc, ok := config.LookupModelConfig(s.Providers.OpenAI, "moonshotai/kimi-k3")
	if !ok || mc.ContextLimit == nil || *mc.ContextLimit != 131072 {
		t.Fatalf("per-model contextLimit = %+v ok=%v", mc.ContextLimit, ok)
	}
	changed, err = MaybeSetModelContextLimit(s, string(config.BuiltInOpenAI), "moonshotai/kimi-k3", 131072)
	if err != nil || changed {
		t.Fatalf("idempotent write should be a no-op, got changed=%v err=%v", changed, err)
	}
}

func TestMaybeSetContextLimitPreferDiscovered(t *testing.T) {
	t.Parallel()
	pinned := true
	pin := 4096
	prefer := true
	s := &config.Settings{
		Sagittarius: &config.SagittariusSettings{ContextLimitPreferDiscovered: &prefer},
		Providers: &config.ProvidersSettings{
			Active: string(config.BuiltInOpenAI),
			OpenAI: &config.ProviderInstanceConfig{ContextLimit: &pin, ContextLimitUserSet: &pinned},
		},
	}
	changed, err := MaybeSetContextLimit(s, string(config.BuiltInOpenAI), 128000)
	if err != nil {
		t.Fatalf("MaybeSetContextLimit error: %v", err)
	}
	if !changed {
		t.Fatalf("expected limit to change when preferDiscovered is true")
	}
	if *s.Providers.OpenAI.ContextLimit != 128000 {
		t.Fatalf("pinned contextLimit was not updated: %d", *s.Providers.OpenAI.ContextLimit)
	}
	if s.Providers.OpenAI.ContextLimitUserSet != nil {
		t.Fatalf("expected ContextLimitUserSet to be cleared, got %v", *s.Providers.OpenAI.ContextLimitUserSet)
	}
}
