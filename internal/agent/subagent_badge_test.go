package agent

import (
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/modes"
)

func TestSubagentPairLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                                     string
		provider, model, liveProvider, liveModel string
		want                                     string
	}{
		{"same pair is silent", "openai", "m", "openai", "m", ""},
		{"cross-provider shows provider/model", "openrouter", "qwen", "openai", "m", "openrouter/qwen"},
		{"same-provider model pin shows bare model", "openai", "qwen", "openai", "m", "qwen"},
		{"gemini display id", "gemini-apikey", "gemini-3-flash", "openai", "m", "gemini/gemini-3-flash"},
		{"empty provider and model is silent", "", "", "openai", "m", ""},
	}
	for _, tc := range tests {
		if got := subagentPairLabel(tc.provider, tc.model, tc.liveProvider, tc.liveModel); got != tc.want {
			t.Errorf("%s: subagentPairLabel = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestSubagentBadgeFollowsPins(t *testing.T) {
	t.Parallel()

	on := true
	settings := openAISettingsWithModelPins(nil)
	settings.Sagittarius.Subagents = &config.SagittariusSubagents{
		Research: &config.SagittariusSubagentClass{Enabled: &on, Model: "child-model"},
		Coding:   &config.SagittariusSubagentClass{Enabled: &on, Provider: "openrouter", Model: "qwen/qwen3-coder"},
	}
	h := newSubagentHarness(t, settings)

	if got := subagentBadge(h.parent, config.SubagentResearch); got != "child-model" {
		t.Errorf("research badge = %q, want child-model (legacy model-only pin)", got)
	}
	if got := subagentBadge(h.parent, config.SubagentCoding); got != "openrouter/qwen/qwen3-coder" {
		t.Errorf("coding badge = %q, want openrouter/qwen/qwen3-coder", got)
	}
	// Reviewer has no pin: it resolves to the live pair, so no badge.
	if got := subagentBadge(h.parent, config.SubagentReviewer); got != "" {
		t.Errorf("unpinned reviewer badge = %q, want empty", got)
	}
}

func TestSubagentBadgeSilentWhenPinMatchesLive(t *testing.T) {
	t.Parallel()

	on := true
	settings := openAISettingsWithModelPins(nil)
	settings.Sagittarius.Subagents = &config.SagittariusSubagents{
		Research: &config.SagittariusSubagentClass{
			Enabled:  &on,
			Provider: string(config.BuiltInOpenAI),
			Model:    "parent-model",
		},
	}
	h := newSubagentHarness(t, settings)
	if got := subagentBadge(h.parent, config.SubagentResearch); got != "" {
		t.Errorf("badge = %q, want empty when the pin matches the live pair", got)
	}
}

func TestSubagentViaLabelReadsChildPair(t *testing.T) {
	t.Parallel()

	on := true
	settings := openAISettingsWithModelPins(nil)
	settings.Sagittarius.Subagents = &config.SagittariusSubagents{
		Coding: &config.SagittariusSubagentClass{Enabled: &on, Provider: "openrouter", Model: "qwen/qwen3-coder"},
	}
	h := newSubagentHarness(t, settings)

	child, err := h.parent.newSubagent(t.Context(), subagentSpec{
		description: "via-label",
		mode:        modes.ModeAgent,
		class:       config.SubagentCoding,
		approval:    ApprovalYolo,
	})
	if err != nil {
		t.Fatalf("newSubagent: %v", err)
	}
	if got := subagentViaLabel(h.parent, child); got != "openrouter/qwen/qwen3-coder" {
		t.Errorf("via = %q, want openrouter/qwen/qwen3-coder", got)
	}

	// A child on the live pair produces no via label.
	child2, err := h.parent.newSubagent(t.Context(), subagentSpec{
		description: "via-label-live",
		mode:        modes.ModeAsk,
		class:       config.SubagentResearch,
		approval:    ApprovalYolo,
	})
	if err != nil {
		t.Fatalf("newSubagent: %v", err)
	}
	if got := subagentViaLabel(h.parent, child2); got != "" {
		t.Errorf("via = %q, want empty for a child on the live pair", got)
	}
}
