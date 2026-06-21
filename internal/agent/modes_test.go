package agent

import (
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/modes"
	"github.com/undeadindustries/sagittarius/internal/provider"
)

func TestModeSwitchMidSession(t *testing.T) {
	t.Parallel()

	gen := &fakeGenerator{
		batches: [][]provider.StreamResponse{
			{{TextDelta: "first", Done: true}},
			{{TextDelta: "second", Done: true}},
		},
	}

	settings := &config.Settings{
		Sagittarius: &config.SagittariusSettings{
			Modes: &config.SagittariusModes{
				Plan: &config.SagittariusModeConfig{Model: "plan-model"},
			},
		},
	}

	runner, err := NewRunner(RunnerConfig{
		Generator:   gen,
		Model:       "provider-default",
		WorkDir:     t.TempDir(),
		Interactive: false,
		Settings:    settings,
		InitialMode: modes.ModeAgent,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	events, err := runner.RunTurn(testContext(t), "hello")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	_ = collectEvents(t, events)

	req := gen.lastRequest()
	if req == nil || req.Model != "provider-default" {
		t.Fatalf("first turn model = %q, want provider-default", modelFromReq(req))
	}

	runner.SetInteractionMode(modes.ModePlan)

	events, err = runner.RunTurn(testContext(t), "plan this")
	if err != nil {
		t.Fatalf("RunTurn after mode switch: %v", err)
	}
	_ = collectEvents(t, events)

	req = gen.lastRequest()
	if req == nil || req.Model != "plan-model" {
		t.Fatalf("second turn model = %q, want plan-model", modelFromReq(req))
	}
}

// TestExplicitAgentModeNotOverriddenByDefault guards that an explicit
// InitialMode: ModeAgent is honored even when sagittarius.defaultMode names a
// different mode. ModeAgent is the zero value, so a prior "0 means unset"
// fallback silently started in the settings default instead of agent.
func TestExplicitAgentModeNotOverriddenByDefault(t *testing.T) {
	t.Parallel()

	gen := &fakeGenerator{
		batches: [][]provider.StreamResponse{
			{{TextDelta: "ok", Done: true}},
		},
	}

	settings := &config.Settings{
		Sagittarius: &config.SagittariusSettings{
			DefaultMode: "plan",
			Modes: &config.SagittariusModes{
				Plan: &config.SagittariusModeConfig{Model: "plan-model"},
			},
		},
	}

	runner, err := NewRunner(RunnerConfig{
		Generator:   gen,
		Model:       "provider-default",
		WorkDir:     t.TempDir(),
		Interactive: false,
		Settings:    settings,
		InitialMode: modes.ModeAgent,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	if got := runner.InteractionMode(); got != modes.ModeAgent {
		t.Fatalf("interaction mode = %v, want ModeAgent", got)
	}

	events, err := runner.RunTurn(testContext(t), "hello")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	_ = collectEvents(t, events)

	req := gen.lastRequest()
	if req == nil || req.Model != "provider-default" {
		t.Fatalf("model = %q, want provider-default (explicit agent mode)", modelFromReq(req))
	}
}

func modelFromReq(req *provider.GenerateRequest) string {
	if req == nil {
		return ""
	}
	return req.Model
}
