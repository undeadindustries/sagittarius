package provider

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/undeadindustries/sagittarius/internal/config"
)

func customProviderSettings(t *testing.T, id string, wf config.WireFormat, inst *config.ProviderInstanceConfig) *config.Settings {
	t.Helper()
	extra := map[string]json.RawMessage{}
	if inst != nil {
		raw, err := json.Marshal(inst)
		if err != nil {
			t.Fatalf("marshal instance: %v", err)
		}
		extra[id] = raw
	}
	return &config.Settings{
		Providers: &config.ProvidersSettings{
			Active: id,
			Custom: map[string]config.CustomProviderDefinition{
				id: {
					DisplayName:  id,
					BaseURL:      "http://127.0.0.1:9999/v1",
					WireFormat:   wf,
					DefaultModel: "test-model",
				},
			},
			Extra: extra,
		},
	}
}

// TestGeneratorCacheInvalidatesOnStallTimeouts pins that changing connectTimeout
// or firstTokenTimeout produces a cache miss and returns a generator with the
// updated budgets. Without these fields in generatorCacheKey, editing a timeout
// in /providers mid-session appeared to save while keeping the old 30s/300s
// generator alive until restart.
func TestGeneratorCacheInvalidatesOnStallTimeouts(t *testing.T) {
	t.Parallel()

	t.Run("anthropic", func(t *testing.T) {
		t.Parallel()
		cache := NewGeneratorCache()
		ctx := context.Background()

		sDefault := customProviderSettings(t, "test-anthropic", config.WireFormatAnthropic, nil)
		g1, err := cache.GetOrCreate(ctx, sDefault)
		if err != nil {
			t.Fatalf("GetOrCreate(default): %v", err)
		}
		a1 := g1.(*AnthropicGenerator)
		if a1.connectTimeout != defaultConnectTimeout || a1.firstTokenTimeout != defaultFirstTokenTimeout {
			t.Fatalf("default budgets = (%s, %s), want (%s, %s)",
				a1.connectTimeout, a1.firstTokenTimeout, defaultConnectTimeout, defaultFirstTokenTimeout)
		}

		// Identical settings hit the cache.
		g1Again, err := cache.GetOrCreate(ctx, sDefault)
		if err != nil {
			t.Fatalf("GetOrCreate(repeat): %v", err)
		}
		if g1Again != g1 {
			t.Fatal("expected cache hit for unchanged settings")
		}

		// Raising connectTimeout must produce a fresh generator with the new budget.
		sConnect := customProviderSettings(t, "test-anthropic", config.WireFormatAnthropic, &config.ProviderInstanceConfig{
			ConnectTimeout: intPtr(260),
		})
		g2, err := cache.GetOrCreate(ctx, sConnect)
		if err != nil {
			t.Fatalf("GetOrCreate(connectTimeout=260): %v", err)
		}
		if g2 == g1 {
			t.Fatal("cache returned stale generator after connectTimeout changed")
		}
		a2 := g2.(*AnthropicGenerator)
		if a2.connectTimeout != 260*time.Second || a2.firstTokenTimeout != defaultFirstTokenTimeout {
			t.Fatalf("updated budgets = (%s, %s), want (260s, %s)",
				a2.connectTimeout, a2.firstTokenTimeout, defaultFirstTokenTimeout)
		}

		// Setting connectTimeout=0 (disabled) and firstTokenTimeout=900 must also miss.
		sBoth := customProviderSettings(t, "test-anthropic", config.WireFormatAnthropic, &config.ProviderInstanceConfig{
			ConnectTimeout:    intPtr(0),
			FirstTokenTimeout: intPtr(900),
		})
		g3, err := cache.GetOrCreate(ctx, sBoth)
		if err != nil {
			t.Fatalf("GetOrCreate(0, 900): %v", err)
		}
		if g3 == g2 {
			t.Fatal("cache returned stale generator after stall timeouts changed")
		}
		a3 := g3.(*AnthropicGenerator)
		if a3.connectTimeout != 0 || a3.firstTokenTimeout != 900*time.Second {
			t.Fatalf("updated budgets = (%s, %s), want (0s, 900s)", a3.connectTimeout, a3.firstTokenTimeout)
		}
	})

	t.Run("openai-chat", func(t *testing.T) {
		t.Parallel()
		cache := NewGeneratorCache()
		ctx := context.Background()

		g1, err := cache.GetOrCreate(ctx, customProviderSettings(t, "test-chat", config.WireFormatOpenAIChat, nil))
		if err != nil {
			t.Fatalf("GetOrCreate(default): %v", err)
		}
		g2, err := cache.GetOrCreate(ctx, customProviderSettings(t, "test-chat", config.WireFormatOpenAIChat, &config.ProviderInstanceConfig{
			ConnectTimeout:    intPtr(120),
			FirstTokenTimeout: intPtr(600),
		}))
		if err != nil {
			t.Fatalf("GetOrCreate(updated): %v", err)
		}
		if g2 == g1 {
			t.Fatal("cache returned stale openai-chat generator after stall timeout change")
		}
		oc := g2.(*OpenAIChatGenerator)
		if oc.connectTimeout != 120*time.Second || oc.firstTokenTimeout != 600*time.Second {
			t.Fatalf("openai-chat budgets = (%s, %s), want (120s, 600s)", oc.connectTimeout, oc.firstTokenTimeout)
		}
	})

	t.Run("openai-responses", func(t *testing.T) {
		t.Parallel()
		cache := NewGeneratorCache()
		ctx := context.Background()

		g1, err := cache.GetOrCreate(ctx, customProviderSettings(t, "test-resp", config.WireFormatOpenAIResponses, nil))
		if err != nil {
			t.Fatalf("GetOrCreate(default): %v", err)
		}
		g2, err := cache.GetOrCreate(ctx, customProviderSettings(t, "test-resp", config.WireFormatOpenAIResponses, &config.ProviderInstanceConfig{
			ConnectTimeout:    intPtr(90),
			FirstTokenTimeout: intPtr(450),
		}))
		if err != nil {
			t.Fatalf("GetOrCreate(updated): %v", err)
		}
		if g2 == g1 {
			t.Fatal("cache returned stale openai-responses generator after stall timeout change")
		}
		or := g2.(*OpenAIResponsesGenerator)
		if or.connectTimeout != 90*time.Second || or.firstTokenTimeout != 450*time.Second {
			t.Fatalf("openai-responses budgets = (%s, %s), want (90s, 450s)", or.connectTimeout, or.firstTokenTimeout)
		}
	})
}

func TestGeneratorCacheInvalidatesGeminiFirstTokenTimeout(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-key")
	cache := NewGeneratorCache()
	ctx := context.Background()

	s1 := &config.Settings{
		Providers: &config.ProvidersSettings{
			Active:       string(config.BuiltInGeminiAPIKey),
			GeminiAPIKey: &config.ProviderInstanceConfig{},
		},
	}
	g1, err := cache.GetOrCreate(ctx, s1)
	if err != nil {
		t.Fatalf("GetOrCreate(default): %v", err)
	}
	s2 := &config.Settings{
		Providers: &config.ProvidersSettings{
			Active:       string(config.BuiltInGeminiAPIKey),
			GeminiAPIKey: &config.ProviderInstanceConfig{FirstTokenTimeout: intPtr(900)},
		},
	}
	g2, err := cache.GetOrCreate(ctx, s2)
	if err != nil {
		t.Fatalf("GetOrCreate(firstTokenTimeout=900): %v", err)
	}
	if g2 == g1 {
		t.Fatal("cache returned stale gemini generator after firstTokenTimeout changed")
	}
	gg := g2.(*GeminiGenerator)
	if gg.firstTokenTimeout != 900*time.Second {
		t.Fatalf("gemini firstTokenTimeout = %s, want 900s", gg.firstTokenTimeout)
	}
}

// TestGeneratorCacheKeyCoversEndpointConfig ensures new fields added to
// EndpointConfig cannot silently bypass GeneratorCache invalidation. Every
// field must either appear in generatorCacheKey or be explicitly excluded
// because it is passed per-request.
func TestGeneratorCacheKeyCoversEndpointConfig(t *testing.T) {
	t.Parallel()

	excluded := map[string]bool{
		"model":          true, // passed per-request on GenerateRequest
		"temperature":    true, // passed per-request on GenerateRequest
		"requiresapikey": true, // only governs missing-key error; resolved bearer is in the key
	}

	keyFields := map[string]bool{}
	kt := reflect.TypeFor[generatorCacheKey]()
	for i := range kt.NumField() {
		keyFields[strings.ToLower(kt.Field(i).Name)] = true
	}

	et := reflect.TypeFor[EndpointConfig]()
	for i := range et.NumField() {
		name := et.Field(i).Name
		lower := strings.ToLower(name)
		if !keyFields[lower] && !excluded[lower] {
			t.Errorf("EndpointConfig.%s is missing from generatorCacheKey (add it to the key or document why it is excluded)", name)
		}
	}
}
