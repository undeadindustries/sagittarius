package agent

import (
	"context"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/provider"
)

// TestApplyReasoningInfosMarksProbedAndSkipsRediscovery verifies that a catalog
// hit with no reasoning block writes ReasoningProbed, and a subsequent
// discoverReasoningInfosIfNeeded call does not treat the model as unknown.
func TestApplyReasoningInfosMarksProbedAndSkipsRediscovery(t *testing.T) {
	s := &config.Settings{Providers: &config.ProvidersSettings{
		Active: string(config.BuiltInOpenAI),
		OpenAI: &config.ProviderInstanceConfig{
			BaseURL: "https://example.invalid/v1",
		},
	}}
	infos := []provider.ModelInfo{{ID: "local/qwen"}} // no Reasoning block
	applyReasoningInfos(s, string(config.BuiltInOpenAI), []string{"local/qwen"}, infos, true)

	if !provider.ReasoningCapabilityKnown(s, string(config.BuiltInOpenAI), "local/qwen") {
		t.Fatal("expected ReasoningCapabilityKnown after probe")
	}
	_, _, _, known := config.ModelReasoningOptions(s, string(config.BuiltInOpenAI), "local/qwen")
	if known {
		t.Fatal("probed-only must keep ModelReasoningOptions known=false")
	}

	// Second activation must not re-fetch: known suppresses the catalog call.
	got, discovered := discoverReasoningInfosIfNeeded(context.Background(), s, string(config.BuiltInOpenAI), []string{"local/qwen"})
	if discovered || got != nil {
		t.Fatalf("expected no rediscovery, got infos=%v discovered=%v", got, discovered)
	}
}

func TestApplyReasoningInfosCachesPositiveCapability(t *testing.T) {
	s := &config.Settings{Providers: &config.ProvidersSettings{
		Active: string(config.BuiltInOpenAI),
		OpenAI: &config.ProviderInstanceConfig{},
	}}
	infos := []provider.ModelInfo{{
		ID: "anthropic/claude-4",
		Reasoning: &provider.ModelReasoningInfo{
			DefaultEnabled:   true,
			SupportedEfforts: []string{"low", "medium", "high"},
			DefaultEffort:    "medium",
		},
	}}
	applyReasoningInfos(s, string(config.BuiltInOpenAI), []string{"anthropic/claude-4"}, infos, true)

	mc, ok := config.LookupModelConfig(s.Providers.OpenAI, "anthropic/claude-4")
	if !ok || mc.ReasoningSupported == nil || !*mc.ReasoningSupported {
		t.Fatalf("expected ReasoningSupported cached: %+v", mc)
	}
	if mc.ReasoningProbed {
		t.Fatal("positive discovery should clear ReasoningProbed")
	}
	efforts, defaultEffort, _, known := config.ModelReasoningOptions(s, string(config.BuiltInOpenAI), "anthropic/claude-4")
	if !known || len(efforts) != 3 || defaultEffort != "medium" {
		t.Fatalf("options = efforts=%v default=%q known=%v", efforts, defaultEffort, known)
	}
}

func TestApplyReasoningInfosNoProbeWithoutDiscovery(t *testing.T) {
	s := &config.Settings{Providers: &config.ProvidersSettings{
		Active: string(config.BuiltInOpenAI),
		OpenAI: &config.ProviderInstanceConfig{},
	}}
	applyReasoningInfos(s, string(config.BuiltInOpenAI), []string{"local/qwen"}, nil, false)
	if provider.ReasoningCapabilityKnown(s, string(config.BuiltInOpenAI), "local/qwen") {
		t.Fatal("failed/empty discovery must not write ReasoningProbed")
	}
}
