package slash_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/agents"
	"github.com/undeadindustries/sagittarius/internal/bgproc"
	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/mcp"
	"github.com/undeadindustries/sagittarius/internal/modes"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/session"
	"github.com/undeadindustries/sagittarius/internal/skills"
	"github.com/undeadindustries/sagittarius/internal/slash"
	"github.com/undeadindustries/sagittarius/internal/tools"
)

type mockHooks struct {
	rebuildCalls  int
	reloadCalls   int
	models        []provider.ModelInfo
	storedKeys    map[string]string
	rebuildLabel  string
	rebuildModel  string
	workDir       string
	lastAssistant string
	lastUITheme   string
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

func (m *mockHooks) MCPToolInventory(context.Context) []mcp.ServerToolInventory {
	return []mcp.ServerToolInventory{
		{Server: "demo", Status: mcp.ServerConnected, Tools: []mcp.ToolInfo{
			{Name: "echo", WireName: "mcp_demo_echo", Description: "echo back", Enabled: true},
			{Name: "danger", WireName: "mcp_demo_danger", Description: "risky", Enabled: false},
		}},
	}
}

func (m *mockHooks) BuiltinTools() []tools.ToolEntry {
	return []tools.ToolEntry{
		{Name: "read_file", Description: "read a file", Source: tools.SourceBuiltin, ReadOnly: true},
	}
}

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

func (m *mockHooks) SelectCurrentModel(context.Context, string, string) (string, error) {
	return "gpt-4o-mini", nil
}

func (m *mockHooks) AllActiveModels() []provider.ProviderModelPair { return nil }

func (m *mockHooks) ProjectSystemPromptPresetID() string { return "" }

func (m *mockHooks) ApplyProjectSystemPromptPreset(ctx context.Context, presetID string) (string, error) {
	_ = ctx
	_ = presetID
	m.reloadCalls++
	m.rebuildCalls++
	return "System prompt → Programmer", nil
}

func (m *mockHooks) WriteRequestDebug() (string, error) {
	return "/tmp/sagittarius-request-test.json", nil
}

func (m *mockHooks) CurrentHistory() ([]provider.Message, error) {
	return []provider.Message{
		{Role: provider.RoleUser, Parts: []provider.Part{{Text: "hi"}}},
		{Role: provider.RoleModel, Parts: []provider.Part{{Text: "hello"}}},
	}, nil
}

func (m *mockHooks) WorkDir() string { return m.workDir }

func (m *mockHooks) SaveCheckpoint(tag string, _ bool) (string, error) {
	return "/tmp/checkpoint-" + tag + ".jsonl", nil
}

func (m *mockHooks) ListCheckpoints() ([]string, error) {
	return []string{"alpha", "beta"}, nil
}

func (m *mockHooks) ResumeCheckpoint(_ context.Context, tag string) (string, []provider.Message, error) {
	return "Resumed " + tag, []provider.Message{
		{Role: provider.RoleUser, Parts: []provider.Part{{Text: "hi"}}},
		{Role: provider.RoleModel, Parts: []provider.Part{{Text: "hello"}}},
	}, nil
}

func (m *mockHooks) DeleteCheckpoint(string) error { return nil }

func (m *mockHooks) ForceCompressHistory(context.Context) (string, error) {
	return "Compressed context: 100 → 20 tokens.", nil
}

func (m *mockHooks) LastAssistantText() string { return m.lastAssistant }

func (m *mockHooks) SessionStatsText(section string) string { return "stats[" + section + "]" }

func (m *mockHooks) SetUITheme(name string) error {
	m.lastUITheme = name
	return nil
}

func (m *mockHooks) ListBackgroundProcesses() []bgproc.Process { return nil }

func (m *mockHooks) KillBackgroundProcess(pid int) error { return nil }

func (m *mockHooks) BackgroundProcessOutput(pid int) string { return "" }

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
	hooks := &mockHooks{rebuildLabel: "OpenAI", rebuildModel: "gpt-4o-mini", lastAssistant: "assistant reply"}
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
	// The provider subcommand tree was retired (menu-first commands).
	// /model is now a real top-level command (global model picker), not retired.
	for _, gone := range []string{"/providers list", "/providers use", "/providers set"} {
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

func TestMCPOpensDialog(t *testing.T) {
	t.Parallel()
	deps, _, _ := testDeps(t, nil)
	p := slash.NewProcessor()

	result := p.Process(context.Background(), "/mcp", deps)
	if !result.Handled {
		t.Fatal("expected handled")
	}
	if result.OpenDialog != slash.DialogMCP {
		t.Fatalf("OpenDialog = %q, want %q", result.OpenDialog, slash.DialogMCP)
	}
}

func TestToolsOpensDialog(t *testing.T) {
	t.Parallel()
	deps, _, _ := testDeps(t, nil)
	p := slash.NewProcessor()

	result := p.Process(context.Background(), "/tools", deps)
	if !result.Handled {
		t.Fatal("expected handled")
	}
	if result.OpenDialog != slash.DialogTools {
		t.Fatalf("OpenDialog = %q, want %q", result.OpenDialog, slash.DialogTools)
	}
}

func TestToolsListAndDescOutput(t *testing.T) {
	t.Parallel()
	deps, _, _ := testDeps(t, nil)
	p := slash.NewProcessor()

	list := p.Process(context.Background(), "/tools list", deps)
	if !list.Handled || list.Err != nil {
		t.Fatalf("/tools list result = %+v", list)
	}
	joined := strings.Join(list.Messages, "\n")
	for _, want := range []string{"Built-in tools", "read_file", "demo", "echo [on]", "danger [off]"} {
		if !strings.Contains(joined, want) {
			t.Errorf("/tools list missing %q\n%s", want, joined)
		}
	}
	// list omits descriptions; desc includes them.
	if strings.Contains(joined, "echo back") {
		t.Errorf("/tools list should not include descriptions\n%s", joined)
	}

	desc := p.Process(context.Background(), "/tools desc", deps)
	joinedDesc := strings.Join(desc.Messages, "\n")
	if !strings.Contains(joinedDesc, "echo back") || !strings.Contains(joinedDesc, "read a file") {
		t.Errorf("/tools desc should include descriptions\n%s", joinedDesc)
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

func TestSystemPromptOpensDialog(t *testing.T) {
	t.Parallel()
	deps, _, _ := testDeps(t, nil)
	p := slash.NewProcessor()

	result := p.Process(context.Background(), "/system-prompt", deps)
	if !result.Handled {
		t.Fatal("expected handled")
	}
	if result.OpenDialog != slash.DialogSystemPrompt {
		t.Fatalf("OpenDialog = %q, want %q", result.OpenDialog, slash.DialogSystemPrompt)
	}
}

func TestSystemPromptAppliesPresetArg(t *testing.T) {
	t.Parallel()
	deps, _, hooks := testDeps(t, nil)
	p := slash.NewProcessor()

	result := p.Process(context.Background(), "/system-prompt programmer", deps)
	if !result.Handled || result.Err != nil {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Messages) == 0 {
		t.Fatal("expected info message")
	}
	if hooks.reloadCalls == 0 {
		t.Fatal("expected ReloadSystemInstruction after preset apply")
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

// TestReasoningNotApplicableOnGemini mutates the package-level provider session
// override global, so it is intentionally NOT parallel: it must not interleave
// with TestReasoningApplicableOnResponses, which sets and asserts the same global.
func TestReasoningNotApplicableOnGemini(t *testing.T) {
	provider.ClearSessionReasoningOverride()
	t.Cleanup(provider.ClearSessionReasoningOverride)

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

// TestReasoningApplicableOnResponses mutates the package-level provider session
// override global, so it is intentionally NOT parallel (see the sibling
// TestReasoningNotApplicableOnGemini): concurrent ClearSessionReasoningOverride
// calls would otherwise wipe the override this test sets and asserts.
func TestReasoningApplicableOnResponses(t *testing.T) {
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
