package agent

import (
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/modes"
)

// TestChildInheritsConstraints: a standing session constraint the user set on
// the parent must bind the child — a leased write must not be able to violate
// a scope limit the parent was told to hold.
func TestChildInheritsConstraints(t *testing.T) {
	t.Parallel()

	h := newSubagentHarness(t, openAISettingsWithModelPins(nil))
	if err := h.parent.AddConstraint("do not touch go.mod"); err != nil {
		t.Fatalf("AddConstraint: %v", err)
	}
	child, err := h.parent.newSubagent(t.Context(), subagentSpec{
		description: "constraints",
		mode:        modes.ModeAgent,
		class:       config.SubagentCoding,
		approval:    ApprovalYolo,
	})
	if err != nil {
		t.Fatalf("newSubagent: %v", err)
	}
	got := child.runner.Constraints()
	if len(got) != 1 || got[0] != "do not touch go.mod" {
		t.Errorf("child constraints = %v, want the parent's constraint", got)
	}
}

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

	if got := subagentBadge(h.parent, config.SubagentResearch); got != "openai/child-model" {
		t.Errorf("research badge = %q, want openai/child-model (legacy model-only pin resolves provider from live)", got)
	}
	if got := subagentBadge(h.parent, config.SubagentCoding); got != "openrouter/qwen/qwen3-coder" {
		t.Errorf("coding badge = %q, want openrouter/qwen/qwen3-coder", got)
	}
	// Reviewer has no pin: it resolves to the live pair, and the badge shows
	// it — the card exists to make routing visible even when nothing is
	// pinned.
	if got, want := subagentBadge(h.parent, config.SubagentReviewer), "openai/parent-model"; got != want {
		t.Errorf("unpinned reviewer badge = %q, want %q (the live pair)", got, want)
	}
}

func TestSubagentBadgeShowsLivePairWhenPinMatches(t *testing.T) {
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
	if got, want := subagentBadge(h.parent, config.SubagentResearch), "openai/parent-model"; got != want {
		t.Errorf("badge = %q, want %q even when the pin matches the live pair", got, want)
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
