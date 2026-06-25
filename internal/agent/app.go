package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/undeadindustries/sagittarius/internal/agents"
	"github.com/undeadindustries/sagittarius/internal/atmention"
	"github.com/undeadindustries/sagittarius/internal/bgproc"
	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/contextmgmt"
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
	// Documents holds both settings tiers (global + optional project) and the
	// merged view. When set, RebuildRunner reads from Documents.Merged for
	// endpoint resolution so project-scoped mode overrides take effect after
	// in-session mutations. Settings must equal Documents.Global when Documents
	// is provided (so all mutation code continues to write to the correct file).
	Documents *config.Documents
	// SessionID keys the context manager's adaptive state and offload dirs. It
	// is reused across provider switches so offload paths stay stable.
	SessionID string
	// BaseProviderID is the canonical provider id that was active at startup
	// before any mode-driven provider override. buildRunner resolves this and
	// passes it here; SetInteractionMode uses it to return to the right base
	// when leaving a mode whose override is empty.
	BaseProviderID string
}

// App implements the optional ui.Completer, ui.MentionCompleter, and
// ui.MetricsProvider interfaces.
var (
	_ ui.Completer              = (*App)(nil)
	_ ui.MentionCompleter       = (*App)(nil)
	_ ui.MetricsProvider        = (*App)(nil)
	_ ui.ComposerStatusProvider = (*App)(nil)
)

// App adapts Runner to ui.App for interactive TUI sessions.
type App struct {
	runner    *Runner
	runtime   *Runtime
	statusMu  sync.RWMutex // guards status
	status    ui.StatusBar
	processor *slash.Processor
	deps      slash.Deps
	sessionID string
	// docs holds both settings tiers and the merged view. RebuildRunner reads
	// from docs.Merged so project-scoped overrides (modes, model routing) take
	// effect after in-session saves. Nil when the caller did not supply Documents
	// (e.g. tests that only set Loader/Settings directly).
	docs *config.Documents
	// generatorCache eliminates repeated client initialisation (DNS + TLS +
	// genai.NewClient) on mode switches that involve a provider override. The
	// cache is self-invalidating: any change to connection parameters or
	// credentials produces a miss automatically.
	generatorCache *provider.GeneratorCache
	// providerDisplay is the current provider's display id (e.g. "openrouter",
	// "gemini"). It backs the "{provider} - {model}" footer label and the exit
	// summary Provider row, kept in sync on every rebuild / model / mode change.
	providerDisplay string
	// baseProviderID records the canonical provider id that was active before a
	// mode override temporarily switched to a different provider. Empty when no
	// provider override is active.
	baseProviderID string
	// mentions is the lazily-built "@path" completion index over the runner's
	// workspace. nil until the first CompleteMention call.
	mentions *atmention.Index
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
		runner:          cfg.Runner,
		runtime:         cfg.Runtime,
		processor:       slash.NewProcessor(),
		sessionID:       cfg.SessionID,
		docs:            cfg.Documents,
		generatorCache:  provider.NewGeneratorCache(),
		baseProviderID:  config.NormalizeProviderID(cfg.BaseProviderID),
		providerDisplay: cfg.ProviderLabel,
		status: ui.StatusBar{
			Left:   workspaceLeft(cfg.Runner),
			Right:  providerModelLabel(cfg.ProviderLabel, cfg.Model),
			Detail: systemPromptStatusDetail(cfg.Runner, cfg.Settings),
			Mode:   mode.String(),
		},
		deps: slash.Deps{
			Loader:    cfg.Loader,
			Settings:  cfg.Settings,
			Documents: cfg.Documents,
			Hooks:     &appHooks{app: nil},
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
	a.statusMu.RLock()
	defer a.statusMu.RUnlock()
	return a.status
}

// providerModelLabel composes the footer's primary label as "{provider} - {model}"
// (e.g. "openrouter - qwen/qwen3.7-plus"), degrading gracefully when either part
// is empty. Live token/context metrics are appended later by the TUI.
func providerModelLabel(providerDisplay, model string) string {
	providerDisplay = strings.TrimSpace(providerDisplay)
	model = strings.TrimSpace(model)
	switch {
	case providerDisplay == "" && model == "":
		return ""
	case providerDisplay == "":
		return model
	case model == "":
		return providerDisplay
	default:
		return providerDisplay + " - " + model
	}
}

// SessionMetrics implements ui.MetricsProvider, exposing live session telemetry
// for the footer and exit summary. Provider/session identifiers come from the
// app; counts and token estimates come from the runner.
func (a *App) SessionMetrics() ui.SessionStats {
	if a.runner == nil {
		return ui.SessionStats{SessionID: a.sessionID, Provider: a.providerDisplay}
	}
	stats := a.runner.Stats()
	// Prefer the recorder's live session ID: after /clear or /chat resume the
	// recorder rotates to a new UUID, so a.sessionID (PID-based) would point
	// at the empty starter file rather than the file that holds the turns.
	if live := a.runner.CurrentSessionID(); live != "" {
		stats.SessionID = live
	} else {
		stats.SessionID = a.sessionID
	}
	stats.Provider = a.providerDisplay
	return stats
}

// ComposerStatus implements ui.ComposerStatusProvider, exposing the tool
// approval policy and skill count for the TUI composer status row.
func (a *App) ComposerStatus() ui.ComposerStatus {
	cs := ui.ComposerStatus{}
	if a.runner != nil {
		cs.ApprovalMode = string(a.runner.ApprovalMode())
	}
	if a.runtime != nil && a.runtime.Catalog != nil {
		cs.SkillCount = len(a.runtime.Catalog.SkillManager().Skills())
	}
	if s := a.effectiveSettings(); s != nil {
		providerID := config.NormalizeProviderID(s.ActiveProvider())
		model := ""
		if ep, err := provider.ResolveEndpointForProvider(s, providerID); err == nil {
			model = strings.TrimSpace(ep.Model)
		}
		cs.ShowThinking = config.ResolveShowThinking(s, providerID, model)
	}
	return cs
}

// effectiveSettings returns the merged (global+project) view for runtime READS
// (lists, pickers, autocomplete, resolution). Writes must target a scope via
// Documents; never mutate or persist through this pointer.
func (a *App) effectiveSettings() *config.Settings {
	if a.docs != nil && a.docs.Merged != nil {
		return a.docs.Merged
	}
	return a.deps.Settings
}

// persistGlobal is the single entry point for global scalar settings WRITES. It
// routes through Documents.MutateGlobal so Merged is refreshed atomically with
// the on-disk save; the raw Loader.Save path is only a fallback for tests that
// construct an App without Documents.
func (a *App) persistGlobal(fn func(*config.Settings) error) error {
	if a.docs != nil {
		return a.docs.MutateGlobal(fn)
	}
	if a.deps.Settings == nil {
		return fmt.Errorf("settings not loaded")
	}
	if err := fn(a.deps.Settings); err != nil {
		return err
	}
	if a.deps.Loader != nil {
		if err := a.deps.Loader.Save(a.deps.Settings); err != nil {
			return fmt.Errorf("save settings: %w", err)
		}
	}
	return nil
}

// SetShowThinking implements ui.ThinkingController: it persists the global
// thinking-box visibility (ui.showThinking) so a live Ctrl+T toggle survives
// restart. The per-provider/model overrides are managed via /models.
func (a *App) SetShowThinking(on bool) error {
	return a.persistGlobal(func(s *config.Settings) error { return s.SetUIShowThinking(on) })
}

// CycleTheme implements ui.ThemeController: it toggles the TUI color theme
// between "default" and "greyscale", persists the choice (ui.theme), and returns
// the new name so the TUI applies it live. Backs the Alt+T shortcut.
func (a *App) CycleTheme() (string, error) {
	s := a.effectiveSettings()
	if s == nil {
		return "", fmt.Errorf("settings not loaded")
	}
	next := "greyscale"
	if strings.EqualFold(strings.TrimSpace(s.UI().Theme), "greyscale") {
		next = "default"
	}
	if err := a.persistGlobal(func(s *config.Settings) error { return s.SetUITheme(next) }); err != nil {
		return "", err
	}
	return next, nil
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

// CompleteMention implements ui.MentionCompleter, providing "@path" file
// completions sourced from the runner's workspace. It is read-only and
// non-blocking (cached workspace listing) so the TUI can call it per keystroke.
func (a *App) CompleteMention(input string, cursor int) ui.Completions {
	if a.runner == nil {
		return ui.Completions{}
	}
	if a.mentions == nil {
		a.mentions = atmention.NewIndex(a.runner.Workspace())
	}
	return a.mentions.Complete(input, cursor)
}

// CycleInteractionMode advances agent → plan → ask → debug → agent.
func (a *App) CycleInteractionMode(ctx context.Context) (<-chan ui.StreamEvent, error) {
	if a.runner == nil {
		return nil, fmt.Errorf("runner not available")
	}
	return a.switchToMode(ctx, modes.CycleMode(a.runner.InteractionMode()))
}

// SetModeByName switches directly to the named interaction mode (agent, plan,
// ask, debug). It backs the Alt+1..4 shortcuts; the UI passes a plain string so
// the bubbletea layer stays free of the modes package.
func (a *App) SetModeByName(ctx context.Context, name string) (<-chan ui.StreamEvent, error) {
	if a.runner == nil {
		return nil, fmt.Errorf("runner not available")
	}
	mode, err := modes.ParseMode(name)
	if err != nil {
		return nil, err
	}
	return a.switchToMode(ctx, mode)
}

// switchToMode rebuilds the runner for the target mode and emits the standard
// status events, shared by the cycle and direct-set entry points. It reuses the
// /mode slash path so mode-provider overrides and footer updates stay identical.
func (a *App) switchToMode(ctx context.Context, mode modes.Mode) (<-chan ui.StreamEvent, error) {
	return a.handleSlash(ctx, "/mode "+mode.String())
}

// wrapIndex returns idx advanced by step within [0,n), wrapping in both
// directions so a forward (+1) or reverse (-1) model cycle stays in range.
func wrapIndex(idx, step, n int) int {
	if n <= 0 {
		return 0
	}
	return ((idx+step)%n + n) % n
}

// CycleModel advances the global curated model list circularly across all
// providers (Ctrl+/), updates the active provider+model, and rebuilds the runner.
func (a *App) CycleModel(ctx context.Context) (<-chan ui.StreamEvent, error) {
	return a.cycleModel(ctx, +1)
}

// CycleModelReverse steps backward through the global curated model list
// (Ctrl+Shift+P), the inverse of CycleModel.
func (a *App) CycleModelReverse(ctx context.Context) (<-chan ui.StreamEvent, error) {
	return a.cycleModel(ctx, -1)
}

// cycleModel moves the active selection step places through the global curated
// (provider, model) list (wrapping), updates the active provider+model, rebuilds
// the runner, and emits an info message showing the resolved live model.
func (a *App) cycleModel(ctx context.Context, step int) (<-chan ui.StreamEvent, error) {
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
	next := pairs[wrapIndex(idx, step, len(pairs))]

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
		model := resolvedModel
		if model == "" {
			model = next.Model
		}
		a.statusMu.Lock()
		a.status.Right = providerModelLabel(a.providerDisplay, model)
		a.statusMu.Unlock()
		out <- ui.StreamEvent{Type: ui.StreamInfo, Text: msg + "\n"}
		out <- ui.StreamEvent{Type: ui.StreamDone}
	}()
	return out, nil
}

func (a *App) handleSlash(ctx context.Context, input string) (<-chan ui.StreamEvent, error) {
	out := make(chan ui.StreamEvent, 8)
	go func() {
		defer close(out)
		// Process runs here, off the Bubble Tea Update goroutine, so disk/network
		// slash handlers (/mcp reload, /compress, /model, /tools…) never freeze the
		// UI; the spinner animates and Esc cancels via ctx while it runs.
		result := a.processor.Process(ctx, input, a.deps)
		a.emitSlashResult(ctx, result, out)
	}()
	return out, nil
}

// emitSlashResult translates a processed slash Result into the UI event stream.
// It runs on handleSlash's goroutine (never the Update goroutine). UI-thread-only
// effects (clipboard, theme, scrollback) are emitted as events so the TUI applies
// them safely on its own loop.
func (a *App) emitSlashResult(ctx context.Context, result slash.Result, out chan<- ui.StreamEvent) {
	if result.Quit {
		out <- ui.StreamEvent{Type: ui.StreamQuit}
		out <- ui.StreamEvent{Type: ui.StreamDone}
		return
	}
	if result.ClearScrollback {
		out <- ui.StreamEvent{Type: ui.StreamClearScrollback}
	}
	for _, entry := range result.Scrollback {
		out <- ui.StreamEvent{
			Type:           ui.StreamScrollback,
			Text:           entry.Text,
			ScrollbackRole: mapScrollRole(entry.Role),
		}
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
	if result.Clipboard != "" {
		out <- ui.StreamEvent{Type: ui.StreamCopyToClipboard, Text: result.Clipboard}
	}
	if result.ThemeName != "" {
		out <- ui.StreamEvent{Type: ui.StreamSetTheme, Text: result.ThemeName}
	}
	if result.MouseMode != "" {
		out <- ui.StreamEvent{Type: ui.StreamSetMouse, Text: result.MouseMode}
	}
	if result.SubmitPrompt != "" {
		// Hand off to a real model turn (e.g. /init analyzing the project) by
		// merging RunTurn's events into this stream. RunTurn emits its own
		// terminal StreamDone, so we do not emit one here.
		turnEvents, err := a.runner.RunTurn(ctx, result.SubmitPrompt)
		if err != nil {
			out <- ui.StreamEvent{Type: ui.StreamError, Err: err}
			out <- ui.StreamEvent{Type: ui.StreamDone}
			return
		}
		for ev := range turnEvents {
			out <- ev
		}
		return
	}
	out <- ui.StreamEvent{Type: ui.StreamDone}
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

	// Use the merged settings (global + project overlay) for endpoint resolution
	// so project-scoped mode and model overrides take effect after any save.
	// deps.Settings (= Global) remains the mutation target for dialog saves.
	effectiveSettings := h.app.deps.Settings
	if h.app.docs != nil {
		h.app.docs.ReloadMerged()
		effectiveSettings = h.app.docs.Merged
	}

	gen, err := h.app.generatorCache.GetOrCreate(ctx, effectiveSettings)
	if err != nil {
		return "", "", err
	}

	// Keep the active provider's live model inside its curated active set. After
	// a provider switch (or any rebuild) a previously-configured model may no
	// longer be activated; coerce it to the first curated model and persist.
	// Coercion uses deps.Settings (global) so any resulting save goes to the
	// global file, not the read-only merged view.
	activeID := h.app.deps.Settings.ActiveProvider()
	if changed, cErr := provider.CoerceModelToCurated(h.app.deps.Settings, activeID); cErr == nil && changed {
		if h.app.deps.Loader != nil {
			_ = h.app.deps.Loader.Save(h.app.deps.Settings)
		}
	}

	endpoint, err := provider.ResolveEndpointConfig(effectiveSettings)
	if err != nil {
		return "", "", err
	}

	h.app.runner.SetGenerator(gen)
	// Propagate the (possibly merged) effective settings so the runner's mode
	// routing, system-prompt resolution, and per-model config always use the
	// up-to-date merged view rather than the global-only snapshot it was built with.
	h.app.runner.SetSettings(effectiveSettings)
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
		NewContextManager(effectiveSettings, gen,
			h.app.runner.CompressionModel,
			h.app.runner.ActiveProviderID,
			func() string { return h.app.runner.InteractionMode().String() },
			h.app.sessionID,
			h.app.runner.RecordUsage),
	)

	// Footer uses the provider display id (e.g. "openrouter", "gemini") to match
	// the /model picker and the user-facing "{provider} - {model}" format.
	label := config.ProviderDisplayID(endpoint.ProviderID)

	h.app.providerDisplay = label
	next := ui.StatusBar{
		Left:   workspaceLeft(h.app.runner),
		Right:  providerModelLabel(label, resolvedModel),
		Detail: systemPromptStatusDetail(h.app.runner, effectiveSettings),
		Mode:   h.app.runner.InteractionMode().String(),
	}
	h.app.statusMu.Lock()
	h.app.status = next
	h.app.statusMu.Unlock()
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
	if h.app == nil {
		return nil
	}
	s := h.app.effectiveSettings()
	if s == nil {
		return nil
	}
	active := s.ActiveProvider()
	if active == "" {
		return nil
	}
	// Delegate to the shared helper that routes Gemini vs OpenAI-compat correctly.
	infos, _ := discoverModelInfos(ctx, s, active)
	return infos
}

func (h *appHooks) SetProviderAPIKey(ctx context.Context, providerID, apiKey string) error {
	return credentials.SetProviderAPIKey(ctx, providerID, apiKey)
}

func (h *appHooks) ReloadMCP(ctx context.Context) (string, error) {
	if h.app == nil || h.app.runtime == nil || h.app.runner == nil {
		return "", fmt.Errorf("runtime not available")
	}
	// Propagate the current merged settings so MCP server discovery reads any
	// project-scoped server definitions that were added since startup.
	if h.app.docs != nil {
		h.app.docs.ReloadMerged()
		h.app.runtime.SetSettings(h.app.docs.Merged)
	}
	reg, err := h.app.runtime.ReloadTools(ctx)
	if err != nil {
		return "", err
	}
	h.app.runner.SetRegistry(reg)
	return formatMCPReloadSummary(h.app.runtime.Catalog.MCPManager().States()), nil
}

// rebuildToolRegistry re-applies MCP tool include/exclude filters from the
// current settings to the already-connected tool cache and installs the rebuilt
// registry on the runner, WITHOUT reconnecting any MCP server. Tool-filter
// toggles change only policy, so this avoids the connect/list churn (and the
// child-process respawn) of a full ReloadMCP.
func (a *App) rebuildToolRegistry() error {
	if a == nil || a.runtime == nil || a.runner == nil {
		return fmt.Errorf("runtime not available")
	}
	if a.docs != nil {
		a.docs.ReloadMerged()
		a.runtime.SetSettings(a.docs.Merged)
	}
	reg, err := a.runtime.RebuildToolRegistry()
	if err != nil {
		return err
	}
	a.runner.SetRegistry(reg)
	return nil
}

// ForceCompressHistory manually compresses the live conversation context and
// returns a human-readable summary of the outcome.
func (h *appHooks) ForceCompressHistory(ctx context.Context) (string, error) {
	if h.app == nil || h.app.runner == nil {
		return "", fmt.Errorf("runner not available")
	}
	if !h.app.runner.ContextCompressionAvailable() {
		return "Context compression is only available for OpenAI-compatible chat providers; the current provider manages context server-side.", nil
	}
	info, err := h.app.runner.ForceCompress(ctx)
	if err != nil {
		return "", err
	}
	return formatCompressionResult(info), nil
}

// LastAssistantText returns the most recent assistant response text for /copy.
func (h *appHooks) LastAssistantText() string {
	if h.app == nil || h.app.runner == nil {
		return ""
	}
	return h.app.runner.LastAssistantText()
}

// SessionStatsText implements slash.Hooks. It renders live session telemetry as
// plain text for /stats; section is "", "session", "model", or "tools".
func (h *appHooks) SessionStatsText(section string) string {
	if h.app == nil {
		return "Session statistics are not available."
	}
	return ui.FormatSessionStats(h.app.SessionMetrics(), section)
}

// SetUITheme implements slash.Hooks: it persists the theme to settings.json.
func (h *appHooks) SetUITheme(name string) error {
	if h.app == nil {
		return fmt.Errorf("settings not loaded")
	}
	return h.app.persistGlobal(func(s *config.Settings) error { return s.SetUITheme(name) })
}

// formatCompressionResult renders a CompressionInfo as a user-facing line.
func formatCompressionResult(info contextmgmt.CompressionInfo) string {
	switch info.Status {
	case contextmgmt.Compressed:
		return fmt.Sprintf("Compressed context: %d → %d tokens.", info.OriginalTokenCount, info.NewTokenCount)
	case contextmgmt.ContentTruncated:
		return fmt.Sprintf("Truncated tool output: %d → %d tokens (no summary produced).", info.OriginalTokenCount, info.NewTokenCount)
	case contextmgmt.CompressionFailedEmptySummary:
		return "Compression produced no usable summary; context left unchanged."
	case contextmgmt.CompressionFailedInflatedTokenCount:
		return "Compression would have grown the context; left unchanged."
	default:
		return "Nothing to compress yet — the conversation is already small."
	}
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

func (h *appHooks) MCPToolInventory(ctx context.Context) []mcp.ServerToolInventory {
	if h.app == nil || h.app.runtime == nil {
		return nil
	}
	return h.app.runtime.Catalog.MCPManager().ToolInventory(ctx)
}

func (h *appHooks) BuiltinTools() []tools.ToolEntry {
	if h.app == nil || h.app.runtime == nil || h.app.runtime.Catalog == nil {
		return nil
	}
	entries := h.app.runtime.Catalog.BuildRegistry().ListEntries()
	out := make([]tools.ToolEntry, 0, len(entries))
	for _, e := range entries {
		if e.Source == tools.SourceMCP {
			continue
		}
		out = append(out, e)
	}
	return out
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

// checkpointTagRE constrains user-supplied checkpoint tags to a filesystem-safe
// charset so a tag can never escape the checkpoints directory or collide with
// the "checkpoint-<tag>.jsonl" naming scheme.
var checkpointTagRE = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// WriteRequestDebug writes the most recent provider request to a timestamped
// JSON file in the working directory and returns its path, for /chat debug. When
// the active generator owns its serialization the exact wire body is written;
// otherwise the provider-neutral request is written as a fallback. It errors
// when no request has been recorded yet (no message sent this session).
func (h *appHooks) WriteRequestDebug() (string, error) {
	if h.app == nil || h.app.runner == nil {
		return "", fmt.Errorf("runner not available")
	}
	data, err := h.app.runner.DebugRequest()
	if err != nil {
		return "", err
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	name := fmt.Sprintf("sagittarius-request-%s.json", time.Now().Format("20060102-150405"))
	path := filepath.Join(wd, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("write debug request: %w", err)
	}
	return path, nil
}

// CurrentHistory returns a copy of the live conversation history for /chat share.
func (h *appHooks) CurrentHistory() ([]provider.Message, error) {
	if h.app == nil || h.app.runner == nil {
		return nil, fmt.Errorf("runner not available")
	}
	return h.app.runner.History(), nil
}

func (h *appHooks) ListBackgroundProcesses() []bgproc.Process {
	if h.app == nil || h.app.runtime == nil || h.app.runtime.BgMgr == nil {
		return nil
	}
	return h.app.runtime.BgMgr.List()
}

func (h *appHooks) KillBackgroundProcess(pid int) error {
	if h.app == nil || h.app.runtime == nil || h.app.runtime.BgMgr == nil {
		return fmt.Errorf("background manager not available")
	}
	return h.app.runtime.BgMgr.Kill(pid)
}

func (h *appHooks) BackgroundProcessOutput(pid int) string {
	if h.app == nil || h.app.runtime == nil || h.app.runtime.BgMgr == nil {
		return ""
	}
	return h.app.runtime.BgMgr.Output(pid)
}

// WorkDir returns the runner's workspace root, used to keep /chat share writes
// inside the project boundary. Returns "" when no runner is available.
func (h *appHooks) WorkDir() string {
	if h.app == nil || h.app.runner == nil {
		return ""
	}
	return h.app.runner.WorkDir()
}

// checkpointsDir resolves the per-project checkpoints directory
// (~/.sagittarius/tmp/<slug>/chats/checkpoints).
func (h *appHooks) checkpointsDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	dir, err := session.ChatsDir(wd)
	if err != nil {
		return "", fmt.Errorf("resolve chats directory: %w", err)
	}
	return filepath.Join(dir, "checkpoints"), nil
}

// checkpointFileName maps a validated tag to its on-disk checkpoint filename.
func checkpointFileName(tag string) string {
	return "checkpoint-" + tag + ".jsonl"
}

// checkpointMetaFileName maps a validated tag to its provider/model sidecar
// filename. The ".meta.json" suffix keeps it out of the ".jsonl" tag listing.
func checkpointMetaFileName(tag string) string {
	return "checkpoint-" + tag + ".meta.json"
}

// validCheckpointTag validates a user-supplied checkpoint tag, rejecting empty
// strings, path traversal, and any character outside [A-Za-z0-9._-].
func validCheckpointTag(tag string) error {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return fmt.Errorf("checkpoint tag is required")
	}
	if tag == "." || tag == ".." {
		return fmt.Errorf("invalid checkpoint tag %q: use letters, digits, '.', '_' or '-'", tag)
	}
	if !checkpointTagRE.MatchString(tag) {
		return fmt.Errorf("invalid checkpoint tag %q: use letters, digits, '.', '_' or '-'", tag)
	}
	return nil
}

// checkpointMeta records the provider and model a checkpoint was saved under, so
// /chat resume can warn when restoring it into a different provider.
type checkpointMeta struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	SavedAt  string `json:"savedAt"`
}

// SaveCheckpoint serialises the live in-memory conversation to a named
// checkpoint and returns the destination path. It refuses to overwrite an
// existing checkpoint unless overwrite is true and guards against saving an
// empty conversation. A best-effort metadata sidecar records the active
// provider/model for the resume-mismatch warning.
//
// The checkpoint is written from runner.History() rather than copied from the
// active session file: after a session rotation (/chat resume, /clear) or a
// context compression the on-disk file no longer matches the in-memory history,
// and the in-memory history is the conversation the user means to save.
func (h *appHooks) SaveCheckpoint(tag string, overwrite bool) (string, error) {
	if h.app == nil || h.app.runner == nil {
		return "", fmt.Errorf("runner not available")
	}
	if err := validCheckpointTag(tag); err != nil {
		return "", err
	}
	history := h.app.runner.History()
	if len(history) == 0 {
		return "", fmt.Errorf("no conversation to save yet — send a message first")
	}
	dir, err := h.checkpointsDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create checkpoints directory: %w", err)
	}
	trimmed := strings.TrimSpace(tag)
	dst := filepath.Join(dir, checkpointFileName(trimmed))
	if !overwrite {
		if _, statErr := os.Stat(dst); statErr == nil {
			return "", fmt.Errorf("checkpoint %q already exists; add 'force' to overwrite: /chat save %s force", trimmed, trimmed)
		}
	}
	if err := session.WriteHistory(dst, h.app.sessionID, "", "", history, h.app.runner.SessionGrants()); err != nil {
		return "", err
	}
	h.writeCheckpointMeta(dir, trimmed)
	return dst, nil
}

// writeCheckpointMeta persists the provider/model sidecar next to a checkpoint.
// Failures are non-fatal (logged): the sidecar only powers a resume-time warning.
func (h *appHooks) writeCheckpointMeta(dir, tag string) {
	meta := checkpointMeta{
		Provider: h.app.providerDisplay,
		Model:    h.app.runner.Model(),
		SavedAt:  time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		slog.Warn("checkpoint metadata marshal failed", "tag", tag, "error", err)
		return
	}
	if err := os.WriteFile(filepath.Join(dir, checkpointMetaFileName(tag)), data, 0o600); err != nil {
		slog.Warn("checkpoint metadata write failed", "tag", tag, "error", err)
	}
}

// readCheckpointMeta returns the provider/model sidecar for a checkpoint, or nil
// when it is absent or unreadable (older checkpoints predate the sidecar).
func (h *appHooks) readCheckpointMeta(dir, tag string) *checkpointMeta {
	data, err := os.ReadFile(filepath.Join(dir, checkpointMetaFileName(tag)))
	if err != nil {
		return nil
	}
	var meta checkpointMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil
	}
	return &meta
}

// ListCheckpoints returns the sorted tags of saved checkpoints. A missing
// checkpoints directory yields an empty list, not an error.
func (h *appHooks) ListCheckpoints() ([]string, error) {
	dir, err := h.checkpointsDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read checkpoints directory: %w", err)
	}
	const prefix, suffix = "checkpoint-", ".jsonl"
	var tags []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
			continue
		}
		tag := strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix)
		if tag != "" {
			tags = append(tags, tag)
		}
	}
	sort.Strings(tags)
	return tags, nil
}

// ResumeCheckpoint restores a saved checkpoint into the live runner history and
// rotates the session recorder. It returns a human-readable summary.
//
// v1 limitation: the resumed prior turns remain only in the checkpoint file.
// After resume the recorder rotates to a fresh session file, so new turns are
// recorded from the resume point forward rather than appended to the
// checkpoint's history.
func (h *appHooks) ResumeCheckpoint(ctx context.Context, tag string) (string, []provider.Message, error) {
	_ = ctx
	if h.app == nil || h.app.runner == nil {
		return "", nil, fmt.Errorf("runner not available")
	}
	if err := validCheckpointTag(tag); err != nil {
		return "", nil, err
	}
	dir, err := h.checkpointsDir()
	if err != nil {
		return "", nil, err
	}
	trimmed := strings.TrimSpace(tag)
	path := filepath.Join(dir, checkpointFileName(trimmed))
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return "", nil, fmt.Errorf("checkpoint %q not found", tag)
		}
		return "", nil, fmt.Errorf("stat checkpoint: %w", err)
	}
	record, err := session.LoadSession(path)
	if err != nil {
		return "", nil, fmt.Errorf("load checkpoint %q: %w", tag, err)
	}
	history := session.ConvertToProviderHistory(record)
	h.app.runner.ReplaceHistory(history, record.SessionGrants)
	h.app.runner.RotateSession()

	var b strings.Builder
	// Sagittarius history is provider-neutral, so cross-provider resume is safe;
	// we still warn (rather than block, as gemini-cli does) because thought
	// signatures and reasoning state are provider-specific and not replayed.
	if meta := h.readCheckpointMeta(dir, trimmed); meta != nil && meta.Provider != "" && meta.Provider != h.app.providerDisplay {
		fmt.Fprintf(&b, "Note: checkpoint was saved under %q; you are now on %q.\n", meta.Provider, h.app.providerDisplay)
	}
	fmt.Fprintf(&b, "Resumed checkpoint %q (%d messages). New turns record to a fresh session.", trimmed, len(history))
	return b.String(), history, nil
}

// DeleteCheckpoint removes a saved checkpoint file.
func (h *appHooks) DeleteCheckpoint(tag string) error {
	if err := validCheckpointTag(tag); err != nil {
		return err
	}
	dir, err := h.checkpointsDir()
	if err != nil {
		return err
	}
	trimmed := strings.TrimSpace(tag)
	path := filepath.Join(dir, checkpointFileName(trimmed))
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("checkpoint %q not found", tag)
		}
		return fmt.Errorf("delete checkpoint %q: %w", tag, err)
	}
	// Best-effort sidecar cleanup; absence is fine (older checkpoints have none).
	if err := os.Remove(filepath.Join(dir, checkpointMetaFileName(trimmed))); err != nil && !os.IsNotExist(err) {
		slog.Warn("checkpoint metadata delete failed", "tag", trimmed, "error", err)
	}
	return nil
}

func (h *appHooks) SetInteractionMode(ctx context.Context, mode modes.Mode) (string, error) {
	if h.app == nil || h.app.runner == nil {
		return "", fmt.Errorf("runner not available")
	}
	// Use the merged view so project-scoped mode overrides (sagittarius.modes.*
	// and the active provider) are respected. Mutations (SetActiveProvider) still
	// target deps.Settings (= global) so writes go to the correct file.
	settings := h.app.deps.Settings
	if h.app.docs != nil {
		settings = h.app.docs.Merged
	}

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
	h.app.statusMu.Lock()
	h.app.status.Right = providerModelLabel(h.app.providerDisplay, model)
	h.app.status.Mode = mode.String()
	h.app.statusMu.Unlock()
	return model, nil
}

// modeConfigForMode returns the SagittariusModeConfig for the given mode, or nil.
// It delegates to config.ModeConfig so reads and writes share one mode→slot switch.
func modeConfigForMode(m *config.SagittariusModes, mode modes.Mode) *config.SagittariusModeConfig {
	return config.ModeConfig(m, mode.String())
}

func (h *appHooks) InteractionMode() (modes.Mode, string) {
	if h.app == nil || h.app.runner == nil {
		return modes.ModeAgent, ""
	}
	mode := h.app.runner.InteractionMode()
	return mode, h.app.runner.Model()
}

// SetModeOverride persists a (provider, model) routing override for the given
// mode name to the specified scope file. An empty model clears the override.
// This implements the headless /modes override command.
func (h *appHooks) SetModeOverride(ctx context.Context, modeName, providerID, model string, scope config.SettingScope) error {
	if h.app == nil {
		return fmt.Errorf("app not available")
	}
	docs := h.app.docs
	if docs == nil {
		return fmt.Errorf("settings not loaded")
	}
	if err := config.SetModeOverride(docs.TargetSettings(scope), modeName, providerID, model); err != nil {
		return err
	}
	return docs.Save(scope)
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
	if h.app == nil {
		return nil
	}
	s := h.app.effectiveSettings()
	if s == nil {
		return nil
	}
	return provider.AllActiveModels(s)
}

func (h *appHooks) ProjectSystemPromptPresetID() string {
	if h.app == nil {
		return ""
	}
	s := h.app.effectiveSettings()
	if s == nil {
		return ""
	}
	return config.ProjectSystemPromptPresetID(s)
}

func (h *appHooks) ApplyProjectSystemPromptPreset(ctx context.Context, presetID string) (string, error) {
	if h.app == nil || h.app.runner == nil {
		return "", fmt.Errorf("runner not available")
	}
	preset, ok := config.LookupPreset(presetID)
	if !ok {
		return "", fmt.Errorf("unknown system prompt preset %q", presetID)
	}

	// Write directly to docs.Project and SaveProject so the merged view updates
	// atomically. Documents is always wired in production; its absence is a
	// programming/test error, not a reason to fall back to a divergent writer.
	if h.app.docs == nil {
		return "", fmt.Errorf("settings documents not loaded")
	}
	s := h.app.docs.TargetSettings(config.ScopeProject)
	if s.Sagittarius == nil {
		s.Sagittarius = &config.SagittariusSettings{}
	}
	s.Sagittarius.SystemPrompt = &config.SagittariusSystemPromptConfig{
		Personality: config.CanonicalPersonalityID(preset.Personality),
		Variant:     config.CanonicalVariant(preset.Variant),
	}
	if err := h.app.docs.SaveProject(); err != nil {
		return "", err
	}

	if err := h.ReloadSystemInstruction(ctx); err != nil {
		return "", err
	}
	if _, _, err := h.RebuildRunner(ctx); err != nil {
		return "", err
	}
	return fmt.Sprintf("System prompt → %s (saved to .sagittarius/settings.json)", preset.Label), nil
}

// systemPromptStatusDetail returns the human-readable system-prompt preset label
// for the footer (e.g. "Programmer (low context)").
func systemPromptStatusDetail(runner *Runner, settings *config.Settings) string {
	label := resolveSystemPromptLabel(runner, settings)
	return "System Prompt: " + label
}

func resolveSystemPromptLabel(runner *Runner, settings *config.Settings) string {
	if settings == nil {
		return "Programmer"
	}
	providerID := settings.ActiveProvider()
	model := ""
	if runner != nil {
		model = runner.Model()
	}
	if presetID := config.ProjectSystemPromptPresetID(settings); presetID != "" {
		if p, ok := config.LookupPreset(presetID); ok {
			return p.Label
		}
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

// workspaceLeft returns a tilde-shortened workspace path for the footer's
// left column, or an empty string when the runner is unavailable.
func workspaceLeft(runner *Runner) string {
	if runner == nil {
		return ""
	}
	dir := runner.WorkDir()
	if dir == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		rel, relErr := filepath.Rel(home, dir)
		if relErr == nil && !strings.HasPrefix(rel, "..") {
			if rel == "." {
				return "~"
			}
			return "~" + string(filepath.Separator) + rel
		}
	}
	return dir
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
	r.modelMu.Lock()
	r.modelPinned = true
	r.modelMu.Unlock()
	r.SetModel(model)
}

// SetRegistry replaces the tool registry and rebuilds the scheduler. Safe to
// call from a slash-command handler while a turn streams on another goroutine.
func (r *Runner) SetRegistry(registry *tools.Registry) {
	if registry == nil {
		return
	}
	r.regMu.Lock()
	r.mergeSchedulerGrantsLocked()
	scheduler := tools.NewScheduler(
		registry,
		approvalToPolicy(r.approval),
		r.interactive,
		r.InteractionMode,
		r.workspace,
		r.schedulerOptions()...,
	)
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
