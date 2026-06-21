package slash_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/agents"
	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/credentials"
	"github.com/undeadindustries/sagittarius/internal/mcp"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/skills"
	"github.com/undeadindustries/sagittarius/internal/slash"
)

type mockHooks struct {
	rebuildCalls int
	reloadCalls  int
	models       []provider.ModelInfo
	storedKeys   map[string]string
	rebuildLabel string
	rebuildModel string
}

func (m *mockHooks) RebuildRunner(context.Context) (string, string, error) {
	m.rebuildCalls++
	return m.rebuildLabel, m.rebuildModel, nil
}

func (m *mockHooks) ReloadSystemInstruction(context.Context) error {
	m.reloadCalls++
	return nil
}

func (m *mockHooks) DiscoverModels(context.Context) []provider.ModelInfo {
	return m.models
}

func (m *mockHooks) SetProviderAPIKey(_ context.Context, providerID, apiKey string) error {
	if m.storedKeys == nil {
		m.storedKeys = map[string]string{}
	}
	m.storedKeys[providerID] = apiKey
	return nil
}

func (m *mockHooks) ReloadMCP(context.Context) (string, error) {
	return "MCP servers reloaded.", nil
}

func (m *mockHooks) ReloadSkills(context.Context) (string, error) {
	return "Agent skills reloaded successfully.", nil
}

func (m *mockHooks) ReloadAgents(context.Context) (agents.ReloadSummary, error) {
	return agents.ReloadSummary{TotalLoaded: 0}, nil
}

func (m *mockHooks) MCPStates() []mcp.ServerState { return nil }

func (m *mockHooks) SkillList() []skills.Definition { return nil }

func (m *mockHooks) AgentList() []agents.Definition { return nil }

func testDeps(t *testing.T, settings *config.Settings) (slash.Deps, *config.Loader, *mockHooks) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	loader, err := config.NewLoader(config.WithSettingsPath(path))
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}
	if settings == nil {
		settings = &config.Settings{
			Providers: &config.ProvidersSettings{
				Active: string(config.BuiltInOpenAI),
				OpenAI: &config.ProviderInstanceConfig{},
			},
			Raw: map[string]json.RawMessage{},
		}
	}
	hooks := &mockHooks{rebuildLabel: "OpenAI", rebuildModel: "gpt-4o-mini"}
	return slash.Deps{Loader: loader, Settings: settings, Hooks: hooks}, loader, hooks
}

func TestHelpListsProviderSubcommands(t *testing.T) {
	t.Parallel()
	p := slash.NewProcessor()
	help := p.Registry().RenderHelp()

	checks := []string{
		"/help",
		"/quit",
		"/provider",
		"/provider list",
		"/provider use",
		"/provider show",
		"/provider set",
		"/provider add",
		"/provider remove",
		"/model",
		"/auth",
		"/memory reload",
		"/skills reload",
		"/mcp reload",
		"/agents reload",
		"List slash commands",
		"Switch the active provider",
	}
	for _, want := range checks {
		if !strings.Contains(help, want) {
			t.Errorf("help missing %q\n%s", want, help)
		}
	}
}

func TestProviderUsePersists(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	settings := &config.Settings{
		Providers: &config.ProvidersSettings{
			Active:       string(config.BuiltInGeminiAPIKey),
			GeminiAPIKey: &config.ProviderInstanceConfig{},
		},
		Raw: map[string]json.RawMessage{},
	}
	deps, loader, hooks := testDeps(t, settings)
	p := slash.NewProcessor()

	result := p.Process(ctx, "/provider use openai", deps)
	if !result.Handled {
		t.Fatal("expected handled")
	}
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if settings.ActiveProvider() != string(config.BuiltInOpenAI) {
		t.Fatalf("active = %q, want openai", settings.ActiveProvider())
	}

	reloaded, err := loader.Load()
	if err != nil && !strings.Contains(err.Error(), "secrets") {
		t.Fatalf("reload settings: %v", err)
	}
	if reloaded.ActiveProvider() != string(config.BuiltInOpenAI) {
		t.Fatalf("persisted active = %q, want openai", reloaded.ActiveProvider())
	}
	if hooks.rebuildCalls != 1 {
		t.Fatalf("rebuild calls = %d, want 1", hooks.rebuildCalls)
	}
}

func TestQuitExits(t *testing.T) {
	t.Parallel()
	p := slash.NewProcessor()
	result := p.Process(context.Background(), "/quit", slash.Deps{})
	if !result.Handled || !result.Quit {
		t.Fatalf("result = %+v, want handled+quit", result)
	}
}

func TestAuthStoresKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	settings := &config.Settings{
		Providers: &config.ProvidersSettings{
			Active:       string(config.BuiltInGeminiAPIKey),
			GeminiAPIKey: &config.ProviderInstanceConfig{},
		},
		Raw: map[string]json.RawMessage{},
	}
	deps, _, hooks := testDeps(t, settings)
	p := slash.NewProcessor()

	result := p.Process(ctx, "/auth test-secret-key", deps)
	if result.Err != nil {
		t.Fatalf("auth error: %v", result.Err)
	}
	if hooks.storedKeys[string(config.BuiltInGeminiAPIKey)] != "test-secret-key" {
		t.Fatalf("stored key = %q", hooks.storedKeys[string(config.BuiltInGeminiAPIKey)])
	}
	combined := strings.Join(result.Messages, " ")
	if strings.Contains(combined, "test-secret-key") {
		t.Fatal("message must not contain raw api key")
	}
	if !strings.Contains(combined, credentials.Redact("x")) {
		t.Fatalf("message should contain redacted placeholder: %q", combined)
	}
}

func TestProviderSetRejectedForGemini(t *testing.T) {
	t.Parallel()
	settings := &config.Settings{
		Providers: &config.ProvidersSettings{
			Active:       string(config.BuiltInGeminiAPIKey),
			GeminiAPIKey: &config.ProviderInstanceConfig{},
		},
		Raw: map[string]json.RawMessage{},
	}
	deps, _, hooks := testDeps(t, settings)
	p := slash.NewProcessor()

	result := p.Process(context.Background(), "/provider set gemini-apikey key secret", deps)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	combined := strings.Join(result.Messages, " ")
	if !strings.Contains(combined, "/auth") {
		t.Fatalf("expected /auth guidance, got: %q", combined)
	}
	if _, ok := hooks.storedKeys[string(config.BuiltInGeminiAPIKey)]; ok {
		t.Fatal("gemini key should not be stored via /provider set")
	}
}

func TestHelpCommandViaProcessor(t *testing.T) {
	t.Parallel()
	p := slash.NewProcessor()
	result := p.Process(context.Background(), "/help", slash.Deps{})
	if !result.Handled || result.Quit {
		t.Fatalf("result = %+v", result)
	}
	combined := strings.Join(result.Messages, "\n")
	for _, name := range []string{"/provider", "/model", "/auth", "/memory", "/skills"} {
		if !strings.Contains(combined, name) {
			t.Errorf("help output missing %s", name)
		}
	}
}

func TestMemoryReloadStub(t *testing.T) {
	t.Parallel()
	deps, _, hooks := testDeps(t, nil)
	p := slash.NewProcessor()
	result := p.Process(context.Background(), "/memory reload", deps)
	if result.Err != nil {
		t.Fatalf("reload error: %v", result.Err)
	}
	if hooks.reloadCalls != 1 {
		t.Fatalf("reload calls = %d", hooks.reloadCalls)
	}
}

func TestProviderAddRemove(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	settings := &config.Settings{
		Providers: &config.ProvidersSettings{
			Active: string(config.BuiltInOpenAI),
			OpenAI: &config.ProviderInstanceConfig{},
			Custom: map[string]config.CustomProviderDefinition{},
		},
		Raw: map[string]json.RawMessage{},
	}
	deps, loader, _ := testDeps(t, settings)
	p := slash.NewProcessor()

	add := p.Process(ctx, "/provider add local-vllm http://127.0.0.1:8000/v1 Local", deps)
	if add.Err != nil {
		t.Fatalf("add: %v", add.Err)
	}
	reloaded, err := loader.Load()
	if err != nil && !strings.Contains(err.Error(), "secrets") {
		t.Fatalf("load: %v", err)
	}
	if reloaded.Providers.Custom["local-vllm"].BaseURL == "" {
		t.Fatal("custom provider not persisted")
	}

	rm := p.Process(ctx, "/provider remove local-vllm", deps)
	if rm.Err != nil {
		t.Fatalf("remove: %v", rm.Err)
	}
}

func TestReasoningNotApplicableOnGemini(t *testing.T) {
	t.Parallel()
	provider.ClearSessionReasoningOverride()

	settings := &config.Settings{
		Providers: &config.ProvidersSettings{
			Active:       string(config.BuiltInGeminiAPIKey),
			GeminiAPIKey: &config.ProviderInstanceConfig{},
		},
		Raw: map[string]json.RawMessage{},
	}
	deps, _, _ := testDeps(t, settings)
	p := slash.NewProcessor()

	result := p.Process(context.Background(), "/reasoning show", deps)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	combined := strings.Join(result.Messages, "\n")
	if !strings.Contains(combined, "not applicable") && !strings.Contains(combined, "only applies to OpenAI Responses") {
		t.Fatalf("expected not-applicable message, got: %q", combined)
	}
}

func TestReasoningApplicableOnResponses(t *testing.T) {
	t.Parallel()
	provider.ClearSessionReasoningOverride()
	t.Cleanup(provider.ClearSessionReasoningOverride)

	settings := &config.Settings{
		Providers: &config.ProvidersSettings{
			Active:          string(config.BuiltInOpenAIResponses),
			OpenAIResponses: &config.ProviderInstanceConfig{ReasoningEffort: "low"},
		},
		Raw: map[string]json.RawMessage{},
	}
	deps, _, _ := testDeps(t, settings)
	p := slash.NewProcessor()

	show := p.Process(context.Background(), "/reasoning show", deps)
	if show.Err != nil {
		t.Fatalf("show error: %v", show.Err)
	}
	combined := strings.Join(show.Messages, "\n")
	if !strings.Contains(combined, "low") {
		t.Fatalf("expected resolved low effort, got: %q", combined)
	}

	set := p.Process(context.Background(), "/reasoning high", deps)
	if set.Err != nil {
		t.Fatalf("set error: %v", set.Err)
	}
	if provider.SessionReasoningOverride() != "high" {
		t.Fatalf("override = %q, want high", provider.SessionReasoningOverride())
	}
}
