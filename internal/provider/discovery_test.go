package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
)

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
	if got := ContextLimitForModel(nil, "totally-unknown"); got != 0 {
		t.Errorf("unknown = %d, want 0", got)
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
		info := &ModelReasoningInfo{DefaultEnabled: true, Mandatory: false, DefaultEffort: "medium"}
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
