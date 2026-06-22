package modes

import (
	"strings"
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
		// No mode model and no provider-scoped default: the provider default wins
		// over the legacy global default.
		{ModeAgent, "provider-default"},
		{ModePlan, "plan-model"},
		{ModeAsk, "ask-model"},
		{ModeDebug, "provider-default"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.mode.String(), func(t *testing.T) {
			t.Parallel()
			if got := ResolveModel(tc.mode, cfg, "openai", providerDefault); got != tc.want {
				t.Fatalf("ResolveModel(%v) = %q, want %q", tc.mode, got, tc.want)
			}
		})
	}
}

// TestModeOverrideBeatsAllDefaults pins the core guarantee: a per-mode model
// override wins over the provider-scoped default, the provider default, and the
// legacy global default.
func TestModeOverrideBeatsAllDefaults(t *testing.T) {
	t.Parallel()

	cfg := &config.SagittariusSettings{
		DefaultModel:  "legacy-global",
		DefaultModels: map[string]string{"openai": "provider-scoped"},
		Modes: &config.SagittariusModes{
			Plan:  &config.SagittariusModeConfig{Model: "plan-model"},
			Ask:   &config.SagittariusModeConfig{Model: "ask-model"},
			Debug: &config.SagittariusModeConfig{Model: "debug-model"},
			Agent: &config.SagittariusModeConfig{Model: "agent-model"},
		},
	}

	tests := []struct {
		mode Mode
		want string
	}{
		{ModePlan, "plan-model"},
		{ModeAsk, "ask-model"},
		{ModeDebug, "debug-model"},
		{ModeAgent, "agent-model"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.mode.String(), func(t *testing.T) {
			t.Parallel()
			if got := ResolveModel(tc.mode, cfg, "openai", "provider-default"); got != tc.want {
				t.Fatalf("ResolveModel(%v) = %q, want %q (mode override must win)", tc.mode, got, tc.want)
			}
		})
	}
}

// TestDefaultModelsPerProvider checks that a provider-scoped default applies only
// to its matching provider id, beats the provider default, but yields to a mode
// model. It also tolerates the short "gemini" alias.
func TestDefaultModelsPerProvider(t *testing.T) {
	t.Parallel()

	cfg := &config.SagittariusSettings{
		DefaultModel: "legacy-global",
		DefaultModels: map[string]string{
			"openai":        "gpt-4o-mini",
			"gemini-apikey": "gemini-2.5-flash",
		},
	}

	tests := []struct {
		name       string
		mode       Mode
		providerID string
		want       string
	}{
		{"openai scoped beats provider default", ModeAgent, "openai", "gpt-4o-mini"},
		{"gemini canonical id", ModeAgent, "gemini-apikey", "gemini-2.5-flash"},
		{"gemini short alias resolves", ModeAgent, "gemini", "gemini-2.5-flash"},
		{"unlisted provider falls back to provider default", ModeAgent, "my-vllm", "provider-default"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ResolveModel(tc.mode, cfg, tc.providerID, "provider-default"); got != tc.want {
				t.Fatalf("ResolveModel(%q) = %q, want %q", tc.providerID, got, tc.want)
			}
		})
	}
}

// TestGlobalDefaultFallbackWhenProviderUnset confirms the legacy global default
// is only used when neither a mode model, a provider-scoped default, nor a
// provider default is available.
func TestGlobalDefaultFallbackWhenProviderUnset(t *testing.T) {
	t.Parallel()

	cfg := &config.SagittariusSettings{DefaultModel: "legacy-global"}

	if got := ResolveModel(ModeAgent, cfg, "openai", ""); got != "legacy-global" {
		t.Fatalf("ResolveModel with empty provider default = %q, want legacy-global", got)
	}
	if got := ResolveModel(ModeAgent, cfg, "openai", "provider-default"); got != "provider-default" {
		t.Fatalf("ResolveModel with provider default = %q, want provider-default", got)
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
	liveModel := "live-model"

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
			if got := ResolveSubagentModel(tc.name, cfg, liveModel); got != tc.want {
				t.Fatalf("ResolveSubagentModel(%q) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}

	// With no subagent override, a subagent follows the live model.
	if got := ResolveSubagentModel("unknown", &config.SagittariusSettings{}, liveModel); got != liveModel {
		t.Fatalf("ResolveSubagentModel no override = %q, want live model %q", got, liveModel)
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
			name: "provider default wins over global default",
			cfg:  &config.SagittariusSettings{DefaultModel: "global-default"},
			mode: ModeAgent,
			want: providerDefault,
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
			if got := ResolveModel(tc.mode, tc.cfg, "openai", providerDefault); got != tc.want {
				t.Fatalf("ResolveModel() = %q, want %q", got, tc.want)
			}
		})
	}

	if got := ResolveSubagentModel("any", nil, providerDefault); got != providerDefault {
		t.Fatalf("ResolveSubagentModel nil cfg = %q, want live model %q", got, providerDefault)
	}
}

func TestUtilityModelsDefaultToLiveModel(t *testing.T) {
	t.Parallel()

	live := "live-model"

	// No overrides: every auxiliary role follows the live model.
	if got := ResolveCompressionModel(nil, live); got != live {
		t.Fatalf("ResolveCompressionModel default = %q, want %q", got, live)
	}
	if got := ResolveToolsModel(&config.SagittariusSettings{}, live); got != live {
		t.Fatalf("ResolveToolsModel default = %q, want %q", got, live)
	}

	// Overrides win over the live model.
	cfg := &config.SagittariusSettings{
		Compression: &config.SagittariusUtilityConfig{Model: "compressor-model"},
		Tools:       &config.SagittariusUtilityConfig{Model: "tools-model"},
	}
	if got := ResolveCompressionModel(cfg, live); got != "compressor-model" {
		t.Fatalf("ResolveCompressionModel override = %q, want compressor-model", got)
	}
	if got := ResolveToolsModel(cfg, live); got != "tools-model" {
		t.Fatalf("ResolveToolsModel override = %q, want tools-model", got)
	}

	// An empty override Model falls back to the live model.
	empty := &config.SagittariusSettings{Compression: &config.SagittariusUtilityConfig{}}
	if got := ResolveCompressionModel(empty, live); got != live {
		t.Fatalf("ResolveCompressionModel empty override = %q, want %q", got, live)
	}
}

func TestSystemPromptSuffixBuiltinReadOnlyModes(t *testing.T) {
	t.Parallel()

	plan := SystemPromptSuffix(ModePlan, nil)
	if plan == "" || !strings.Contains(plan, "CRITICAL") || !strings.Contains(plan, "docs/plans") {
		t.Fatalf("plan suffix = %q, want CRITICAL read-only framing", plan)
	}

	ask := SystemPromptSuffix(ModeAsk, nil)
	if ask == "" || !strings.Contains(ask, "STRICTLY FORBIDDEN") {
		t.Fatalf("ask suffix = %q, want STRICTLY FORBIDDEN framing", ask)
	}

	if got := SystemPromptSuffix(ModeAgent, nil); got != "" {
		t.Fatalf("agent suffix = %q, want empty", got)
	}

	custom := SystemPromptSuffix(ModePlan, &config.SagittariusSettings{
		Modes: &config.SagittariusModes{
			Plan: &config.SagittariusModeConfig{SystemPromptSuffix: "User custom."},
		},
	})
	if !strings.Contains(custom, "CRITICAL") || !strings.Contains(custom, "User custom.") {
		t.Fatalf("custom+plan suffix = %q", custom)
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
