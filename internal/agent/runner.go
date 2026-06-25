package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/undeadindustries/sagittarius/internal/atmention"
	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/contextmgmt"
	"github.com/undeadindustries/sagittarius/internal/modes"
	"github.com/undeadindustries/sagittarius/internal/prompt"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/session"
	"github.com/undeadindustries/sagittarius/internal/snapshot"
	"github.com/undeadindustries/sagittarius/internal/tools"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

// errProviderUnavailable is surfaced when a turn runs without a usable provider
// (e.g. interactive startup with a missing API key). The user can recover with
// /auth or /provider use before the next request.
var errProviderUnavailable = errors.New("no provider configured: run /auth to set an API key or /provider use <id> to switch")

// State is the runner lifecycle phase for one user turn.
type State int

const (
	StateIdle State = iota
	StateStreaming
	StateAwaitingTools
	StateDone
)

// RunnerConfig configures a multi-turn agent loop backed by a ContentGenerator.
type RunnerConfig struct {
	Generator    provider.ContentGenerator
	Model        string
	WorkDir      string
	ApprovalMode ApprovalMode
	// Interactive enables TUI confirmation prompts; headless mode sets false.
	Interactive bool
	// ContextManager applies local-context defenses pre-turn and post-tool. Nil
	// (non-openai-chat providers) makes context management a pure pass-through.
	ContextManager *contextmgmt.Manager
	// SessionRecorder enables session persistence. Nil disables recording.
	SessionRecorder *session.Recorder
	// InitialHistory pre-populates the conversation from a resumed session.
	InitialHistory []provider.Message
	// InitialSessionGrants pre-populates tools already approved for the session.
	InitialSessionGrants []string
	// Settings enables interaction-mode model routing. Nil disables mode overrides.
	Settings *config.Settings
	// ProjectBoundary blocks out-of-project file mutations (file writes and a
	// shell heuristic) when true. Default false (backward compatible).
	ProjectBoundary bool
	// Snapshotter records write_file mutations for /diff and /undo. Nil disables
	// snapshotting.
	Snapshotter *snapshot.Manager
	// InitialMode seeds the session interaction mode. The zero value
	// (modes.ModeAgent) is authoritative, not "unset": callers that want the
	// settings default must resolve it via modes.DefaultFromSettings and pass
	// the result (cmd/sagittarius does this). This keeps an explicit ModeAgent
	// from being silently overridden by sagittarius.defaultMode.
	InitialMode modes.Mode
	// ModelPinned skips mode-based routing when true (CLI -m override).
	ModelPinned bool
	// AllowFix permits run_project_checks to run mutating formatters (fix=true).
	// Resolved from sagittarius.verify.allowFix; default false.
	AllowFix bool
	// SuggestVerifyAfterWrite emits a single info reminder to verify after a turn
	// that wrote files. Resolved from sagittarius.verify.suggestAfterWrite;
	// default false. It never runs checks automatically.
	SuggestVerifyAfterWrite bool
}

// Runner orchestrates conversation history and provider streaming for the agent loop.
type Runner struct {
	genMu                sync.RWMutex
	gen                  provider.ContentGenerator
	genErr               error
	modelMu              sync.RWMutex
	model                string
	providerDefaultModel string
	modelPinned          bool
	settingsMu           sync.RWMutex
	settings             *config.Settings
	modeState            *modes.State
	// system is the full system instruction sent to the provider:
	// systemBase + mode suffix. systemBase is the personality prompt + memory.
	// memory is the AGENTS.md content alone (re-composed on rebuild). All three
	// are guarded by modelMu (read alongside model in buildGenerateRequest).
	system           string
	systemBase       string
	memory           string
	approval         ApprovalMode
	interactive      bool
	workDir          string
	workspace        *tools.Workspace
	regMu            sync.RWMutex
	registry         *tools.Registry
	scheduler        *tools.Scheduler
	history          []provider.Message
	ctxMgrMu         sync.RWMutex
	ctxMgr           *contextmgmt.Manager
	turnCounter      int
	state            State
	stateMu          sync.RWMutex
	lastRequest      *provider.GenerateRequest
	lastRequestMu    sync.RWMutex
	sessionRecorder  *session.Recorder
	metrics          *sessionMetrics
	projectBoundary  bool
	snap             *snapshot.Manager
	suggestVerify    bool
	goplsHintPending bool
	// loadedMemoryFiles are the AGENTS.md paths that contributed to the system
	// instruction, captured at construction for the welcome banner.
	loadedMemoryFiles []string
	// initialSessionGrants seeds the scheduler.
	initialSessionGrants []string
	// turnActive guards against overlapping RunTurn calls mutating history.
	turnActive atomic.Bool
}

// LoadedMemoryFiles returns the AGENTS.md paths that contributed to the system
// instruction (global first, then project files). Used by the UI to show which
// memory files were loaded.
func (r *Runner) LoadedMemoryFiles() []string {
	return r.loadedMemoryFiles
}

// Workspace returns the runner's trusted workspace root for path validation.
// Used by the TUI to drive "@path" file-mention autocompletion.
func (r *Runner) Workspace() *tools.Workspace {
	return r.workspace
}

// NewRunner constructs a Runner and discovers project memory for the system prompt.
//
// A nil cfg.Generator is permitted for interactive sessions that start without a
// usable provider (e.g. a missing API key). Such a runner returns a recoverable
// error on each turn until SetGenerator installs a working provider. Pair a nil
// generator with SetGeneratorError to explain the cause to the user.
func NewRunner(cfg RunnerConfig) (*Runner, error) {
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, fmt.Errorf("agent runner: model is required")
	}

	workDir := cfg.WorkDir
	if strings.TrimSpace(workDir) == "" {
		var err error
		workDir, err = defaultWorkDir()
		if err != nil {
			return nil, err
		}
	}

	memory, err := DiscoverSystemInstruction(workDir)
	if err != nil {
		return nil, fmt.Errorf("agent runner: %w", err)
	}
	memoryFiles, err := DiscoverMemoryFiles(workDir)
	if err != nil {
		return nil, fmt.Errorf("agent runner: %w", err)
	}

	mode := cfg.ApprovalMode
	if mode == "" {
		mode = ApprovalDefault
	}

	ws, err := tools.NewWorkspace(workDir)
	if err != nil {
		return nil, fmt.Errorf("agent runner: workspace: %w", err)
	}
	registry := tools.NewBuiltinRegistry(ws, tools.WithAllowFix(cfg.AllowFix))
	policy := approvalToPolicy(mode)
	scheduler := tools.NewScheduler(registry, policy, cfg.Interactive, nil, ws)
	// Wire interaction-mode gate, project boundary, and snapshot hook after the
	// runner is constructed (attachInteractionModeGate rebuilds the scheduler).

	var history []provider.Message
	if len(cfg.InitialHistory) > 0 {
		history = append(history, cfg.InitialHistory...)
	}

	runner := &Runner{
		gen:                  cfg.Generator,
		model:                cfg.Model,
		providerDefaultModel: cfg.Model,
		modelPinned:          cfg.ModelPinned,
		settings:             cfg.Settings,
		modeState:            modes.NewState(cfg.InitialMode),
		memory:               memory,
		approval:             mode,
		interactive:          cfg.Interactive,
		workDir:              ws.Root(),
		workspace:            ws,
		registry:             registry,
		scheduler:            scheduler,
		ctxMgr:               cfg.ContextManager,
		state:                StateIdle,
		sessionRecorder:      cfg.SessionRecorder,
		history:              history,
		metrics:              newSessionMetrics(),
		projectBoundary:      cfg.ProjectBoundary,
		snap:                 cfg.Snapshotter,
		suggestVerify:        cfg.SuggestVerifyAfterWrite,
		goplsHintPending:     needsGoplsHint(cfg.Settings, ws.Root()),
		loadedMemoryFiles:    memoryFiles,
		initialSessionGrants: cfg.InitialSessionGrants,
	}
	if !cfg.ModelPinned {
		runner.refreshModelFromMode()
	} else {
		runner.rebuildSystem()
	}
	runner.attachInteractionModeGate()
	return runner, nil
}

func approvalToPolicy(mode ApprovalMode) tools.Policy {
	switch mode {
	case ApprovalAutoEdit:
		return tools.Policy{Mode: tools.ApprovalAutoEdit}
	case ApprovalYolo:
		return tools.Policy{Mode: tools.ApprovalYolo}
	default:
		return tools.Policy{Mode: tools.ApprovalDefault}
	}
}

// State returns the current runner lifecycle phase.
func (r *Runner) State() State {
	r.stateMu.RLock()
	defer r.stateMu.RUnlock()
	return r.state
}

// LastGenerateRequest returns the most recent provider request (for tests).
func (r *Runner) LastGenerateRequest() *provider.GenerateRequest {
	r.lastRequestMu.RLock()
	defer r.lastRequestMu.RUnlock()
	return r.lastRequest
}

// DebugRequest returns the most recent request as indented JSON for /chat debug.
// When the active generator owns its serialization (openai-chat, openai-responses)
// the exact wire body is returned; otherwise the provider-neutral GenerateRequest
// is marshalled as a faithful, if not byte-exact, fallback.
func (r *Runner) DebugRequest() ([]byte, error) {
	req := r.LastGenerateRequest()
	if req == nil {
		return nil, fmt.Errorf("no provider request recorded yet — send a message first")
	}
	r.genMu.RLock()
	gen := r.gen
	r.genMu.RUnlock()
	if dbg, ok := gen.(provider.WireRequestDebugger); ok {
		if body, err := dbg.DebugWireRequest(req); err == nil {
			return body, nil
		}
		// Fall through to the neutral request on serialization error rather than
		// failing debug entirely.
	}
	data, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal provider request: %w", err)
	}
	return data, nil
}

// Model returns the configured model id for this runner.
func (r *Runner) Model() string {
	r.modelMu.RLock()
	defer r.modelMu.RUnlock()
	return r.model
}

// CompressionModel returns the model used for context compression /
// summarization: the sagittarius.compression.model override when set, otherwise
// the live mode-resolved model. Resolved per call so it tracks mid-session model
// changes (provider switch, /mode).
func (r *Runner) CompressionModel() string {
	return modes.ResolveCompressionModel(r.sagittariusSettings(), r.Model())
}

// ToolsModel returns the model used for tool-utility calls: the
// sagittarius.tools.model override when set, otherwise the live mode-resolved
// model. Reserved for tool-utility model routing (no consumer yet).
func (r *Runner) ToolsModel() string {
	return modes.ResolveToolsModel(r.sagittariusSettings(), r.Model())
}

// ModelPinned reports whether CLI or explicit pinning bypasses mode routing.
func (r *Runner) ModelPinned() bool {
	return r.modelPinned
}

// ApprovalMode returns the active tool-approval policy (default/autoEdit/yolo).
func (r *Runner) ApprovalMode() ApprovalMode {
	return r.approval
}

// InteractionMode returns the active interaction mode.
func (r *Runner) InteractionMode() modes.Mode {
	if r.modeState == nil {
		return modes.ModeAgent
	}
	return r.modeState.Mode()
}

// SetInteractionMode switches mode and refreshes the resolved model.
func (r *Runner) SetInteractionMode(mode modes.Mode) string {
	if r.modeState != nil {
		r.modeState.SetMode(mode)
	}
	if !r.modelPinned {
		r.refreshModelFromMode()
	} else {
		r.rebuildSystem()
	}
	return r.Model()
}

// SetSettings updates settings used for mode routing (e.g. after reload).
func (r *Runner) SetSettings(s *config.Settings) {
	r.settingsMu.Lock()
	r.settings = s
	r.settingsMu.Unlock()
	if !r.modelPinned {
		r.refreshModelFromMode()
	} else {
		r.rebuildSystem()
	}
}

// SetProviderDefaultModel records the active provider's default model and
// re-resolves the effective model unless pinned.
func (r *Runner) SetProviderDefaultModel(model string) {
	model = strings.TrimSpace(model)
	if model == "" {
		return
	}
	r.providerDefaultModel = model
	if !r.modelPinned {
		r.refreshModelFromMode()
	} else {
		r.rebuildSystem()
	}
}

// GeneratorError returns the reason the runner has no usable provider, or nil
// when a generator is installed. Used to surface startup notices in the TUI.
func (r *Runner) GeneratorError() error {
	r.genMu.RLock()
	defer r.genMu.RUnlock()
	if r.gen != nil {
		return nil
	}
	if r.genErr != nil {
		return r.genErr
	}
	return errProviderUnavailable
}

// SetGeneratorError records why no provider is available so the next turn can
// explain the failure. Cleared by SetGenerator.
func (r *Runner) SetGeneratorError(err error) {
	r.genMu.Lock()
	r.genErr = err
	r.genMu.Unlock()
}

// generator returns the active provider or a recoverable error when absent.
func (r *Runner) generator() (provider.ContentGenerator, error) {
	r.genMu.RLock()
	defer r.genMu.RUnlock()
	if r.gen != nil {
		return r.gen, nil
	}
	if r.genErr != nil {
		return nil, r.genErr
	}
	return nil, errProviderUnavailable
}

// RunTurn handles one user message and streams assistant output events.
func (r *Runner) RunTurn(ctx context.Context, userInput string) (<-chan ui.StreamEvent, error) {
	userInput = strings.TrimSpace(userInput)
	if userInput == "" {
		ch := make(chan ui.StreamEvent, 1)
		close(ch)
		return ch, nil
	}

	if _, err := r.generator(); err != nil {
		ch := make(chan ui.StreamEvent, 2)
		ch <- ui.StreamEvent{Type: ui.StreamError, Err: err}
		ch <- ui.StreamEvent{Type: ui.StreamDone}
		close(ch)
		return ch, nil
	}

	if !r.turnActive.CompareAndSwap(false, true) {
		ch := make(chan ui.StreamEvent, 2)
		ch <- ui.StreamEvent{Type: ui.StreamError, Err: errors.New("a turn is already in progress")}
		ch <- ui.StreamEvent{Type: ui.StreamDone}
		close(ch)
		return ch, nil
	}

	// Expand "@path" references into the message parts sent to the model. The
	// scrollback and session history keep the raw text the user typed; only the
	// model-bound parts gain the injected file content. A resolution failure
	// (missing file, directory, binary, outside workspace) aborts the turn with a
	// surfaced error rather than silently dropping context.
	parts, err := atmention.Expand(r.workspace, userInput)
	if err != nil {
		r.turnActive.Store(false)
		ch := make(chan ui.StreamEvent, 2)
		ch <- ui.StreamEvent{Type: ui.StreamError, Err: err}
		ch <- ui.StreamEvent{Type: ui.StreamDone}
		close(ch)
		return ch, nil
	}

	r.setState(StateIdle)
	r.metrics.recordTurn()
	r.history = append(r.history, provider.Message{
		Role:  provider.RoleUser,
		Parts: parts,
	})
	if r.sessionRecorder != nil {
		r.sessionRecorder.RecordUserMessage(userInput)
	}

	out := make(chan ui.StreamEvent, 8)
	go r.runAgentLoop(ctx, out)
	return out, nil
}

// RunHeadless executes a single non-interactive turn, writing text deltas to out.
// Destructive tools are auto-denied in default/auto_edit modes unless ApprovalYolo is set.
func (r *Runner) RunHeadless(ctx context.Context, prompt string, out io.Writer) error {
	if out == nil {
		out = io.Discard
	}

	events, err := r.RunTurn(ctx, prompt)
	if err != nil {
		return err
	}

	for ev := range events {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		switch ev.Type {
		case ui.StreamTextDelta:
			if _, err := io.WriteString(out, ev.Text); err != nil {
				return fmt.Errorf("write headless output: %w", err)
			}
		case ui.StreamError:
			if ev.Err != nil {
				return ev.Err
			}
			if ev.Text != "" {
				return fmt.Errorf("%s", ev.Text)
			}
			return fmt.Errorf("stream error")
		case ui.StreamDone:
			return nil
		}
	}
	return nil
}

func (r *Runner) runAgentLoop(ctx context.Context, out chan<- ui.StreamEvent) {
	defer func() {
		r.turnActive.Store(false)
		close(out)
	}()

	gen, err := r.generator()
	if err != nil {
		out <- ui.StreamEvent{Type: ui.StreamError, Err: err}
		return
	}

	r.setState(StateStreaming)

	if r.goplsHintPending {
		r.goplsHintPending = false
		out <- ui.StreamEvent{Type: ui.StreamInfo, Text: goplsHint}
	}

	verifyHinted := false
	maxRounds := config.ResolveMaxToolRounds(r.sagittariusSettings(), tools.MaxToolRounds)

	emit := func(ev ui.StreamEvent) {
		select {
		case <-ctx.Done():
		case out <- ev:
		}
	}

	for {
		for round := 0; round < maxRounds; round++ {
			r.prepareContext(ctx)
			if !r.modelPinned {
				r.refreshModelFromMode()
			}
			req := r.buildGenerateRequest()
			r.storeLastRequest(req)
			currentModel := r.Model()
			currentProvider := r.activeProviderID()
			currentMode := r.InteractionMode().String()

			respCh, err := gen.GenerateContentStream(ctx, req)
			if err != nil {
				out <- ui.StreamEvent{Type: ui.StreamError, Err: err}
				return
			}

			toolCalls, modelText, modelParts, streamUsage, streamErr := r.consumeStream(ctx, respCh, out)
			if streamErr != nil {
				return
			}
			// Record token usage: prefer provider-reported counts; fall back to heuristics.
			if streamUsage != nil {
				r.metrics.recordTurnUsage(currentProvider, currentModel, currentMode,
					streamUsage.InputTokens, streamUsage.OutputTokens,
					streamUsage.CostUSD, streamUsage.CostKnown)
			} else {
				inTok := estimateMessageTokens(req.Messages)
				outTok := 0
				if modelText != "" {
					outTok = contextmgmt.EstimateTokens([]provider.Part{{Text: modelText}})
				}
				r.metrics.recordTurnUsage(currentProvider, currentModel, currentMode,
					inTok, outTok, 0, false)
			}

			// Prefer the provider's verbatim model parts (carries Gemini thought
			// signatures) when supplied; otherwise reconstruct from text + tool
			// calls (OpenAI-family path).
			if len(modelParts) > 0 {
				r.appendModelParts(modelParts, modelText, toolCalls)
			} else {
				r.appendModelMessage(modelText, toolCalls)
			}

			if len(toolCalls) == 0 {
				r.setState(StateDone)
				out <- ui.StreamEvent{Type: ui.StreamDone}
				return
			}

			r.setState(StateAwaitingTools)

			responses, err := r.toolScheduler().Execute(ctx, toolCalls, emit)
			if err != nil {
				out <- ui.StreamEvent{Type: ui.StreamError, Err: err}
				return
			}
			r.metrics.recordTools(len(toolCalls), countToolFailures(responses))
			r.appendFunctionResponses(responses)
			if r.suggestVerify && !verifyHinted && containsSuccessfulWrite(responses) {
				verifyHinted = true
				out <- ui.StreamEvent{Type: ui.StreamInfo, Text: verifyReminder}
			}
			r.setState(StateStreaming)
		}

		// Rounds exhausted. In interactive sessions ask the user whether to
		// continue; headless always stops to avoid runaway loops.
		if !r.interactive || !r.askContinueRounds(ctx, out, emit, maxRounds) {
			break
		}
		// User approved another batch — loop again with the same limit.
	}

	r.setState(StateDone)
	out <- ui.StreamEvent{Type: ui.StreamError, Text: "max tool rounds exceeded"}
	out <- ui.StreamEvent{Type: ui.StreamDone}
}

// prepareContext applies the local-context defenses (ejection, masking, and
// over-budget compression) to history before each generate request. It runs at
// the top of every tool round, so it acts as both the pre-turn and post-tool
// hook. Defenses degrade gracefully: on error the runner proceeds with whatever
// history PrepareTurn returns. A nil ContextManager makes this a no-op.
func (r *Runner) prepareContext(ctx context.Context) {
	mgr := r.contextManager()
	if mgr == nil {
		return
	}
	prepared, err := mgr.PrepareTurn(ctx, r.history, r.turnCounter)
	r.turnCounter++
	if prepared != nil {
		r.history = prepared
	}
	if err != nil {
		// PrepareTurn already logged; proceed with the (best-effort) history.
		return
	}
}

// SetContextManager swaps the active context manager. It is called after a
// provider change so local-context defenses match the new wire format: a nil
// manager (e.g. switching to gemini-native or openai-responses) makes context
// management a pure pass-through.
func (r *Runner) SetContextManager(mgr *contextmgmt.Manager) {
	r.ctxMgrMu.Lock()
	r.ctxMgr = mgr
	r.ctxMgrMu.Unlock()
}

func (r *Runner) contextManager() *contextmgmt.Manager {
	r.ctxMgrMu.RLock()
	defer r.ctxMgrMu.RUnlock()
	return r.ctxMgr
}

func (r *Runner) buildGenerateRequest() *provider.GenerateRequest {
	r.modelMu.RLock()
	model := r.model
	system := r.system
	r.modelMu.RUnlock()
	req := &provider.GenerateRequest{
		Model:             model,
		SystemInstruction: system,
		Messages:          append([]provider.Message(nil), r.history...),
		Tools:             r.toolRegistry().ListDeclarationsForMode(r.InteractionMode()),
	}
	// Resolve temperature against the live model so mid-session model changes
	// (mode routing) apply the right sampling without rebuilding the generator.
	req.Temperature = config.ResolveEffectiveTemperature(r.settingsSnapshot(), r.activeProviderID(), model)
	return req
}

func (r *Runner) sagittariusSettings() *config.SagittariusSettings {
	r.settingsMu.RLock()
	defer r.settingsMu.RUnlock()
	if r.settings == nil {
		return nil
	}
	return r.settings.Sagittarius
}

func (r *Runner) activeProviderID() string {
	r.settingsMu.RLock()
	defer r.settingsMu.RUnlock()
	return r.settings.ActiveProvider()
}

// ActiveProviderID returns the current active provider id (exported for callers
// outside the agent package, e.g. cmd/sagittarius when wiring NewContextManager).
func (r *Runner) ActiveProviderID() string {
	return r.activeProviderID()
}

func (r *Runner) RefreshModelFromMode() {
	r.refreshModelFromMode()
}

func (r *Runner) refreshModelFromMode() {
	mode := r.InteractionMode()
	providerID := r.activeProviderID()
	providerDefault := r.providerDefaultModel
	resolved := modes.ResolveModel(mode, r.sagittariusSettings(), providerID, providerDefault)
	modes.LogModeSelection(mode, resolved, providerID, providerDefault)
	r.modelMu.Lock()
	r.model = resolved
	r.modelMu.Unlock()
	r.rebuildSystem()
}

// rebuildSystem recomposes the base prompt (personality + memory) and then the
// full system instruction (base + mode suffix). Call it whenever the model,
// provider, settings, mode, or memory change.
func (r *Runner) rebuildSystem() {
	r.rebuildBasePrompt()
	r.applyModeSystemSuffix()
}

// rebuildBasePrompt resolves the personality and variant for the live
// (provider, model), builds the personality prompt with an honest identity
// line, and concatenates the AGENTS.md memory. The result is stored in
// systemBase (mode suffix is appended separately by applyModeSystemSuffix).
func (r *Runner) rebuildBasePrompt() {
	r.modelMu.RLock()
	model := r.model
	memory := r.memory
	r.modelMu.RUnlock()

	settings := r.settingsSnapshot()
	providerID := r.activeProviderID()

	base := prompt.Build(prompt.Options{
		Personality: prompt.ResolvePersonality(settings, providerID, model),
		Variant:     prompt.ResolveVariant(settings, providerID, model),
		Identity: prompt.Identity{
			Model:        model,
			ProviderName: r.providerDisplayName(providerID),
		},
		ToolNames:      r.toolDeclarationNames(),
		Interactive:    r.interactive,
		IsGitRepo:      isGitRepo(r.workDir),
		SandboxEnabled: false, // sandbox not ported (AD-017)
	})

	if memory = strings.TrimSpace(memory); memory != "" {
		base = strings.TrimRight(base, "\n") + "\n\n" + memory
	}

	r.modelMu.Lock()
	r.systemBase = base
	r.modelMu.Unlock()
}

func (r *Runner) applyModeSystemSuffix() {
	suffix := modes.SystemPromptSuffix(r.InteractionMode(), r.sagittariusSettings())
	r.modelMu.Lock()
	base := r.systemBase
	if suffix != "" {
		base = strings.TrimRight(base, "\n") + "\n\n" + suffix
	}
	r.system = base
	r.modelMu.Unlock()
}

// settingsSnapshot returns the current full settings under the settings lock.
func (r *Runner) settingsSnapshot() *config.Settings {
	r.settingsMu.RLock()
	defer r.settingsMu.RUnlock()
	return r.settings
}

// providerDisplayName resolves a human-readable label for providerID (built-in
// display name, custom provider displayName, or the id itself).
func (r *Runner) providerDisplayName(providerID string) string {
	if strings.TrimSpace(providerID) == "" {
		return ""
	}
	if def, ok := config.LookupBuiltInProvider(providerID); ok {
		return def.DisplayName
	}
	settings := r.settingsSnapshot()
	if settings != nil && settings.Providers != nil {
		if custom, ok := settings.Providers.Custom[providerID]; ok && custom.DisplayName != "" {
			return custom.DisplayName
		}
	}
	return providerID
}

// toolDeclarationNames lists the wire names of the registered tools for the
// prompt's "Available Tools" section.
func (r *Runner) toolDeclarationNames() []string {
	decls := r.toolRegistry().ListDeclarationsForMode(r.InteractionMode())
	names := make([]string, 0, len(decls))
	for _, d := range decls {
		if d.Name != "" {
			names = append(names, d.Name)
		}
	}
	return names
}

// isGitRepo reports whether dir (or an ancestor) contains a .git entry.
func isGitRepo(dir string) bool {
	dir = strings.TrimSpace(dir)
	for dir != "" {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return false
}

// attachInteractionModeGate wires plan/ask read-only enforcement into the scheduler.
func (r *Runner) attachInteractionModeGate() {
	r.regMu.Lock()
	defer r.regMu.Unlock()
	if r.registry == nil {
		return
	}
	r.mergeSchedulerGrantsLocked()
	r.scheduler = tools.NewScheduler(
		r.registry,
		approvalToPolicy(r.approval),
		r.interactive,
		r.InteractionMode,
		r.workspace,
		r.schedulerOptions()...,
	)
}

func (r *Runner) newToolScheduler(registry *tools.Registry) *tools.Scheduler {
	return tools.NewScheduler(
		registry,
		approvalToPolicy(r.approval),
		r.interactive,
		r.InteractionMode,
		r.workspace,
		r.schedulerOptions()...,
	)
}

// schedulerOptions returns the project-boundary and snapshot options shared by
// every scheduler the runner builds. A nil snapshot manager is passed as a nil
// Snapshotter interface (not a typed-nil) so the scheduler's nil check works.
func (r *Runner) schedulerOptions() []tools.SchedulerOption {
	opts := []tools.SchedulerOption{tools.WithProjectBoundary(r.projectBoundary)}
	if r.snap != nil {
		opts = append(opts, tools.WithSnapshotter(r.snap))
	}
	opts = append(opts,
		tools.WithSessionGrants(r.initialSessionGrants),
		tools.WithSessionGrantRecorder(func(toolName string) {
			r.rememberSessionGrant(toolName)
			if r.sessionRecorder != nil {
				_ = r.sessionRecorder.RecordSessionGrant(toolName)
			}
		}),
	)
	return opts
}

// rememberSessionGrant records a session-wide tool approval in runner state so
// scheduler rebuilds (SetRegistry, attachInteractionModeGate) keep the grant.
func (r *Runner) rememberSessionGrant(toolName string) {
	r.regMu.Lock()
	defer r.regMu.Unlock()
	r.appendInitialSessionGrantLocked(toolName)
}

func (r *Runner) appendInitialSessionGrantLocked(toolName string) {
	for _, g := range r.initialSessionGrants {
		if g == toolName {
			return
		}
	}
	r.initialSessionGrants = append(r.initialSessionGrants, toolName)
}

// mergeSchedulerGrantsLocked copies live scheduler grants into initialSessionGrants
// before rebuilding the scheduler so in-session approvals survive SetRegistry.
func (r *Runner) mergeSchedulerGrantsLocked() {
	if r.scheduler == nil {
		return
	}
	for _, g := range r.scheduler.SessionGrants() {
		r.appendInitialSessionGrantLocked(g)
	}
}

// SnapshotDiff renders the net unified diff of files changed this session,
// optionally filtered by a path substring. Returns "" when snapshots are
// disabled or nothing changed.
func (r *Runner) SnapshotDiff(pathFilter string) (string, error) {
	if r.snap == nil {
		return "", nil
	}
	return r.snap.Diff(pathFilter)
}

// SnapshotUndo reverts the last n recorded file changes and returns the
// restored relative paths.
func (r *Runner) SnapshotUndo(n int) ([]string, error) {
	if r.snap == nil {
		return nil, fmt.Errorf("snapshots are disabled")
	}
	return r.snap.Undo(n)
}

// SnapshotEnabled reports whether file snapshots are active for this session.
func (r *Runner) SnapshotEnabled() bool {
	return r.snap != nil
}

// toolRegistry returns the active tool registry under the registry lock.
func (r *Runner) toolRegistry() *tools.Registry {
	r.regMu.RLock()
	defer r.regMu.RUnlock()
	return r.registry
}

// toolScheduler returns the active tool scheduler under the registry lock.
func (r *Runner) toolScheduler() *tools.Scheduler {
	r.regMu.RLock()
	defer r.regMu.RUnlock()
	return r.scheduler
}

func (r *Runner) consumeStream(
	ctx context.Context,
	respCh <-chan provider.StreamResponse,
	out chan<- ui.StreamEvent,
) ([]provider.ToolCall, string, []provider.Part, *provider.Usage, error) {
	var modelText strings.Builder
	var toolCalls []provider.ToolCall
	var modelParts []provider.Part
	var usage *provider.Usage
	streamDone := false

	for !streamDone {
		select {
		case <-ctx.Done():
			out <- ui.StreamEvent{Type: ui.StreamError, Err: ctx.Err()}
			return nil, "", nil, nil, ctx.Err()
		case resp, ok := <-respCh:
			if !ok {
				streamDone = true
				continue
			}
			if resp.Error != nil {
				out <- ui.StreamEvent{Type: ui.StreamError, Err: resp.Error}
				return nil, "", nil, nil, resp.Error
			}
			if resp.TextDelta != "" {
				modelText.WriteString(resp.TextDelta)
			}
			if resp.Usage != nil {
				usage = resp.Usage
			}
			if len(resp.ModelParts) > 0 {
				modelParts = resp.ModelParts
			}
			toolCalls = append(toolCalls, resp.ToolCalls...)

			for _, ev := range MapStreamResponse(resp) {
				if ev.Type == ui.StreamDone {
					streamDone = true
					continue
				}
				select {
				case <-ctx.Done():
					out <- ui.StreamEvent{Type: ui.StreamError, Err: ctx.Err()}
					return nil, "", nil, nil, ctx.Err()
				case out <- ev:
				}
			}
		}
	}

	return toolCalls, modelText.String(), modelParts, usage, nil
}

func (r *Runner) appendModelMessage(text string, toolCalls []provider.ToolCall) {
	parts := make([]provider.Part, 0, 1+len(toolCalls))
	if text != "" {
		parts = append(parts, provider.Part{Text: text})
	}
	for _, call := range toolCalls {
		callCopy := call
		parts = append(parts, provider.Part{FunctionCall: &callCopy})
	}
	if len(parts) == 0 {
		return
	}
	r.history = append(r.history, provider.Message{
		Role:  provider.RoleModel,
		Parts: parts,
	})
	if r.sessionRecorder != nil {
		r.sessionRecorder.RecordModelMessage(text, toolCalls)
	}
}

// appendModelParts stores the provider's verbatim model parts (preserving
// Gemini thought signatures) in history. text and toolCalls are passed through
// to the session recorder, which persists the provider-neutral projection;
// signatures are not yet persisted across resume (tracked separately).
func (r *Runner) appendModelParts(parts []provider.Part, text string, toolCalls []provider.ToolCall) {
	if len(parts) == 0 {
		return
	}
	r.history = append(r.history, provider.Message{
		Role:  provider.RoleModel,
		Parts: parts,
	})
	if r.sessionRecorder != nil {
		r.sessionRecorder.RecordModelMessage(text, toolCalls)
	}
}

func (r *Runner) appendFunctionResponses(responses []provider.FunctionResponse) {
	if len(responses) == 0 {
		return
	}
	parts := make([]provider.Part, 0, len(responses))
	for _, resp := range responses {
		respCopy := resp
		parts = append(parts, provider.Part{FunctionResponse: &respCopy})
	}
	r.history = append(r.history, provider.Message{
		Role:  provider.RoleUser,
		Parts: parts,
	})
	if r.sessionRecorder != nil {
		r.sessionRecorder.RecordFunctionResponses(responses)
	}
}

func (r *Runner) setState(state State) {
	r.stateMu.Lock()
	r.state = state
	r.stateMu.Unlock()
}

// ClearHistory wipes the in-memory conversation history so the next turn starts fresh.
func (r *Runner) ClearHistory() {
	r.history = r.history[:0]
	r.turnCounter = 0
}

// History returns a defensive copy of the current conversation history. The
// copy is shallow (Part slices are shared), which is sufficient for read-only
// consumers such as /chat share and /chat debug; callers must not mutate the
// returned messages in place. Like ClearHistory, this assumes the single-turn
// goroutine contract — it is safe to call between turns, not during one.
func (r *Runner) History() []provider.Message {
	return append([]provider.Message(nil), r.history...)
}

// lastAssistantText returns the concatenated text of the most recent model
// message in history, or "" when there is none. Tool-call and tool-response
// parts (which carry no Text) are ignored.
func lastAssistantText(history []provider.Message) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role != provider.RoleModel {
			continue
		}
		var b strings.Builder
		for _, p := range history[i].Parts {
			if p.Text != "" {
				if b.Len() > 0 {
					b.WriteString("\n")
				}
				b.WriteString(p.Text)
			}
		}
		if b.Len() > 0 {
			return b.String()
		}
	}
	return ""
}

// LastAssistantText returns the text of the most recent assistant (model) turn,
// or "" when none exists. Like History it must be called between turns.
func (r *Runner) LastAssistantText() string {
	return lastAssistantText(r.history)
}

// ReplaceHistory swaps the in-memory conversation history for a copy of h,
// resets the context turn counter, and optionally sets the session grants.
func (r *Runner) ReplaceHistory(h []provider.Message, grants []string) {
	r.history = append([]provider.Message(nil), h...)
	r.turnCounter = 0
	r.regMu.Lock()
	r.initialSessionGrants = append([]string(nil), grants...)
	r.regMu.Unlock()
	r.attachInteractionModeGate() // Recreate scheduler to adopt new grants
}

// SessionGrants returns a copy of the current session tool grants.
func (r *Runner) SessionGrants() []string {
	r.regMu.RLock()
	defer r.regMu.RUnlock()
	if r.scheduler == nil {
		return nil
	}
	return r.scheduler.SessionGrants()
}

// ContextCompressionAvailable reports whether manual /compress can run for the
// active provider. It is false for non openai-chat providers, whose context
// manager is nil or disabled.
func (r *Runner) ContextCompressionAvailable() bool {
	return r.contextManager().CompressionAvailable()
}

// ForceCompress summarizes the current conversation history immediately via the
// context manager, replacing r.history in place, and returns the compression
// info for UI reporting. It is a no-op (CompressionNoOp) when compression is
// unavailable.
//
// Like ReplaceHistory it must be called between turns: r.history is owned by the
// turn goroutine and has no mutex. Slash handlers satisfy this contract.
func (r *Runner) ForceCompress(ctx context.Context) (contextmgmt.CompressionInfo, error) {
	newHistory, info, err := r.contextManager().ForceCompress(ctx, r.history)
	if err != nil {
		return info, err
	}
	if newHistory != nil {
		r.history = newHistory
	}
	return info, nil
}

// WorkDir returns the runner's resolved workspace root.
func (r *Runner) WorkDir() string {
	return r.workDir
}

// askContinueRounds emits a StreamToolConfirm asking the user whether to
// continue the agentic loop for another maxRounds cycles. It returns true when
// the user approves (ConfirmOnce or ConfirmSession) and false on deny or
// context cancellation.
func (r *Runner) askContinueRounds(ctx context.Context, out chan<- ui.StreamEvent, emit func(ui.StreamEvent), maxRounds int) bool {
	replyCh := make(chan ui.ConfirmDecision, 1)
	emit(ui.StreamEvent{
		Type:         ui.StreamToolConfirm,
		ToolName:     "continue_agent",
		Text:         fmt.Sprintf("Max tool rounds reached (%d). Continue for another %d rounds?", maxRounds, maxRounds),
		ConfirmReply: replyCh,
	})
	select {
	case <-ctx.Done():
		return false
	case decision := <-replyCh:
		return decision == ui.ConfirmOnce || decision == ui.ConfirmSession
	}
}

// countToolFailures counts function responses that carry an "error" key, the
// convention used by the tool scheduler for failed or denied executions.
func countToolFailures(responses []provider.FunctionResponse) int {
	n := 0
	for i := range responses {
		if _, ok := responses[i].Response["error"]; ok {
			n++
		}
	}
	return n
}

// verifyReminder is the one-line nudge emitted after a write when
// sagittarius.verify.suggestAfterWrite is enabled.
const verifyReminder = "Files were written. Verify the changes (lint, format check, type check, build, tests) " +
	"with run_project_checks or the project's own scripts before declaring the task done."

// containsSuccessfulWrite reports whether any response is a write_file call that
// completed without an error, used to gate the post-write verify reminder.
func containsSuccessfulWrite(responses []provider.FunctionResponse) bool {
	for i := range responses {
		if responses[i].Name != tools.WriteFileToolName {
			continue
		}
		if _, failed := responses[i].Response["error"]; !failed {
			return true
		}
	}
	return false
}

// goplsHint is the one-time startup nudge shown for Go projects without a gopls
// MCP server configured.
const goplsHint = "This is a Go project. For richer diagnostics and navigation, configure the " +
	"gopls MCP server (gopls v0.20+): add a \"gopls\" entry to mcpServers with command \"gopls\", " +
	"args [\"mcp\"]. See docs/code-quality.md."

// needsGoplsHint reports whether the workspace is a Go module (go.mod at root)
// with no gopls MCP server configured, in which case the startup hint applies.
func needsGoplsHint(settings *config.Settings, root string) bool {
	if root == "" {
		return false
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		return false
	}
	servers, err := settings.MCPServers()
	if err != nil {
		return false
	}
	for name, cfg := range servers {
		if strings.EqualFold(name, "gopls") || strings.EqualFold(cfg.Command, "gopls") {
			return false
		}
	}
	return true
}

// RecordUsage records token counts for an external caller (e.g. the compression
// summarizer). model and mode identify the (model,mode) pair; costUSD/costKnown
// carry the optional OpenRouter cost. Attributed as auxiliary (compression) usage
// so it does not overwrite the last-turn footer snapshot.
func (r *Runner) RecordUsage(prov, model, mode string, inTok, outTok int, costUSD float64, costKnown bool) {
	r.metrics.recordAuxUsage(prov, model, mode, inTok, outTok, costUSD, costKnown)
}

func (r *Runner) Stats() ui.SessionStats {
	turns, toolCalls, toolFailures, inTok, outTok, ctxTok,
		costUSD, costKnown, dur,
		lastIn, lastOut, lastCost, lastCostKnown := r.metrics.snapshot()
	return ui.SessionStats{
		Model:            r.Model(),
		Turns:            turns,
		ToolCalls:        toolCalls,
		ToolFailures:     toolFailures,
		InputTokens:      inTok,
		OutputTokens:     outTok,
		SessionCostUSD:   costUSD,
		SessionCostKnown: costKnown,
		LastInputTokens:  lastIn,
		LastOutputTokens: lastOut,
		LastCostUSD:      lastCost,
		LastCostKnown:    lastCostKnown,
		ContextTokens:    ctxTok,
		ContextLimit:     r.contextManager().ContextLimit(),
		Duration:         dur,
		ModelUsage:       r.metrics.usageSnapshot(),
	}
}

// RotateSession starts a new session-recording file, abandoning the current
// one. Paired with ClearHistory by /clear so post-clear turns are recorded to a
// fresh session instead of being appended to the cleared conversation. No-op
// when session recording is disabled.
func (r *Runner) RotateSession() {
	if r.sessionRecorder != nil {
		r.sessionRecorder.Rotate()
	}
}

// CurrentSessionID returns the session ID currently being recorded. After a
// Rotate (e.g. /clear or /chat resume) the recorder issues a new UUID; this
// always reflects that latest ID so the exit summary and resume hint stay
// accurate.
func (r *Runner) CurrentSessionID() string {
	if r.sessionRecorder != nil {
		return r.sessionRecorder.SessionID()
	}
	return ""
}

func (r *Runner) storeLastRequest(req *provider.GenerateRequest) {
	r.lastRequestMu.Lock()
	r.lastRequest = req
	r.lastRequestMu.Unlock()
}

func defaultWorkDir() (string, error) {
	wd, err := getWorkDir()
	if err != nil {
		return "", fmt.Errorf("resolve work dir: %w", err)
	}
	return wd, nil
}

// getWorkDir is overridden in tests.
var getWorkDir = os.Getwd
