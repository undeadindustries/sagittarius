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
	// BaseProviderID is the canonical provider id that was active at startup
	// before any mode-driven provider override. buildRunner resolves this and
	// passes it here; SetInteractionMode uses it to return to the right base
	// when leaving a mode whose override is empty.
	BaseProviderID string
}

// App implements the optional ui.Completer and ui.MetricsProvider interfaces.
var (
	_ ui.Completer       = (*App)(nil)
	_ ui.MetricsProvider = (*App)(nil)
)

// App adapts Runner to ui.App for interactive TUI sessions.
type App struct {
	runner    *Runner
	runtime   *Runtime
	status    ui.StatusBar
	processor *slash.Processor
	deps      slash.Deps
	sessionID string
	// baseProviderID records the canonical provider id that was active before a
	// mode override temporarily switched to a different provider. Empty when no
	// provider override is active.
	baseProviderID string
}

// NewApp wraps runner for interactive use and exposes footer metadata.
func NewApp(cfg AppConfig) *App {
	if cfg.ProviderLabel == "" {
		cfg.ProviderLabel = "ready"
	}
	if cfg.Model == "" && cfg.Runner != nil {
		cfg.Model = cfg.Runner.Model()
	}
	mode := modes.ModeAgent
	if cfg.Runner != nil {
		mode = cfg.Runner.InteractionMode()
	}
	app := &App{
		runner:         cfg.Runner,
		runtime:        cfg.Runtime,
		processor:      slash.NewProcessor(),
		sessionID:      cfg.SessionID,
		baseProviderID: config.NormalizeProviderID(cfg.BaseProviderID),
		status: ui.StatusBar{
			Left:   cfg.ProviderLabel,
			Right:  cfg.Model,
			Detail: systemPromptStatusDetail(cfg.Runner, cfg.Settings),
			Mode:   mode.String(),
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

// SessionMetrics implements ui.MetricsProvider, exposing live session telemetry
// for the footer and exit summary. Provider/session identifiers come from the
// app; counts and token estimates come from the runner.
func (a *App) SessionMetrics() ui.SessionStats {
	if a.runner == nil {
		return ui.SessionStats{SessionID: a.sessionID, Provider: a.status.Left}
	}
	stats := a.runner.Stats()
	stats.SessionID = a.sessionID
	stats.Provider = a.status.Left
	return stats
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

// CycleModel advances the global curated model list circularly across all
// providers, updates the active provider+model, and rebuilds the runner.
// It emits an info message showing the resolved live model.
func (a *App) CycleModel(ctx context.Context) (<-chan ui.StreamEvent, error) {
	if a.runner == nil || a.deps.Settings == nil || a.deps.Loader == nil {
		return nil, fmt.Errorf("runner not available")
	}
	s := a.deps.Settings

	// Gather every (provider, model) pair across all providers.
	pairs := provider.AllActiveModels(s)
	if len(pairs) == 0 {
		return nil, fmt.Errorf("no curated models — activate some in /providers → (select provider) → Manage models…")
	}

	// Current selection: active provider + its configured model.
	activeID := config.NormalizeProviderID(s.ActiveProvider())
	currentModel := ""
	if ep, err := provider.ResolveEndpointForProvider(s, activeID); err == nil {
		currentModel = strings.TrimSpace(ep.Model)
	}
	idx := 0
	for i, p := range pairs {
		if p.ProviderID == activeID && p.Model == currentModel {
			idx = i
			break
		}
	}
	next := pairs[(idx+1)%len(pairs)]

	if err := provider.SelectCurrentModel(a.deps.Loader, s, next.ProviderID, next.Model); err != nil {
		return nil, err
	}
	provLabel, resolvedModel, rebuildErr := a.deps.Hooks.RebuildRunner(ctx)
	_ = provLabel

	out := make(chan ui.StreamEvent, 4)
	go func() {
		defer close(out)
		if rebuildErr != nil {
			out <- ui.StreamEvent{Type: ui.StreamError, Err: rebuildErr}
			out <- ui.StreamEvent{Type: ui.StreamDone}
			return
		}
		label := next.DisplayID + "/" + next.Model
		msg := fmt.Sprintf("Model → %s", label)
		if resolvedModel != "" && resolvedModel != next.Model {
			msg += fmt.Sprintf(" (mode override active: using %s)", resolvedModel)
		}
		a.status.Right = resolvedModel
		if resolvedModel == "" {
			a.status.Right = next.Model
		}
		out <- ui.StreamEvent{Type: ui.StreamInfo, Text: msg + "\n"}
		out <- ui.StreamEvent{Type: ui.StreamDone}
	}()
	return out, nil
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

	// Keep the active provider's live model inside its curated active set. After
	// a provider switch (or any rebuild) a previously-configured model may no
	// longer be activated; coerce it to the first curated model and persist.
	activeID := h.app.deps.Settings.ActiveProvider()
	if changed, cErr := provider.CoerceModelToCurated(h.app.deps.Settings, activeID); cErr == nil && changed {
		if h.app.deps.Loader != nil {
			_ = h.app.deps.Loader.Save(h.app.deps.Settings)
		}
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
	// Pass runner.CompressionModel (resolved per call) so chat compression/
	// summarization runs against the live model user turns use — including after a
	// mid-session /mode switch that does not rebuild the runner — unless a
	// sagittarius.compression.model override is configured (AD-015 active-model
	// rule; per-utility override).
	h.app.runner.SetContextManager(
		NewContextManager(h.app.deps.Settings, gen, h.app.runner.CompressionModel, h.app.sessionID, h.app.runner.RecordUsage),
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
		Detail: systemPromptStatusDetail(h.app.runner, h.app.deps.Settings),
		Mode:   h.app.runner.InteractionMode().String(),
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
	active := h.app.deps.Settings.ActiveProvider()
	if active == "" {
		return nil
	}
	// Delegate to the shared helper that routes Gemini vs OpenAI-compat correctly.
	infos, _ := discoverModelInfos(ctx, h.app.deps.Settings, active)
	return infos
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

func (h *appHooks) SetInteractionMode(ctx context.Context, mode modes.Mode) (string, error) {
	if h.app == nil || h.app.runner == nil {
		return "", fmt.Errorf("runner not available")
	}
	settings := h.app.deps.Settings

	// Resolve this mode's provider override, if any.
	modeProvider := ""
	if settings != nil && settings.Sagittarius != nil && settings.Sagittarius.Modes != nil {
		mc := modeConfigForMode(settings.Sagittarius.Modes, mode)
		if mc != nil {
			modeProvider = config.NormalizeProviderID(mc.Provider)
		}
	}
	currentActive := ""
	if settings != nil {
		currentActive = config.NormalizeProviderID(settings.ActiveProvider())
	}

	// Deterministic target: use the mode's provider when set, otherwise fall
	// back to the base provider the user selected (or started with). This
	// replaces the fragile needProviderRevert branch — the logic is now a
	// single comparison instead of two separate switch/revert conditions.
	base := h.app.baseProviderID
	target := modeProvider
	if target == "" {
		target = base
	}

	if target != "" && target != currentActive {
		if err := provider.SetActiveProvider(settings, target); err != nil {
			return "", fmt.Errorf("mode %s: switch provider to %q: %w", mode.String(), target, err)
		}
		if _, _, err := h.RebuildRunner(ctx); err != nil {
			// Revert in-memory on failure so subsequent calls see a consistent state.
			_ = provider.SetActiveProvider(settings, currentActive)
			return "", fmt.Errorf("mode %s: rebuild runner: %w", mode.String(), err)
		}
	}

	model := h.app.runner.SetInteractionMode(mode)
	h.app.status.Right = model
	h.app.status.Mode = mode.String()
	return model, nil
}

// modeConfigForMode returns the SagittariusModeConfig for the given mode, or nil.
func modeConfigForMode(m *config.SagittariusModes, mode modes.Mode) *config.SagittariusModeConfig {
	if m == nil {
		return nil
	}
	switch mode {
	case modes.ModePlan:
		return m.Plan
	case modes.ModeAsk:
		return m.Ask
	case modes.ModeDebug:
		return m.Debug
	case modes.ModeAgent:
		return m.Agent
	default:
		return nil
	}
}

func (h *appHooks) InteractionMode() (modes.Mode, string) {
	if h.app == nil || h.app.runner == nil {
		return modes.ModeAgent, ""
	}
	mode := h.app.runner.InteractionMode()
	return mode, h.app.runner.Model()
}

func (h *appHooks) SnapshotDiff(pathFilter string) (string, error) {
	if h.app == nil || h.app.runner == nil {
		return "", fmt.Errorf("runner not available")
	}
	return h.app.runner.SnapshotDiff(pathFilter)
}

func (h *appHooks) SnapshotUndo(n int) ([]string, error) {
	if h.app == nil || h.app.runner == nil {
		return nil, fmt.Errorf("runner not available")
	}
	return h.app.runner.SnapshotUndo(n)
}

// SelectCurrentModel switches the active (provider, model) globally and rebuilds
// the runner. Called by /model command and the modelpick dialog.
func (h *appHooks) SelectCurrentModel(ctx context.Context, providerID, model string) (string, error) {
	if h.app == nil || h.app.runner == nil {
		return "", fmt.Errorf("runner not available")
	}
	if err := provider.SelectCurrentModel(h.app.deps.Loader, h.app.deps.Settings, providerID, model); err != nil {
		return "", err
	}
	// An explicit /model pick redefines the base. Any subsequent mode switch
	// that has no provider override will return to this provider, not the
	// startup-default one.
	h.app.baseProviderID = config.NormalizeProviderID(providerID)
	_, resolvedModel, err := h.RebuildRunner(ctx)
	if err != nil {
		return "", err
	}
	return resolvedModel, nil
}

// AllActiveModels returns every curated (provider, model) pair for the /model
// picker and autocomplete.
func (h *appHooks) AllActiveModels() []provider.ProviderModelPair {
	if h.app == nil || h.app.deps.Settings == nil {
		return nil
	}
	return provider.AllActiveModels(h.app.deps.Settings)
}

// systemPromptStatusDetail returns the human-readable system-prompt preset label
// for the footer (e.g. "Programmer (low context)").
func systemPromptStatusDetail(runner *Runner, settings *config.Settings) string {
	if settings == nil {
		return "Programmer"
	}
	providerID := settings.ActiveProvider()
	model := ""
	if runner != nil {
		model = runner.Model()
	}
	if presetID := provider.CurrentSystemPromptPreset(settings, providerID); presetID != "" {
		if p, ok := config.LookupPreset(presetID); ok {
			return p.Label
		}
	}
	personality := config.ResolvePersonality(settings, providerID, model)
	variant := config.ResolveVariant(settings, providerID, model)
	if p, ok := config.PresetForPersonalityVariant(personality, variant); ok {
		return p.Label
	}
	return "Programmer"
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
	scheduler := r.newToolScheduler(registry)
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

// ReloadSystemInstruction re-reads AGENTS.md memory and recomposes the system
// prompt (personality prompt + memory + mode suffix).
func (r *Runner) ReloadSystemInstruction() error {
	memory, err := DiscoverSystemInstruction(r.workDir)
	if err != nil {
		return err
	}
	r.modelMu.Lock()
	r.memory = memory
	r.modelMu.Unlock()
	r.rebuildSystem()
	return nil
}
