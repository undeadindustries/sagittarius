package slash_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/agents"
	"github.com/undeadindustries/sagittarius/internal/bgproc"
	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/goal"
	"github.com/undeadindustries/sagittarius/internal/grill"
	"github.com/undeadindustries/sagittarius/internal/mcp"
	"github.com/undeadindustries/sagittarius/internal/modes"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/selfupdate"
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
	renamedTitle  string

	// interactionMode backs InteractionMode(); defaults to modes.ModeAgent
	// (the zero value) so most tests need not set it explicitly.
	interactionMode modes.Mode
	// interactionModel overrides InteractionMode()'s model; empty defaults to
	// "gpt-4o-mini" (the historical mock default).
	interactionModel string
	// grillSession backs GrillStatus()/SetGrill()/... for a stateful mock,
	// so /grill command tests can assert real start/pause/resume/done/clear
	// transitions rather than fixed stubs.
	grillSession *grill.Session
	// reasoningOverride backs ReasoningOverride()/SetReasoningOverride()/
	// ClearReasoningOverride(), mirroring Runner's ephemeral /reasoning pin.
	reasoningOverride string
	// memoryEntries backs AddMemory()/ListMemories()/RemoveMemory() with a
	// stateful in-memory slice, so /memory command tests can assert real
	// add/list/remove behavior.
	memoryEntries []slash.MemoryEntry
	// constraints backs AddConstraint()/ListConstraints()/ClearConstraints()
	// with a stateful in-memory slice, so /constraints command tests can
	// assert real add/list/clear behavior.
	constraints []string
	// setModeCalls records every mode passed to SetInteractionMode, so tests
	// can assert the top-level /agent, /plan, /ask, /debug shortcuts invoke
	// the same hook as their "/mode <name>" equivalents.
	setModeCalls []modes.Mode
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

func (m *mockHooks) AddMemory(_ context.Context, text string, scope config.SettingScope) (string, error) {
	path := "/tmp/AGENTS.md"
	if scope == config.ScopeProject {
		path = filepath.Join(m.workDir, "AGENTS.md")
	}
	m.memoryEntries = append(m.memoryEntries, slash.MemoryEntry{Scope: scope, Path: path, Text: text})
	m.reloadCalls++ // mirrors appHooks.AddMemory reloading the system instruction
	return path, nil
}

func (m *mockHooks) ListMemories() ([]slash.MemoryEntry, error) {
	return m.memoryEntries, nil
}

func (m *mockHooks) RemoveMemory(_ context.Context, index int) (string, error) {
	if index < 1 || index > len(m.memoryEntries) {
		return "", fmt.Errorf("memory index %d out of range (1-%d)", index, len(m.memoryEntries))
	}
	removed := m.memoryEntries[index-1].Text
	m.memoryEntries = append(m.memoryEntries[:index-1:index-1], m.memoryEntries[index:]...)
	m.reloadCalls++ // mirrors appHooks.RemoveMemory reloading the system instruction
	return removed, nil
}

func (m *mockHooks) AddConstraint(text string) error {
	for _, existing := range m.constraints {
		if strings.EqualFold(existing, text) {
			return nil
		}
	}
	m.constraints = append(m.constraints, text)
	return nil
}

func (m *mockHooks) ListConstraints() []string {
	return m.constraints
}

func (m *mockHooks) ClearConstraints() error {
	m.constraints = nil
	return nil
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

func (m *mockHooks) SetModeOverride(_ context.Context, _, _, _ string, _ config.SettingScope) error {
	return nil
}
func (m *mockHooks) SetInteractionMode(_ context.Context, mode modes.Mode) (string, error) {
	m.setModeCalls = append(m.setModeCalls, mode)
	return "gpt-4o-mini", nil
}

func (m *mockHooks) InteractionMode() (modes.Mode, string) {
	if m.interactionModel != "" {
		return m.interactionMode, m.interactionModel
	}
	return m.interactionMode, "gpt-4o-mini"
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

func (m *mockHooks) RenameSession(title string) error {
	m.renamedTitle = title
	return nil
}

func (m *mockHooks) ForkSession() (string, string, error) {
	return "forked-session-id", "/tmp/forked-session.jsonl", nil
}

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

func (m *mockHooks) GoalStatus() *goal.Goal                           { return nil }
func (m *mockHooks) SetGoal(objective string, tokenBudget *int) error { return nil }
func (m *mockHooks) PauseGoal(note string) error                      { return nil }
func (m *mockHooks) ResumeGoal(note string) error                     { return nil }
func (m *mockHooks) CompleteGoal(note string) error                   { return nil }
func (m *mockHooks) BlockGoal(note string) error                      { return nil }
func (m *mockHooks) ClearGoal(note string) error                      { return nil }
func (m *mockHooks) SetGoalBudget(tokens int) error                   { return nil }

func (m *mockHooks) GrillStatus() *grill.Session { return m.grillSession }

func (m *mockHooks) SetGrill(topic string) error {
	m.grillSession = &grill.Session{Topic: topic, Status: grill.StatusActive}
	return nil
}

func (m *mockHooks) PauseGrill(note string) error {
	if m.grillSession == nil {
		return fmt.Errorf("no active grill session")
	}
	m.grillSession.Status = grill.StatusPaused
	m.grillSession.Note = note
	return nil
}

func (m *mockHooks) ResumeGrill(note string) error {
	if m.grillSession == nil {
		return fmt.Errorf("no active grill session")
	}
	m.grillSession.Status = grill.StatusActive
	m.grillSession.Note = note
	return nil
}

func (m *mockHooks) EndGrill(note string) (string, error) {
	if m.grillSession == nil {
		return "", fmt.Errorf("no active grill session")
	}
	m.grillSession.Status = grill.StatusSummarizing
	m.grillSession.Note = note
	return grill.SpecPrompt(m.grillSession.Topic, m.grillSession.Decisions, "docs/specs/"+grill.SlugTopic(m.grillSession.Topic)+".md"), nil
}

func (m *mockHooks) ClearGrill(note string) error {
	m.grillSession = nil
	return nil
}

func (m *mockHooks) ToolkitReport() string {
	return "mock toolkit report"
}

func (m *mockHooks) ToolkitDismiss() error {
	return nil
}

func (m *mockHooks) ReasoningOverride() string {
	return m.reasoningOverride
}

func (m *mockHooks) SetReasoningOverride(effort string) {
	m.reasoningOverride = effort
}

func (m *mockHooks) ClearReasoningOverride() {
	m.reasoningOverride = ""
}

func (m *mockHooks) CheckForUpdate(ctx context.Context, force bool) (*selfupdate.CheckResult, error) {
	return nil, nil
}

func (m *mockHooks) InstallUpdate(ctx context.Context) (*selfupdate.InstallResult, error) {
	return &selfupdate.InstallResult{Version: "v1.0.0"}, nil
}

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

// TestTopLevelModeShortcuts asserts /agent, /plan, /ask, /debug are direct,
// argument-free shortcuts for "/mode <name>": same hook call, same success
// message, no subcommand parsing.
func TestTopLevelModeShortcuts(t *testing.T) {
	t.Parallel()
	cases := []struct {
		cmd  string
		mode modes.Mode
	}{
		{"/agent", modes.ModeAgent},
		{"/plan", modes.ModePlan},
		{"/ask", modes.ModeAsk},
		{"/debug", modes.ModeDebug},
	}
	for _, tc := range cases {
		t.Run(tc.cmd, func(t *testing.T) {
			deps, _, hooks := testDeps(t, nil)
			p := slash.NewProcessor()

			shortcutResult := p.Process(context.Background(), tc.cmd, deps)
			if !shortcutResult.Handled {
				t.Fatalf("%s: expected handled", tc.cmd)
			}
			if len(hooks.setModeCalls) != 1 || hooks.setModeCalls[0] != tc.mode {
				t.Fatalf("%s: setModeCalls = %v, want [%v]", tc.cmd, hooks.setModeCalls, tc.mode)
			}

			longFormDeps, _, longFormHooks := testDeps(t, nil)
			longFormResult := p.Process(context.Background(), "/mode "+tc.mode.String(), longFormDeps)
			if len(shortcutResult.Messages) != len(longFormResult.Messages) {
				t.Fatalf("%s: messages = %v, want to match /mode long form %v", tc.cmd, shortcutResult.Messages, longFormResult.Messages)
			}
			for i := range shortcutResult.Messages {
				if shortcutResult.Messages[i] != longFormResult.Messages[i] {
					t.Fatalf("%s message = %q, want %q (parity with /mode long form)", tc.cmd, shortcutResult.Messages[i], longFormResult.Messages[i])
				}
			}
			if len(longFormHooks.setModeCalls) != 1 || longFormHooks.setModeCalls[0] != tc.mode {
				t.Fatalf("/mode %s: setModeCalls = %v, want [%v]", tc.mode.String(), longFormHooks.setModeCalls, tc.mode)
			}
		})
	}
}

// TestTopLevelModeShortcutsRejectArgs asserts /agent, /plan, /ask, /debug
// reject trailing arguments with a usage message instead of silently
// switching mode and dropping the extra token (e.g. "/agent reload" must not
// switch to agent mode).
func TestTopLevelModeShortcutsRejectArgs(t *testing.T) {
	t.Parallel()
	cases := []string{"/agent", "/plan", "/ask", "/debug"}
	for _, cmd := range cases {
		t.Run(cmd, func(t *testing.T) {
			deps, _, hooks := testDeps(t, nil)
			p := slash.NewProcessor()

			result := p.Process(context.Background(), cmd+" extra", deps)
			if !result.Handled {
				t.Fatalf("%s extra: expected handled", cmd)
			}
			if len(hooks.setModeCalls) != 0 {
				t.Fatalf("%s extra: setModeCalls = %v, want none (mode must not switch)", cmd, hooks.setModeCalls)
			}
			wantMsg := "Usage: " + cmd
			if len(result.Messages) != 1 || result.Messages[0] != wantMsg {
				t.Fatalf("%s extra: messages = %v, want [%q]", cmd, result.Messages, wantMsg)
			}
		})
	}
}

func TestModesOpensDialog(t *testing.T) {
	t.Parallel()
	deps, _, _ := testDeps(t, nil)
	p := slash.NewProcessor()

	result := p.Process(context.Background(), "/modes", deps)
	if !result.Handled {
		t.Fatal("expected handled")
	}
	if result.OpenDialog != slash.DialogModes {
		t.Fatalf("OpenDialog = %q, want %q", result.OpenDialog, slash.DialogModes)
	}
}

func TestModesOverrideIncompleteOpensDialog(t *testing.T) {
	t.Parallel()
	deps, _, _ := testDeps(t, nil)
	p := slash.NewProcessor()

	result := p.Process(context.Background(), "/modes override", deps)
	if !result.Handled {
		t.Fatal("expected handled")
	}
	if result.OpenDialog != slash.DialogModes {
		t.Fatalf("OpenDialog = %q, want %q", result.OpenDialog, slash.DialogModes)
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

func TestMemoryAddDefaultsToGlobalScope(t *testing.T) {
	t.Parallel()
	deps, _, hooks := testDeps(t, nil)
	p := slash.NewProcessor()

	result := p.Process(context.Background(), "/memory add prefers pnpm over npm", deps)

	if result.Err != nil {
		t.Fatalf("add error: %v", result.Err)
	}
	if len(hooks.memoryEntries) != 1 {
		t.Fatalf("memoryEntries = %#v, want 1 entry", hooks.memoryEntries)
	}
	entry := hooks.memoryEntries[0]
	if entry.Scope != config.ScopeGlobal {
		t.Errorf("scope = %v, want ScopeGlobal", entry.Scope)
	}
	if entry.Text != "prefers pnpm over npm" {
		t.Errorf("text = %q, want %q", entry.Text, "prefers pnpm over npm")
	}
	if hooks.reloadCalls != 1 {
		t.Errorf("reloadCalls = %d, want 1 (memory add should reload the system instruction)", hooks.reloadCalls)
	}
	if len(result.Messages) != 1 || !strings.Contains(result.Messages[0], "Added to") {
		t.Errorf("messages = %#v, want a confirmation naming the file", result.Messages)
	}
}

func TestMemoryAddProjectFlag(t *testing.T) {
	t.Parallel()
	deps, _, hooks := testDeps(t, nil)
	hooks.workDir = "/repo"
	p := slash.NewProcessor()

	result := p.Process(context.Background(), "/memory add --project CI takes 40 minutes", deps)

	if result.Err != nil {
		t.Fatalf("add error: %v", result.Err)
	}
	if len(hooks.memoryEntries) != 1 {
		t.Fatalf("memoryEntries = %#v, want 1 entry", hooks.memoryEntries)
	}
	entry := hooks.memoryEntries[0]
	if entry.Scope != config.ScopeProject {
		t.Errorf("scope = %v, want ScopeProject", entry.Scope)
	}
	if entry.Text != "CI takes 40 minutes" {
		t.Errorf("text = %q, want %q", entry.Text, "CI takes 40 minutes")
	}
}

func TestMemoryAddRejectsEmptyText(t *testing.T) {
	t.Parallel()
	deps, _, hooks := testDeps(t, nil)
	p := slash.NewProcessor()

	result := p.Process(context.Background(), "/memory add", deps)

	if result.Err != nil {
		t.Fatalf("expected a usage message, not an error result: %v", result.Err)
	}
	if len(hooks.memoryEntries) != 0 {
		t.Fatalf("expected no entry to be added, got %#v", hooks.memoryEntries)
	}
	if len(result.Messages) != 1 || !strings.Contains(result.Messages[0], "Usage:") {
		t.Errorf("messages = %#v, want a usage hint", result.Messages)
	}
}

func TestMemoryAddRejectsWhitespaceOnlyAfterProjectFlag(t *testing.T) {
	t.Parallel()
	deps, _, hooks := testDeps(t, nil)
	p := slash.NewProcessor()

	result := p.Process(context.Background(), "/memory add --project", deps)

	if result.Err != nil {
		t.Fatalf("expected a usage message, not an error result: %v", result.Err)
	}
	if len(hooks.memoryEntries) != 0 {
		t.Fatalf("expected no entry to be added, got %#v", hooks.memoryEntries)
	}
}

func TestMemoryListNumbersGlobalFirst(t *testing.T) {
	t.Parallel()
	deps, _, hooks := testDeps(t, nil)
	hooks.memoryEntries = []slash.MemoryEntry{
		{Scope: config.ScopeGlobal, Path: "/home/.sagittarius/AGENTS.md", Text: "global fact"},
		{Scope: config.ScopeProject, Path: "/repo/AGENTS.md", Text: "project fact"},
	}
	p := slash.NewProcessor()

	result := p.Process(context.Background(), "/memory list", deps)

	if result.Err != nil {
		t.Fatalf("list error: %v", result.Err)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("messages = %#v, want exactly one", result.Messages)
	}
	got := result.Messages[0]
	if !strings.Contains(got, "1. [global] global fact") {
		t.Errorf("expected numbered global entry, got:\n%s", got)
	}
	if !strings.Contains(got, "2. [project] project fact") {
		t.Errorf("expected numbered project entry, got:\n%s", got)
	}
}

func TestMemoryListEmpty(t *testing.T) {
	t.Parallel()
	deps, _, _ := testDeps(t, nil)
	p := slash.NewProcessor()

	result := p.Process(context.Background(), "/memory list", deps)

	if result.Err != nil {
		t.Fatalf("list error: %v", result.Err)
	}
	if len(result.Messages) != 1 || !strings.Contains(result.Messages[0], "No memory entries") {
		t.Errorf("messages = %#v, want a no-entries message", result.Messages)
	}
}

func TestMemoryRemoveByIndex(t *testing.T) {
	t.Parallel()
	deps, _, hooks := testDeps(t, nil)
	hooks.memoryEntries = []slash.MemoryEntry{
		{Scope: config.ScopeGlobal, Text: "keep me"},
		{Scope: config.ScopeGlobal, Text: "remove me"},
	}
	p := slash.NewProcessor()

	result := p.Process(context.Background(), "/memory remove 2", deps)

	if result.Err != nil {
		t.Fatalf("remove error: %v", result.Err)
	}
	if len(hooks.memoryEntries) != 1 || hooks.memoryEntries[0].Text != "keep me" {
		t.Fatalf("memoryEntries = %#v, want only %q left", hooks.memoryEntries, "keep me")
	}
	if hooks.reloadCalls != 1 {
		t.Errorf("reloadCalls = %d, want 1 (memory remove should reload the system instruction)", hooks.reloadCalls)
	}
	if len(result.Messages) != 1 || !strings.Contains(result.Messages[0], "remove me") {
		t.Errorf("messages = %#v, want the removed text echoed back", result.Messages)
	}
}

func TestMemoryRemoveOutOfRange(t *testing.T) {
	t.Parallel()
	deps, _, hooks := testDeps(t, nil)
	hooks.memoryEntries = []slash.MemoryEntry{{Scope: config.ScopeGlobal, Text: "only entry"}}
	p := slash.NewProcessor()

	result := p.Process(context.Background(), "/memory remove 5", deps)

	if result.Err == nil {
		t.Fatal("expected an error for an out-of-range index")
	}
	if len(hooks.memoryEntries) != 1 {
		t.Fatalf("expected no mutation on error, got %#v", hooks.memoryEntries)
	}
}

func TestMemoryRemoveNonNumericArgument(t *testing.T) {
	t.Parallel()
	deps, _, hooks := testDeps(t, nil)
	hooks.memoryEntries = []slash.MemoryEntry{{Scope: config.ScopeGlobal, Text: "only entry"}}
	p := slash.NewProcessor()

	result := p.Process(context.Background(), "/memory remove abc", deps)

	if result.Err != nil {
		t.Fatalf("expected a usage message, not an error result: %v", result.Err)
	}
	if len(result.Messages) != 1 || !strings.Contains(result.Messages[0], "Usage:") {
		t.Errorf("messages = %#v, want a usage hint", result.Messages)
	}
	if len(hooks.memoryEntries) != 1 {
		t.Fatalf("expected no mutation, got %#v", hooks.memoryEntries)
	}
}

func TestConstraintsAddRejectsEmptyText(t *testing.T) {
	t.Parallel()
	deps, _, hooks := testDeps(t, nil)
	p := slash.NewProcessor()

	result := p.Process(context.Background(), "/constraints add", deps)

	if result.Err != nil {
		t.Fatalf("expected a usage message, not an error result: %v", result.Err)
	}
	if len(hooks.constraints) != 0 {
		t.Fatalf("expected no constraint to be added, got %#v", hooks.constraints)
	}
	if len(result.Messages) != 1 || !strings.Contains(result.Messages[0], "Usage:") {
		t.Errorf("messages = %#v, want a usage hint", result.Messages)
	}
}

func TestConstraintsAddAndList(t *testing.T) {
	t.Parallel()
	deps, _, hooks := testDeps(t, nil)
	p := slash.NewProcessor()

	addResult := p.Process(context.Background(), "/constraints add do not touch AGENTS.md yet", deps)
	if addResult.Err != nil {
		t.Fatalf("add error: %v", addResult.Err)
	}
	if len(hooks.constraints) != 1 || hooks.constraints[0] != "do not touch AGENTS.md yet" {
		t.Fatalf("constraints = %#v, want 1 entry", hooks.constraints)
	}

	listResult := p.Process(context.Background(), "/constraints list", deps)
	if listResult.Err != nil {
		t.Fatalf("list error: %v", listResult.Err)
	}
	if len(listResult.Messages) != 1 || !strings.Contains(listResult.Messages[0], "1. do not touch AGENTS.md yet") {
		t.Errorf("messages = %#v, want a numbered constraint", listResult.Messages)
	}
}

func TestConstraintsListEmpty(t *testing.T) {
	t.Parallel()
	deps, _, _ := testDeps(t, nil)
	p := slash.NewProcessor()

	result := p.Process(context.Background(), "/constraints list", deps)

	if result.Err != nil {
		t.Fatalf("list error: %v", result.Err)
	}
	if len(result.Messages) != 1 || !strings.Contains(result.Messages[0], "No standing constraints") {
		t.Errorf("messages = %#v, want a no-constraints message", result.Messages)
	}
}

func TestConstraintsClear(t *testing.T) {
	t.Parallel()
	deps, _, hooks := testDeps(t, nil)
	hooks.constraints = []string{"do not touch AGENTS.md yet"}
	p := slash.NewProcessor()

	result := p.Process(context.Background(), "/constraints clear", deps)

	if result.Err != nil {
		t.Fatalf("clear error: %v", result.Err)
	}
	if len(hooks.constraints) != 0 {
		t.Fatalf("expected constraints cleared, got %#v", hooks.constraints)
	}
	if len(result.Messages) != 1 || !strings.Contains(result.Messages[0], "Cleared") {
		t.Errorf("messages = %#v, want a cleared confirmation", result.Messages)
	}
}

// TestReasoningShowOnGeminiUnmatchedModel verifies /reasoning show degrades
// gracefully (no session override, no pin, no matched family rule) for a
// Gemini model that isn't a gemini-3/2.5 dynamic-thinking family member — the
// mock's fixed InteractionMode model ("gpt-4o-mini") never matches the Gemini
// wire-format rules, exercising the "no known reasoning capability" path.
func TestReasoningShowOnGeminiUnmatchedModel(t *testing.T) {
	t.Parallel()

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
	if !strings.Contains(combined, "off") {
		t.Fatalf("expected an 'off' resolution for an unmatched model, got: %q", combined)
	}
}

// TestReasoningApplicableOnResponses verifies /reasoning show reports a
// pinned providers.<id>.reasoningEffort and that /reasoning <level> sets the
// mock's ephemeral override (the real override lives on Runner; see
// runner_reasoning_test.go for that).
func TestReasoningApplicableOnResponses(t *testing.T) {
	t.Parallel()

	settings := &config.Settings{
		Providers: &config.ProvidersSettings{
			Active:          string(config.BuiltInOpenAIResponses),
			OpenAIResponses: &config.ProviderInstanceConfig{ReasoningEffort: "low"},
		},
		Raw: map[string]json.RawMessage{},
	}
	deps, _, hooks := testDeps(t, settings)
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
	if hooks.reasoningOverride != "high" {
		t.Fatalf("override = %q, want high", hooks.reasoningOverride)
	}
}

// TestReasoningMandatoryRejectsDisable verifies a mandatory family (gpt-5-pro)
// rejects an attempt to disable reasoning via /reasoning none.
func TestReasoningMandatoryRejectsDisable(t *testing.T) {
	t.Parallel()

	settings := &config.Settings{
		Providers: &config.ProvidersSettings{
			Active:          string(config.BuiltInOpenAIResponses),
			OpenAIResponses: &config.ProviderInstanceConfig{Model: "gpt-5-pro"},
		},
		Raw: map[string]json.RawMessage{},
	}
	deps, _, hooks := testDeps(t, settings)
	hooks.interactionModel = "gpt-5-pro"
	p := slash.NewProcessor()

	result := p.Process(context.Background(), "/reasoning none", deps)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	combined := strings.Join(result.Messages, "\n")
	if !strings.Contains(combined, "mandatory") {
		t.Fatalf("expected mandatory rejection message, got: %q", combined)
	}
	if hooks.reasoningOverride != "" {
		t.Fatalf("override should not have been set, got: %q", hooks.reasoningOverride)
	}
}

func TestUpdateCommand(t *testing.T) {
	deps, _, _ := testDeps(t, nil)
	p := slash.NewProcessor()

	// bare /update
	res := p.Process(context.Background(), "/update", deps)
	if res.Err != nil {
		t.Fatalf("expected no error for bare /update, got: %v", res.Err)
	}

	// /update install
	res = p.Process(context.Background(), "/update install", deps)
	if res.Err != nil {
		t.Fatalf("expected no error for /update install, got: %v", res.Err)
	}

	// /update unknown
	res = p.Process(context.Background(), "/update unknown", deps)
	if res.Err == nil {
		t.Fatalf("expected error for /update unknown, got nil")
	}
}
