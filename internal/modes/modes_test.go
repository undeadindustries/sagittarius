package modes

import (
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
)

func TestModeSelectsModel(t *testing.T) {
	t.Parallel()

	cfg := &config.SagittariusSettings{
		DefaultModel: "global-default",
		Modes: &config.SagittariusModes{
			Plan: &config.SagittariusModeConfig{Model: "plan-model"},
			Ask:  &config.SagittariusModeConfig{Model: "ask-model"},
		},
	}
	providerDefault := "provider-default"

	tests := []struct {
		mode Mode
		want string
	}{
		{ModeAgent, "global-default"},
		{ModePlan, "plan-model"},
		{ModeAsk, "ask-model"},
		{ModeDebug, "global-default"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.mode.String(), func(t *testing.T) {
			t.Parallel()
			if got := ResolveModel(tc.mode, cfg, providerDefault); got != tc.want {
				t.Fatalf("ResolveModel(%v) = %q, want %q", tc.mode, got, tc.want)
			}
		})
	}
}

func TestSubagentModelFallback(t *testing.T) {
	t.Parallel()

	cfg := &config.SagittariusSettings{
		DefaultModel: "global-default",
		Subagents: &config.SagittariusSubagents{
			Default: config.SagittariusSubagentConfig{Model: "subagent-default"},
			Named: map[string]config.SagittariusSubagentConfig{
				"investigator": {Model: "investigator-model"},
			},
		},
	}
	providerDefault := "provider-default"

	tests := []struct {
		name string
		want string
	}{
		{"investigator", "investigator-model"},
		{"unknown", "subagent-default"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ResolveSubagentModel(tc.name, cfg, providerDefault); got != tc.want {
				t.Fatalf("ResolveSubagentModel(%q) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

func TestGlobalDefaultWhenUnset(t *testing.T) {
	t.Parallel()

	providerDefault := "provider-default"

	tests := []struct {
		name string
		cfg  *config.SagittariusSettings
		mode Mode
		want string
	}{
		{
			name: "nil settings uses provider default",
			cfg:  nil,
			mode: ModeAgent,
			want: providerDefault,
		},
		{
			name: "empty sagittarius uses provider default",
			cfg:  &config.SagittariusSettings{},
			mode: ModePlan,
			want: providerDefault,
		},
		{
			name: "global default wins over provider",
			cfg:  &config.SagittariusSettings{DefaultModel: "global-default"},
			mode: ModeAgent,
			want: "global-default",
		},
		{
			name: "subagent falls through to provider when no subagent keys",
			cfg:  &config.SagittariusSettings{},
			mode: ModeAgent,
			want: providerDefault,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ResolveModel(tc.mode, tc.cfg, providerDefault); got != tc.want {
				t.Fatalf("ResolveModel() = %q, want %q", got, tc.want)
			}
		})
	}

	if got := ResolveSubagentModel("any", nil, providerDefault); got != providerDefault {
		t.Fatalf("ResolveSubagentModel nil cfg = %q, want %q", got, providerDefault)
	}
}

func TestParseMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want Mode
	}{
		{"agent", ModeAgent},
		{"plan", ModePlan},
		{"ASK", ModeAsk},
		{"", ModeAgent},
	}
	for _, tc := range tests {
		got, err := ParseMode(tc.in)
		if err != nil {
			t.Fatalf("ParseMode(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("ParseMode(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
