package slash_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/agents"
	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/mcp"
	"github.com/undeadindustries/sagittarius/internal/modes"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/session"
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

func (m *mockHooks) ListSessions() ([]session.SessionInfo, error) { return nil, nil }

func (m *mockHooks) ClearHistory() error { return nil }

func (m *mockHooks) SetInteractionMode(context.Context, modes.Mode) (string, error) {
	return "gpt-4o-mini", nil
}

func (m *mockHooks) InteractionMode() (modes.Mode, string) {
	return modes.ModeAgent, "gpt-4o-mini"
}

func (m *mockHooks) SnapshotDiff(string) (string, error) { return "", nil }

func (m *mockHooks) SnapshotUndo(int) ([]string, error) { return nil, nil }

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

func TestHelpListsCommands(t *testing.T) {
	t.Parallel()
	p := slash.NewProcessor()
	help := p.Registry().RenderHelp()

	checks := []string{
		"/help",
		"/quit",
		"/providers",
		"/models",
		"/memory reload",
		"/skills reload",
		"/mcp reload",
		"/agents reload",
		"/mode",
		"/mode show",
		"List slash commands",
	}
	for _, want := range checks {
		if !strings.Contains(help, want) {
			t.Errorf("help missing %q\n%s", want, help)
		}
	}
	// The provider/model subcommand trees were retired (menu-first commands).
	for _, gone := range []string{"/providers list", "/providers use", "/providers set", "/model "} {
		if strings.Contains(help, gone) {
			t.Errorf("help should not list retired subcommand %q\n%s", gone, help)
		}
	}
}

func TestProvidersOpensDialog(t *testing.T) {
	t.Parallel()
	deps, _, _ := testDeps(t, nil)
	p := slash.NewProcessor()

	result := p.Process(context.Background(), "/providers", deps)
	if !result.Handled {
		t.Fatal("expected handled")
	}
	if result.OpenDialog != slash.DialogProviders {
		t.Fatalf("OpenDialog = %q, want %q", result.OpenDialog, slash.DialogProviders)
	}
}

func TestModelsOpensDialog(t *testing.T) {
	t.Parallel()
	deps, _, _ := testDeps(t, nil)
	p := slash.NewProcessor()

	result := p.Process(context.Background(), "/models", deps)
	if !result.Handled {
		t.Fatal("expected handled")
	}
	if result.OpenDialog != slash.DialogModels {
		t.Fatalf("OpenDialog = %q, want %q", result.OpenDialog, slash.DialogModels)
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

func TestHelpCommandViaProcessor(t *testing.T) {
	t.Parallel()
	p := slash.NewProcessor()
	result := p.Process(context.Background(), "/help", slash.Deps{})
	if !result.Handled || result.Quit {
		t.Fatalf("result = %+v", result)
	}
	combined := strings.Join(result.Messages, "\n")
	for _, name := range []string{"/providers", "/models", "/memory", "/skills"} {
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
