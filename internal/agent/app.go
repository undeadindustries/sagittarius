package agent

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/agents"
	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/credentials"
	"github.com/undeadindustries/sagittarius/internal/mcp"
	"github.com/undeadindustries/sagittarius/internal/modes"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/session"
	"github.com/undeadindustries/sagittarius/internal/skills"
	"github.com/undeadindustries/sagittarius/internal/slash"
	"github.com/undeadindustries/sagittarius/internal/tools"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

// AppConfig wires the interactive agent loop with slash command support.
type AppConfig struct {
	Runner        *Runner
	Runtime       *Runtime
	ProviderLabel string
	Model         string
	Loader        *config.Loader
	Settings      *config.Settings
	// SessionID keys the context manager's adaptive state and offload dirs. It
	// is reused across provider switches so offload paths stay stable.
	SessionID string
}

// App implements the optional ui.Completer interface for slash autocompletion.
var _ ui.Completer = (*App)(nil)

// App adapts Runner to ui.App for interactive TUI sessions.
type App struct {
	runner    *Runner
	runtime   *Runtime
	status    ui.StatusBar
	processor *slash.Processor
	deps      slash.Deps
	sessionID string
}

// NewApp wraps runner for interactive use and exposes footer metadata.
func NewApp(cfg AppConfig) *App {
	if cfg.ProviderLabel == "" {
		cfg.ProviderLabel = "ready"
	}
	if cfg.Model == "" && cfg.Runner != nil {
		cfg.Model = cfg.Runner.Model()
	}
	app := &App{
		runner:    cfg.Runner,
		runtime:   cfg.Runtime,
		processor: slash.NewProcessor(),
		sessionID: cfg.SessionID,
		status: ui.StatusBar{
			Left:   cfg.ProviderLabel,
			Right:  cfg.Model,
			Detail: modeStatusDetail(cfg.Runner),
		},
		deps: slash.Deps{
			Loader:   cfg.Loader,
			Settings: cfg.Settings,
			Hooks:    &appHooks{app: nil},
		},
	}
	app.deps.Hooks = &appHooks{app: app}
	return app
}

// HandleInput implements ui.App. Slash commands are handled locally; other
// input is delegated to the agent runner.
func (a *App) HandleInput(ctx context.Context, input string) (<-chan ui.StreamEvent, error) {
	if slash.IsSlashInput(input) {
		return a.handleSlash(ctx, input)
	}
	return a.runner.RunTurn(ctx, input)
}

// Status returns footer metadata for the TUI status bar.
func (a *App) Status() ui.StatusBar {
	return a.status
}

// Complete implements ui.Completer, providing slash-command, subcommand, and
// argument (e.g. provider id) completions for the interactive input line. It is
// read-only and non-blocking so the TUI can call it on every keystroke.
func (a *App) Complete(input string) ui.Completions {
	comp := a.processor.Registry().Complete(input, a.deps)
	items := make([]ui.Suggestion, 0, len(comp.Items))
	for _, s := range comp.Items {
		items = append(items, ui.Suggestion{
			Label:       s.Label,
			Description: s.Description,
			Insert:      s.Insert,
			AppendSpace: s.AppendSpace,
		})
	}
	return ui.Completions{Items: items, ReplaceFrom: comp.ReplaceFrom}
}

// CycleInteractionMode advances agent → plan → ask → debug → agent.
func (a *App) CycleInteractionMode(ctx context.Context) (<-chan ui.StreamEvent, error) {
	if a.runner == nil {
		return nil, fmt.Errorf("runner not available")
	}
	next := modes.CycleMode(a.runner.InteractionMode())
	return a.handleSlash(ctx, "/mode "+next.String())
}

func (a *App) handleSlash(ctx context.Context, input string) (<-chan ui.StreamEvent, error) {
	result := a.processor.Process(ctx, input, a.deps)
	out := make(chan ui.StreamEvent, 4)

	go func() {
		defer close(out)
		if result.Quit {
			out <- ui.StreamEvent{Type: ui.StreamQuit}
			out <- ui.StreamEvent{Type: ui.StreamDone}
			return
		}
		for _, msg := range result.Messages {
			out <- ui.StreamEvent{Type: ui.StreamInfo, Text: msg + "\n"}
		}
		if result.Err != nil {
			out <- ui.StreamEvent{Type: ui.StreamError, Err: result.Err}
		}
		if result.OpenDialog != "" {
			out <- ui.StreamEvent{Type: ui.StreamOpenDialog, Dialog: mapDialogKind(result.OpenDialog)}
		}
		out <- ui.StreamEvent{Type: ui.StreamDone}
	}()

	return out, nil
}

type appHooks struct {
	app *App
}

func (h *appHooks) RebuildRunner(ctx context.Context) (string, string, error) {
	if h.app == nil || h.app.runner == nil {
		return "", "", fmt.Errorf("runner not available")
	}
	if h.app.deps.Settings == nil {
		return "", "", fmt.Errorf("settings not loaded")
	}

	gen, err := provider.NewContentGenerator(ctx, h.app.deps.Settings)
	if err != nil {
		return "", "", err
	}
	endpoint, err := provider.ResolveEndpointConfig(h.app.deps.Settings)
	if err != nil {
		return "", "", err
	}

	h.app.runner.SetGenerator(gen)
	h.app.runner.SetProviderDefaultModel(endpoint.Model)
	if !h.app.runner.ModelPinned() {
		h.app.runner.RefreshModelFromMode()
	}

	resolvedModel := h.app.runner.Model()

	// Rebuild the context manager so local-context defenses track the new wire
	// format. NewContextManager returns nil off the openai-chat path, making
	// context management a pure pass-through for gemini-native / openai-responses.
	// Pass runner.Model (resolved per call) so chat compression/summarization
	// always runs against the model user turns use, including after a mid-session
	// /mode switch that does not rebuild the runner (AD-015 active-model rule).
	h.app.runner.SetContextManager(
		NewContextManager(h.app.deps.Settings, gen, h.app.runner.Model, h.app.sessionID),
	)

	label := endpoint.ProviderID
	if def, ok := config.LookupBuiltInProvider(endpoint.ProviderID); ok {
		label = def.DisplayName
	} else if h.app.deps.Settings.Providers != nil {
		if custom, ok := h.app.deps.Settings.Providers.Custom[endpoint.ProviderID]; ok && custom.DisplayName != "" {
			label = custom.DisplayName
		}
	}

	h.app.status = ui.StatusBar{
		Left:   label,
		Right:  resolvedModel,
		Detail: "mode: " + h.app.runner.InteractionMode().String(),
	}
	return label, resolvedModel, nil
}

func (h *appHooks) ReloadSystemInstruction(ctx context.Context) error {
	if h.app == nil || h.app.runner == nil {
		return fmt.Errorf("runner not available")
	}
	_ = ctx
	return h.app.runner.ReloadSystemInstruction()
}

func (h *appHooks) DiscoverModels(ctx context.Context) []provider.ModelInfo {
	if h.app == nil || h.app.deps.Settings == nil {
		return nil
	}
	endpoint, err := provider.ResolveEndpointConfig(h.app.deps.Settings)
	if err != nil || endpoint.BaseURL == "" {
		return nil
	}
	bearer := endpoint.Bearer
	if bearer == "" && endpoint.RequiresAPIKey {
		key, err := credentials.ResolveProviderAPIKey(ctx, endpoint.ProviderID)
		if err == nil {
			bearer = key
		}
	}
	return provider.DiscoverModels(ctx, endpoint.BaseURL, bearer, nil)
}

func (h *appHooks) SetProviderAPIKey(ctx context.Context, providerID, apiKey string) error {
	return credentials.SetProviderAPIKey(ctx, providerID, apiKey)
}

func (h *appHooks) ReloadMCP(ctx context.Context) (string, error) {
	if h.app == nil || h.app.runtime == nil || h.app.runner == nil {
		return "", fmt.Errorf("runtime not available")
	}
	reg, err := h.app.runtime.ReloadTools(ctx)
	if err != nil {
		return "", err
	}
	h.app.runner.SetRegistry(reg)
	return formatMCPReloadSummary(h.app.runtime.Catalog.MCPManager().States()), nil
}

func (h *appHooks) ReloadSkills(ctx context.Context) (string, error) {
	if h.app == nil || h.app.runtime == nil || h.app.runner == nil {
		return "", fmt.Errorf("runtime not available")
	}
	before := skillNames(h.app.runtime.Catalog.SkillManager().Skills())
	reg, err := h.app.runtime.ReloadSkills(ctx)
	if err != nil {
		return "", err
	}
	h.app.runner.SetRegistry(reg)
	after := skillNames(h.app.runtime.Catalog.SkillManager().Skills())
	return formatSkillsReloadSummary(before, after), nil
}

func (h *appHooks) ReloadAgents(ctx context.Context) (agents.ReloadSummary, error) {
	if h.app == nil || h.app.runtime == nil {
		return agents.ReloadSummary{}, fmt.Errorf("runtime not available")
	}
	return h.app.runtime.ReloadAgents(ctx)
}

func (h *appHooks) MCPStates() []mcp.ServerState {
	if h.app == nil || h.app.runtime == nil {
		return nil
	}
	return h.app.runtime.Catalog.MCPManager().States()
}

func (h *appHooks) SkillList() []skills.Definition {
	if h.app == nil || h.app.runtime == nil {
		return nil
	}
	return h.app.runtime.Catalog.SkillManager().Skills()
}

func (h *appHooks) AgentList() []agents.Definition {
	if h.app == nil || h.app.runtime == nil {
		return nil
	}
	return h.app.runtime.Agents.AllDefinitions()
}

// ListSessions lists sessions for the current working directory.
func (h *appHooks) ListSessions() ([]session.SessionInfo, error) {
	projectRoot, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("resolve working directory: %w", err)
	}
	chatsDir, err := session.ChatsDir(projectRoot)
	if err != nil {
		return nil, err
	}
	currentID := ""
	if h.app != nil {
		currentID = h.app.sessionID
	}
	return session.ListSessions(chatsDir, currentID)
}

// ClearHistory wipes the in-memory conversation history of the runner and
// rotates the session recorder so subsequent turns are written to a new session
// file rather than appended to the cleared conversation.
func (h *appHooks) ClearHistory() error {
	if h.app == nil || h.app.runner == nil {
		return fmt.Errorf("runner not available")
	}
	h.app.runner.ClearHistory()
	h.app.runner.RotateSession()
	return nil
}

func (h *appHooks) SetInteractionMode(_ context.Context, mode modes.Mode) (string, error) {
	if h.app == nil || h.app.runner == nil {
		return "", fmt.Errorf("runner not available")
	}
	model := h.app.runner.SetInteractionMode(mode)
	h.app.status.Right = model
	h.app.status.Detail = "mode: " + mode.String()
	return model, nil
}

func (h *appHooks) InteractionMode() (modes.Mode, string) {
	if h.app == nil || h.app.runner == nil {
		return modes.ModeAgent, ""
	}
	mode := h.app.runner.InteractionMode()
	return mode, h.app.runner.Model()
}

func modeStatusDetail(runner *Runner) string {
	if runner == nil {
		return "mode: agent"
	}
	return "mode: " + runner.InteractionMode().String()
}

func skillNames(items []skills.Definition) map[string]struct{} {
	out := make(map[string]struct{}, len(items))
	for _, item := range items {
		out[item.Name] = struct{}{}
	}
	return out
}

func formatSkillsReloadSummary(before, after map[string]struct{}) string {
	added := 0
	removed := 0
	for name := range after {
		if _, ok := before[name]; !ok {
			added++
		}
	}
	for name := range before {
		if _, ok := after[name]; !ok {
			removed++
		}
	}
	msg := "Agent skills reloaded successfully."
	if added > 0 || removed > 0 {
		msg += fmt.Sprintf(" (%d added, %d removed)", added, removed)
	}
	return msg
}

func formatMCPReloadSummary(states []mcp.ServerState) string {
	if len(states) == 0 {
		return "MCP servers reloaded. No servers configured."
	}
	var lines []string
	lines = append(lines, "MCP servers reloaded:")
	for _, st := range states {
		line := fmt.Sprintf("  %s: %s (%d tools)", st.Name, st.Status, st.ToolCount)
		if st.LastError != "" {
			line += " — " + st.LastError
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// SetGenerator replaces the content generator (used after provider changes)
// and clears any recorded provider-unavailable error.
func (r *Runner) SetGenerator(gen provider.ContentGenerator) {
	r.genMu.Lock()
	r.gen = gen
	r.genErr = nil
	r.genMu.Unlock()
}

// SetModel updates the model used for generate requests directly.
func (r *Runner) SetModel(model string) {
	model = strings.TrimSpace(model)
	if model == "" {
		return
	}
	r.modelMu.Lock()
	r.model = model
	r.modelMu.Unlock()
}

// PinModel locks the runner to an explicit model, bypassing mode routing.
func (r *Runner) PinModel(model string) {
	model = strings.TrimSpace(model)
	if model == "" {
		return
	}
	r.modelPinned = true
	r.SetModel(model)
}

// SetRegistry replaces the tool registry and rebuilds the scheduler. Safe to
// call from a slash-command handler while a turn streams on another goroutine.
func (r *Runner) SetRegistry(registry *tools.Registry) {
	if registry == nil {
		return
	}
	scheduler := tools.NewScheduler(registry, approvalToPolicy(r.approval), r.interactive)
	r.regMu.Lock()
	r.registry = registry
	r.scheduler = scheduler
	r.regMu.Unlock()
}

// Registry returns the active tool registry.
func (r *Runner) Registry() *tools.Registry {
	r.regMu.RLock()
	defer r.regMu.RUnlock()
	return r.registry
}

// ReloadSystemInstruction re-reads GEMINI.md / AGENTS.md into the system prompt.
func (r *Runner) ReloadSystemInstruction() error {
	system, err := DiscoverSystemInstruction(r.workDir)
	if err != nil {
		return err
	}
	r.systemBase = system
	r.applyModeSystemSuffix()
	return nil
}
