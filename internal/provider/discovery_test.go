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
