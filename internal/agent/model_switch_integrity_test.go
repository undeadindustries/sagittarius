package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/modes"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/slash"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

func setupSwitchIntegrityApp(t *testing.T, withProjectShadow bool) (*App, *config.Documents) {
	t.Helper()
	t.Setenv("SAGITTARIUS_HOME", t.TempDir())
	t.Setenv("GEMINI_API_KEY", "test-gem-key")
	t.Setenv("ANTHROPIC_API_KEY", "test-anth-key")

	anthSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"ok\"}}\n\n" +
			"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n" +
			"data: {\"type\":\"message_stop\"}\n\n"))
	}))
	t.Cleanup(anthSrv.Close)

	projectDir := t.TempDir()
	docs, err := config.LoadDocuments(projectDir)
	if err != nil {
		t.Fatalf("LoadDocuments: %v", err)
	}
	docs.Global.Providers = &config.ProvidersSettings{
		Active: "anthropic",
		GeminiAPIKey: &config.ProviderInstanceConfig{
			Model:        "gemini-4-carbon",
			ActiveModels: []string{"gemini-4-carbon"},
		},
		Custom: map[string]config.CustomProviderDefinition{
			"anthropic": {
				DisplayName:  "Anthropic",
				BaseURL:      anthSrv.URL + "/v1/messages",
				APIKeyEnvVar: "ANTHROPIC_API_KEY",
				WireFormat:   config.WireFormatAnthropic,
				DefaultModel: "claude-opus-5-5",
			},
		},
		Extra: map[string]json.RawMessage{
			"anthropic": json.RawMessage(`{"model":"claude-opus-5-5","activeModels":["claude-opus-5-5"]}`),
		},
	}
	if err := docs.Save(config.ScopeGlobal); err != nil {
		t.Fatalf("save global: %v", err)
	}
	if withProjectShadow {
		if err := docs.MutateProject(func(project, _ *config.Settings) error {
			project.Providers = &config.ProvidersSettings{
				Active: "anthropic",
			}
			return nil
		}); err != nil {
			t.Fatalf("save project: %v", err)
		}
	}
	docs.ReloadMerged()

	runner, err := NewRunner(RunnerConfig{
		Generator:   &fakeGenerator{},
		Model:       "claude-opus-5-5",
		WorkDir:     projectDir,
		Settings:    docs.Merged(),
		Interactive: false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	app := NewApp(AppConfig{
		Runner:         runner,
		Loader:         docs.Loader(),
		Settings:       docs.Global,
		Documents:      docs,
		ProviderLabel:  "anthropic",
		Model:          "claude-opus-5-5",
		BaseProviderID: "anthropic",
		SessionID:      "switch-test",
	})
	return app, docs
}

func TestCycleModelUpdatesBaseProviderAcrossModeSwitches(t *testing.T) {
	app, _ := setupSwitchIntegrityApp(t, false)

	events, err := app.CycleModel(context.Background())
	if err != nil {
		t.Fatalf("CycleModel: %v", err)
	}
	for range events {
	}

	if got := app.baseProvider(); got != string(config.BuiltInGeminiAPIKey) {
		t.Fatalf("baseProvider = %q, want gemini-apikey", got)
	}
	if got := app.runner.ActiveProviderID(); got != string(config.BuiltInGeminiAPIKey) {
		t.Fatalf("active provider = %q, want gemini-apikey", got)
	}
	if got := app.runner.Model(); got != "gemini-4-carbon" {
		t.Fatalf("runner model = %q, want gemini-4-carbon", got)
	}

	// Cycling modes must stay on gemini-apikey, not snap back to stale anthropic.
	if _, err := app.deps.Hooks.SetInteractionMode(context.Background(), modes.ModePlan); err != nil {
		t.Fatalf("SetInteractionMode(plan): %v", err)
	}
	if _, err := app.deps.Hooks.SetInteractionMode(context.Background(), modes.ModeAgent); err != nil {
		t.Fatalf("SetInteractionMode(agent): %v", err)
	}
	if got := app.runner.ActiveProviderID(); got != string(config.BuiltInGeminiAPIKey) {
		t.Fatalf("active provider after mode cycle = %q, want gemini-apikey", got)
	}
	if got := app.runner.Model(); got != "gemini-4-carbon" {
		t.Fatalf("model after mode cycle = %q, want gemini-4-carbon", got)
	}
}

func TestGlobalPickUnderProjectShadowUpdatesOwningTierAndWarns(t *testing.T) {
	app, docs := setupSwitchIntegrityApp(t, true)

	resolved, shadowNotice, err := app.selectLiveModel(context.Background(), string(config.BuiltInGeminiAPIKey), "gemini-4-carbon", config.ScopeGlobal)
	if err != nil {
		t.Fatalf("selectLiveModel: %v", err)
	}
	if resolved != "gemini-4-carbon" {
		t.Fatalf("resolved = %q, want gemini-4-carbon", resolved)
	}
	if !strings.Contains(shadowNotice, "project settings") {
		t.Fatalf("shadowNotice = %q, want project settings notice", shadowNotice)
	}
	if got := docs.Project.Providers.Active; got != string(config.BuiltInGeminiAPIKey) {
		t.Fatalf("project active = %q, want gemini-apikey", got)
	}
}

func TestProviderMatchedModeOverrideAndNoInPlaceMergedMutation(t *testing.T) {
	app, docs := setupSwitchIntegrityApp(t, false)
	docs.Global.Sagittarius = &config.SagittariusSettings{
		Modes: &config.SagittariusModes{
			Plan: &config.SagittariusModeConfig{
				Provider: "anthropic",
				Model:    "claude-opus-5-5",
			},
		},
	}
	if err := docs.Save(config.ScopeGlobal); err != nil {
		t.Fatal(err)
	}
	docs.ReloadMerged()

	// Switch base to gemini.
	if _, _, err := app.selectLiveModel(context.Background(), string(config.BuiltInGeminiAPIKey), "gemini-4-carbon", config.ScopeGlobal); err != nil {
		t.Fatal(err)
	}
	// Mode plan has provider=anthropic, so entering plan sets runtimeProviderID=anthropic
	// without mutating docs.Global.Providers.Active!
	m, err := app.deps.Hooks.SetInteractionMode(context.Background(), modes.ModePlan)
	if err != nil {
		t.Fatalf("SetInteractionMode(plan): %v", err)
	}
	if m != "claude-opus-5-5" {
		t.Fatalf("plan model = %q, want claude-opus-5-5", m)
	}
	if docs.Global.Providers.Active != string(config.BuiltInGeminiAPIKey) {
		t.Fatalf("SetInteractionMode mutated Global active to %q!", docs.Global.Providers.Active)
	}

	// While in plan mode, switching to gemini must NOT apply plan's anthropic model to gemini.
	if _, _, err := app.selectLiveModel(context.Background(), string(config.BuiltInGeminiAPIKey), "gemini-4-carbon", config.ScopeGlobal); err != nil {
		t.Fatal(err)
	}
	if got := app.runner.Model(); got != "gemini-4-carbon" {
		t.Fatalf("runner model after switch in plan mode = %q, want gemini-4-carbon (not claude-opus-5-5)", got)
	}
}

func TestReasoningEffortPersistsPerModelAcrossSwitchesAndMatchesFooterAndSlash(t *testing.T) {
	app, docs := setupSwitchIntegrityApp(t, false)
	p := slash.NewProcessor()
	deps := app.deps

	// On anthropic claude-opus-5-5: set effort high.
	res := p.Process(context.Background(), "/reasoning high", deps)
	if res.Err != nil {
		t.Fatalf("/reasoning high: %v", res.Err)
	}
	if got := app.ComposerStatus().ReasoningEffort; got != "high" {
		t.Fatalf("opus footer effort = %q, want high", got)
	}

	// Switch to gemini-4-carbon: set effort low.
	if _, _, err := app.selectLiveModel(context.Background(), string(config.BuiltInGeminiAPIKey), "gemini-4-carbon", config.ScopeGlobal); err != nil {
		t.Fatal(err)
	}
	if got := app.ComposerStatus().ReasoningEffort; got != "" {
		t.Fatalf("carbon initial footer effort = %q, want empty", got)
	}
	res = p.Process(context.Background(), "/reasoning low", deps)
	if res.Err != nil {
		t.Fatalf("/reasoning low: %v", res.Err)
	}
	if got := app.ComposerStatus().ReasoningEffort; got != "low" {
		t.Fatalf("carbon footer effort = %q, want low", got)
	}

	// Switch back to anthropic claude-opus-5-5: high must be restored, and /reasoning show agrees.
	if _, _, err := app.selectLiveModel(context.Background(), "anthropic", "claude-opus-5-5", config.ScopeGlobal); err != nil {
		t.Fatal(err)
	}
	if got := app.ComposerStatus().ReasoningEffort; got != "high" {
		t.Fatalf("back on opus footer effort = %q, want high", got)
	}
	if req := app.runner.buildGenerateRequest(); req.Reasoning == nil || req.Reasoning.Effort != "high" {
		t.Fatalf("opus request reasoning = %+v, want high", req.Reasoning)
	}
	show := p.Process(context.Background(), "/reasoning show", deps)
	joined := strings.Join(show.Messages, "\n")
	if !strings.Contains(joined, "claude-opus-5-5") || !strings.Contains(joined, "high") {
		t.Fatalf("/reasoning show = %q, want claude-opus-5-5 and high", joined)
	}

	// /reasoning clear removes the per-model pin.
	_ = p.Process(context.Background(), "/reasoning clear", deps)
	if pin := currentModelReasoningPin(docs.Merged(), "anthropic", "claude-opus-5-5"); pin != "" {
		t.Fatalf("pin after clear = %q, want empty", pin)
	}
}

func TestAuxGeneratorDoesNotShareMainResponsesChainAndCompressionGuard(t *testing.T) {
	t.Setenv("SAGITTARIUS_HOME", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "test-openai-key")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n" +
			"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\"}}\n\n"))
	}))
	t.Cleanup(srv.Close)

	chainingOn := true
	settings := &config.Settings{
		Providers: &config.ProvidersSettings{
			Active: string(config.BuiltInOpenAIResponses),
			Custom: map[string]config.CustomProviderDefinition{
				string(config.BuiltInOpenAIResponses): {
					DisplayName:  "OpenAI Responses",
					BaseURL:      srv.URL + "/v1/responses",
					APIKeyEnvVar: "OPENAI_API_KEY",
					WireFormat:   config.WireFormatOpenAIResponses,
					DefaultModel: "gpt-5",
				},
			},
			OpenAIResponses: &config.ProviderInstanceConfig{
				Model:               "gpt-5",
				UseResponseChaining: &chainingOn,
			},
		},
		Sagittarius: &config.SagittariusSettings{
			Compression: &config.SagittariusUtilityConfig{Model: "claude-haiku-5-5"},
		},
	}
	mainGen, err := provider.NewContentGenerator(context.Background(), settings)
	if err != nil {
		t.Fatalf("NewContentGenerator: %v", err)
	}
	runner, err := NewRunner(RunnerConfig{
		Generator: mainGen,
		Model:     "gpt-5",
		WorkDir:   t.TempDir(),
		Settings:  settings,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	aux, err := runner.auxGenerator(context.Background())
	if err != nil {
		t.Fatalf("auxGenerator: %v", err)
	}
	if aux == mainGen {
		t.Fatal("auxGenerator returned main Responses generator; aux calls would corrupt main lastResponseID")
	}
	if got := runner.CompressionModel(); got != "gpt-5" {
		t.Fatalf("CompressionModel = %q, want live gpt-5 when override names foreign claude model", got)
	}
}

func TestModelSwitchConfirmThresholdAndSingleRebuild(t *testing.T) {
	app, docs := setupSwitchIntegrityApp(t, false)
	// Seed > 100k live context tokens.
	app.runner.metrics.recordTurnUsage(usageKey{provider: "anthropic", model: "claude-opus-5-5"}, provider.Usage{InputTokens: 125_000})

	if warn := app.ModelSwitchWarningFor(string(config.BuiltInGeminiAPIKey), "gemini-4-carbon"); !strings.Contains(warn, "125k") {
		t.Fatalf("warning = %q, want ~125k warning", warn)
	}
	if warn := app.ModelSwitchWarningFor("anthropic", "claude-opus-5-5"); warn != "" {
		t.Fatalf("same-model warning = %q, want empty", warn)
	}

	zero := 0
	docs.Global.Sagittarius = &config.SagittariusSettings{ModelSwitchConfirmTokens: &zero}
	if err := docs.Save(config.ScopeGlobal); err != nil {
		t.Fatal(err)
	}
	docs.ReloadMerged()
	if warn := app.ModelSwitchWarningFor(string(config.BuiltInGeminiAPIKey), "gemini-4-carbon"); warn != "" {
		t.Fatalf("threshold=0 warning = %q, want empty", warn)
	}
}

func TestCrossProviderSessionRoundTripPreservesAndIsolatesHistory(t *testing.T) {
	t.Setenv("SAGITTARIUS_HOME", t.TempDir())
	var anthSeenThink bool
	var anthCallCount int
	anthSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		anthCallCount++
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		for _, rawMsg := range body["messages"].([]any) {
			m := rawMsg.(map[string]any)
			if m["role"] == "assistant" {
				for _, b := range m["content"].([]any) {
					blk := b.(map[string]any)
					if blk["type"] == "thinking" && blk["signature"] == "gem-sig" {
						t.Errorf("Gemini signature leaked into Anthropic thinking block: %#v", blk)
					}
					if blk["type"] == "thinking" && blk["signature"] == "anth-sig-1" {
						anthSeenThink = true
					}
				}
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"\",\"signature\":\"\"}}\n\n" +
			"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"anth thought\"}}\n\n" +
			"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"anth-sig-1\"}}\n\n" +
			"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
			"data: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"text\",\"text\":\"from anth\"}}\n\n" +
			"data: {\"type\":\"content_block_stop\",\"index\":1}\n\n" +
			"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":4}}\n\n" +
			"data: {\"type\":\"message_stop\"}\n\n"))
	}))
	t.Cleanup(anthSrv.Close)

	gemGen := &reasoningStreamGen{
		rounds: []provider.StreamResponse{
			{
				ModelParts: []provider.Part{{Text: "from gem", ThoughtSignature: []byte("gem-sig")}},
				TextDelta:  "from gem",
				Done:       true,
			},
			{
				ModelParts: []provider.Part{{Text: "from gem 2", ThoughtSignature: []byte("gem-sig-2")}},
				TextDelta:  "from gem 2",
				Done:       true,
			},
		},
	}
	anthGen, err := provider.NewAnthropicGenerator(provider.AnthropicConfig{
		BaseURL:    anthSrv.URL + "/v1/messages",
		Model:      "claude-opus-5-5",
		HTTPClient: anthSrv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}

	settingsGem := &config.Settings{
		Providers: &config.ProvidersSettings{
			Active:       string(config.BuiltInGeminiAPIKey),
			GeminiAPIKey: &config.ProviderInstanceConfig{Model: "gemini-4-carbon"},
		},
	}
	settingsAnth := &config.Settings{
		Providers: &config.ProvidersSettings{
			Active: "anthropic",
			Custom: map[string]config.CustomProviderDefinition{
				"anthropic": {BaseURL: anthSrv.URL + "/v1/messages", WireFormat: config.WireFormatAnthropic, DefaultModel: "claude-opus-5-5"},
			},
		},
	}

	runner, err := NewRunner(RunnerConfig{
		Generator: gemGen,
		Model:     "gemini-4-carbon",
		WorkDir:   t.TempDir(),
		Settings:  settingsGem,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Turn 1: Gemini
	ev, err := runner.RunTurn(testContext(t), "gem 1")
	if err != nil {
		t.Fatal(err)
	}
	collectEvents(t, ev)

	// Turn 2: Switch to Anthropic -> Gemini signature must NOT become thinking block.
	runner.SetGenerator(anthGen)
	runner.ApplyEndpointState(settingsAnth, "claude-opus-5-5")
	ev, err = runner.RunTurn(testContext(t), "anth 1")
	if err != nil {
		t.Fatal(err)
	}
	collectEvents(t, ev)

	// Turn 3: Switch back to Gemini -> Anthropic thinking must NOT reach Gemini, yet stay in history!
	runner.SetGenerator(gemGen)
	runner.ApplyEndpointState(settingsGem, "gemini-4-carbon")
	ev, err = runner.RunTurn(testContext(t), "gem 2")
	if err != nil {
		t.Fatal(err)
	}
	collectEvents(t, ev)
	lastGemReq := gemGen.requests[len(gemGen.requests)-1]
	for _, m := range lastGemReq.Messages {
		for _, p := range m.Parts {
			if string(p.ThoughtSignature) == "anth-sig-1" || p.Reasoning == "anth thought" {
				t.Fatalf("Anthropic thinking leaked to Gemini request: %#v", p)
			}
		}
	}

	// Turn 4: Switch back to Anthropic -> Anthropic thinking survived in history and replays!
	runner.SetGenerator(anthGen)
	runner.ApplyEndpointState(settingsAnth, "claude-opus-5-5")
	ev, err = runner.RunTurn(testContext(t), "anth 2")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range collectEvents(t, ev) {
		if e.Type == ui.StreamError {
			t.Fatalf("stream error: %v %s", e.Err, e.Text)
		}
	}
	if anthCallCount != 2 || !anthSeenThink {
		t.Fatalf("anthCallCount=%d anthSeenThink=%v, want 2 / true", anthCallCount, anthSeenThink)
	}
}

// TestPinnedCLIModelSurvivesModeSwitchAndUnpinsOnModelPick guards Bugbot #1:
// switching modes while --model is pinned must not swap the generator to a
// mode's provider override or revert to baseProviderID, and an explicit
// same-provider /model pick must release the pin.
func TestPinnedCLIModelSurvivesModeSwitchAndUnpinsOnModelPick(t *testing.T) {
	app, docs := setupSwitchIntegrityApp(t, false)
	// Base persisted provider is gemini-apikey; plan mode overrides to gemini-4-carbon;
	// simulate launching with `-m anthropic/claude-opus-5-5` (runtimeProviderID=anthropic, pinned).
	docs.Global.Providers.Active = string(config.BuiltInGeminiAPIKey)
	docs.Global.Sagittarius = &config.SagittariusSettings{
		Modes: &config.SagittariusModes{
			Plan: &config.SagittariusModeConfig{
				Provider: string(config.BuiltInGeminiAPIKey),
				Model:    "gemini-4-carbon",
			},
		},
	}
	if err := docs.Save(config.ScopeGlobal); err != nil {
		t.Fatal(err)
	}
	docs.ReloadMerged()

	app.setProvidersState(string(config.BuiltInGeminiAPIKey), "anthropic")
	app.runner.PinModel("claude-opus-5-5")
	if _, _, err := app.deps.Hooks.RebuildRunner(context.Background()); err != nil {
		t.Fatalf("initial RebuildRunner: %v", err)
	}
	if got := app.runner.ActiveProviderID(); got != "anthropic" {
		t.Fatalf("before mode switch ActiveProviderID = %q, want anthropic", got)
	}

	// Switching to plan (and back to agent) while pinned must keep provider=anthropic and model=claude-opus-5-5.
	if _, err := app.deps.Hooks.SetInteractionMode(context.Background(), modes.ModePlan); err != nil {
		t.Fatalf("SetInteractionMode(plan): %v", err)
	}
	if got := app.runner.ActiveProviderID(); got != "anthropic" {
		t.Fatalf("plan ActiveProviderID = %q, want anthropic (must not swap provider while pinned)", got)
	}
	if got := app.runner.Model(); got != "claude-opus-5-5" {
		t.Fatalf("plan Model = %q, want claude-opus-5-5", got)
	}
	if _, err := app.deps.Hooks.SetInteractionMode(context.Background(), modes.ModeAgent); err != nil {
		t.Fatalf("SetInteractionMode(agent): %v", err)
	}
	if got := app.runner.ActiveProviderID(); got != "anthropic" {
		t.Fatalf("agent ActiveProviderID = %q, want anthropic", got)
	}

	// An explicit same-provider /model pick releases the CLI pin.
	if _, _, err := app.selectLiveModel(context.Background(), "anthropic", "claude-opus-5-5", config.ScopeGlobal); err != nil {
		t.Fatalf("selectLiveModel(same provider): %v", err)
	}
	if app.runner.ModelPinned() {
		t.Fatal("ModelPinned should be false after explicit /model pick on the same provider")
	}
}
