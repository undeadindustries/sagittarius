package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/undeadindustries/sagittarius/internal/atmention"
	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/contextmgmt"
	"github.com/undeadindustries/sagittarius/internal/goal"
	"github.com/undeadindustries/sagittarius/internal/grill"
	"github.com/undeadindustries/sagittarius/internal/hooks"
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
	Runtime      *Runtime
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
	// Resolved from sagittarius.verify.allowFix; default false. Only seeds the
	// placeholder registry built by NewRunner — the live value is a
	// Catalog.builtinToggles field re-resolved by RefreshBuiltinToggles on
	// every settings change (see internal/agent/catalog.go), so a /settings
	// edit takes effect on the next registry rebuild without a restart.
	AllowFix bool
	// The remaining sagittarius.verify.* behaviors (suggestAfterWrite,
	// autoCheckAfterWrite, autoCheckModuleWide, autoCheckTimeoutSeconds,
	// repoLocalTools, editLoopThreshold) are read directly from the live
	// settings snapshot at use time (see the verify* helpers in postwrite.go)
	// rather than cached here, so /settings changes apply on the very next
	// turn with no RunnerConfig field or restart required.
	// InitialGoal pre-populates the active session goal from a resumed session.
	InitialGoal *goal.Snapshot
	// InitialGrill pre-populates the active grill-me session from a resumed session.
	InitialGrill *grill.Snapshot
	// InitialConstraints pre-populates standing session constraints (see
	// constraints.go) from a resumed session, so a "do not touch X yet" limit
	// set before --resume survives across the restart.
	InitialConstraints []string
	// InitialReadOnly seeds the standing session read-only posture.
	InitialReadOnly *bool
	// InitialReadOnlyConversational restores the turn-level conversational lock
	// from a resumed session. Without it a "don't change anything yet" said
	// before --resume would silently lift on restart.
	InitialReadOnlyConversational *bool
	// VerboseLog, when non-nil, receives a full timestamped transcript of every
	// request sent to the provider and every response/tool result received
	// (see --log-verbose). It is opt-in and independent of debug logging; the
	// Runner takes ownership and closes it from Close().
	VerboseLog io.WriteCloser
	// LivenessRelease releases the session liveness lock acquired at startup.
	// It runs in Close() so a normal shutdown drops the lock; a crash or SIGHUP
	// lets the kernel release it instead, which is exactly the unclean-exit
	// signal the resume banner relies on. Nil-safe. Best-effort and idempotent.
	LivenessRelease func()
	// HooksRegistry holds the lifecycle hooks registry.
	HooksRegistry *hooks.Registry
}

// Runner orchestrates conversation history and provider streaming for the agent loop.
type Runner struct {
	genMu  sync.RWMutex
	gen    provider.ContentGenerator
	genErr error

	runtime *Runtime
	// modelMu guards model, providerDefaultModel, modelPinned, and the
	// system-prompt fields (system, systemBase, memory) that model resolution
	// rewrites together.
	modelMu              sync.RWMutex
	model                string
	providerDefaultModel string
	modelPinned          bool
	// readOnlyPosture tracks the durable session-wide read-only state.
	readOnlyPosture bool
	// readOnlyConversational tracks the turn-level conversational read-only lock.
	readOnlyConversational bool
	// reasoningOverride* implement the ephemeral, per-(provider,model)
	// /reasoning pin. It replaces a former process-global (provider.
	// SessionReasoningOverride) that could bleed across Runner instances and
	// never invalidated itself on a provider/model switch. Storing the
	// (provider, model) it was set for and comparing on read means the
	// override self-invalidates the moment the live model changes, with no
	// explicit clear-on-switch call required at every switch call site.
	reasoningOverrideProviderID string
	reasoningOverrideModel      string
	reasoningOverrideEffort     string
	settingsMu                  sync.RWMutex
	settings                    *config.Settings
	modeState                   *modes.State
	// system is the full system instruction sent to the provider:
	// systemBase + mode suffix. systemBase is the personality prompt + memory.
	// memory is the AGENTS.md content alone (re-composed on rebuild). All three
	// are guarded by modelMu (read alongside model in buildGenerateRequest).
	system      string
	systemBase  string
	memory      string
	approval    ApprovalMode
	interactive bool
	workDir     string
	workspace   *tools.Workspace
	regMu       sync.RWMutex
	registry    *tools.Registry
	scheduler   *tools.Scheduler
	historyMu   sync.RWMutex // guards history + turnCounter + hookTurnIndex
	history     []provider.Message
	ctxMgrMu    sync.RWMutex
	ctxMgr      *contextmgmt.Manager
	turnCounter int
	// hookTurnIndex is the user-turn count reported to hooks as turn_index. It is
	// deliberately separate from turnCounter, which drives the context-management
	// cadence and resets whenever history is replaced: a hook script gating on
	// "every Nth turn" needs a count that survives --resume and /chat resume.
	hookTurnIndex   int
	state           State
	stateMu         sync.RWMutex
	lastRequest     *provider.GenerateRequest
	lastRequestMu   sync.RWMutex
	sessionRecorder *session.Recorder
	metrics         *sessionMetrics
	projectBoundary bool
	snap            *snapshot.Manager
	// editStatsMu guards editStats and nudgedPaths, the AD-080 repeated-edit
	// loop detector's per-turn state (see postwrite.go). Both maps are reset
	// at the start of every RunTurn.
	editStatsMu       sync.Mutex
	editStats         map[string]int
	editMatchFailures map[string]int
	nudgedPaths       map[string]bool
	// repoLocalMu guards repoLocalGrants, the session-lifetime memo of
	// interactive repo-local tool approvals ("allow for this session"),
	// mirroring Scheduler.sessionGrants.
	repoLocalMu      sync.Mutex
	repoLocalGrants  map[string]bool
	goplsHintPending bool
	// lastBranch records the git branch most recently written to the session
	// file so recordBranch only appends a $set line on change (a long session
	// can cross branches; sampling every turn without this guard would bloat
	// the JSONL). Empty until the first sample.
	lastBranch string
	// autoTitleMu guards autoTitleDone (fire-once titling) and titleAnnouncement
	// (the passive composer notice shown in prompt mode).
	autoTitleMu       sync.Mutex
	autoTitleDone     bool
	titleAnnouncement string
	// loadedMemoryFiles are the AGENTS.md paths that contributed to the system
	// instruction, captured at construction for the welcome banner.
	loadedMemoryFiles []string
	// initialSessionGrants seeds the scheduler.
	initialSessionGrants []string
	// turnActive guards against overlapping RunTurn calls mutating history.
	turnActive atomic.Bool

	goalMu     sync.RWMutex
	activeGoal *goal.Goal

	grillMu     sync.RWMutex
	activeGrill *grill.Session

	// constraintsMu guards constraints, the standing list of user-stated scope
	// limits ("do not touch AGENTS.md yet") that must survive masking and
	// compression, unlike a plain history message. See constraints.go.
	constraintsMu sync.RWMutex
	constraints   []string

	// verboseLog is the optional --log-verbose transcript sink; nil-safe (see
	// verboselog.go) so hot-path call sites never need to check for nil.
	verboseLog *verboseLog
	// livenessRelease drops the session liveness lock on Close(); nil when the
	// lock was not acquired (best-effort acquisition) or recording is disabled.
	livenessRelease func()
	hooksRegistry   *hooks.Registry
	firstTurnOnce   sync.Once
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

// skillResolver returns the skill manager backing "@skill:name" mentions, or
// nil when no catalog is attached (tests, degraded startup). The nil checks are
// explicit rather than a single return: handing back a nil *skills.Manager
// would box into a non-nil interface and defeat the caller's nil check.
func (r *Runner) skillResolver() atmention.SkillResolver {
	if r.runtime == nil || r.runtime.Catalog == nil {
		return nil
	}
	mgr := r.runtime.Catalog.SkillManager()
	if mgr == nil {
		return nil
	}
	return mgr
}

// SkillNames lists the installed, enabled skill names for "@skill:"
// autocompletion. It returns nil when no catalog is attached.
func (r *Runner) SkillNames() []string {
	if r.runtime == nil || r.runtime.Catalog == nil {
		return nil
	}
	mgr := r.runtime.Catalog.SkillManager()
	if mgr == nil {
		return nil
	}
	defs := mgr.Skills()
	names := make([]string, 0, len(defs))
	for _, d := range defs {
		names = append(names, d.Name)
	}
	return names
}

// Close releases resources the runner owns directly — currently only the
// optional --log-verbose transcript file. Safe to call on a nil *Runner or
// when verbose logging is disabled. Callers should defer this alongside
// Runtime.Close().
func (r *Runner) Close() error {
	if r == nil {
		return nil
	}
	// Fire SessionEnd hook synchronously with a bounded 5s timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = r.FireHookEvent(ctx, hooks.EventSessionEnd, "exit", func(inp *hooks.HookInput) {
		inp.SessionEndReason = "exit"
	}, nil)

	// Record a clean exit before tearing anything down. The marker is the only
	// way a later launch can tell a normal shutdown apart from a dropped
	// connection or crash, both of which skip this defer. Best-effort: a
	// failure here is never surfaced, since Close is already unwinding.
	if r.sessionRecorder != nil {
		_ = r.sessionRecorder.SetCleanExit()
	}
	// Drop the liveness lock on a normal shutdown. A crash or SIGHUP skips this
	// and the kernel releases the lock instead — the distinction the resume
	// banner keys off.
	if r.livenessRelease != nil {
		r.livenessRelease()
	}
	return r.verboseLog.Close()
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

	// Create the runner struct first to pass it to goal tools
	var history []provider.Message
	if len(cfg.InitialHistory) > 0 {
		history = append(history, cfg.InitialHistory...)
	}

	runner := &Runner{
		runtime:              cfg.Runtime,
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
		ctxMgr:               cfg.ContextManager,
		state:                StateIdle,
		sessionRecorder:      cfg.SessionRecorder,
		history:              history,
		metrics:              newSessionMetrics(),
		projectBoundary:      cfg.ProjectBoundary,
		snap:                 cfg.Snapshotter,
		editStats:            make(map[string]int),
		editMatchFailures:    make(map[string]int),
		nudgedPaths:          make(map[string]bool),
		repoLocalGrants:      make(map[string]bool),
		goplsHintPending:     needsGoplsHint(cfg.Settings, ws.Root()),
		loadedMemoryFiles:    memoryFiles,
		initialSessionGrants: cfg.InitialSessionGrants,
		verboseLog:           newVerboseLog(cfg.VerboseLog),
		livenessRelease:      cfg.LivenessRelease,
		hooksRegistry:        cfg.HooksRegistry,
	}

	// A resumed session has already had its first turn, so turn_index continues
	// from the restored history and FirstTurn must not fire again.
	if n := countUserTurns(history); n > 0 {
		runner.hookTurnIndex = n
		runner.firstTurnOnce.Do(func() {})
	}

	if cfg.InitialGoal != nil {
		runner.activeGoal = goal.FromSnapshot(cfg.InitialGoal)
		if runner.activeGoal != nil {
			runner.activeGoal.TurnCount = 0
			// Token baseline will be reset to current session tokens at end of setup or on Resume
			runner.activeGoal.TokensBaseline = runner.TotalSessionTokens()
		}
	}
	if cfg.InitialGrill != nil {
		runner.activeGrill = grill.FromSnapshot(cfg.InitialGrill)
	}
	if len(cfg.InitialConstraints) > 0 {
		runner.constraints = append([]string(nil), cfg.InitialConstraints...)
	}
	if cfg.InitialReadOnly != nil {
		runner.readOnlyPosture = *cfg.InitialReadOnly
	}
	if cfg.InitialReadOnlyConversational != nil {
		runner.readOnlyConversational = *cfg.InitialReadOnlyConversational
	}

	registerGoalTools(runner, registry)
	registerGrillTools(runner, registry)

	if cfg.Settings != nil && config.SubagentsEnabled(cfg.Settings, nil) {
		registry.Register(newTaskTool(runner))
	}
	registry.Register(newSaveMemoryTool(runner))

	policy := approvalToPolicy(mode)
	scheduler := tools.NewScheduler(registry, policy, cfg.Interactive, nil, ws)
	runner.scheduler = scheduler
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
	return r.debugRequestBody(req)
}

// debugRequestBody serializes req as indented JSON, preferring the active
// generator's exact wire body (openai-chat, openai-responses) when available.
// Shared by DebugRequest (/chat debug, last request only) and the per-round
// --log-verbose transcript (every request, as it is built).
func (r *Runner) debugRequestBody(req *provider.GenerateRequest) ([]byte, error) {
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

// SetReasoningOverride pins an ephemeral (non-persisted) reasoning effort for
// the live (provider, model) pair, driving /reasoning <level>. It reads
// providerID before taking modelMu (activeProviderID locks settingsMu
// separately; the two mutexes are never held nested, so this ordering is
// safe) so the recorded scope always matches what buildGenerateRequest will
// see on the next round.
func (r *Runner) SetReasoningOverride(effort string) {
	providerID := r.activeProviderID()
	r.modelMu.Lock()
	r.reasoningOverrideProviderID = providerID
	r.reasoningOverrideModel = r.model
	r.reasoningOverrideEffort = strings.TrimSpace(effort)
	r.modelMu.Unlock()
}

// ClearReasoningOverride drops the pinned reasoning override, driving
// /reasoning clear.
func (r *Runner) ClearReasoningOverride() {
	r.modelMu.Lock()
	r.reasoningOverrideProviderID = ""
	r.reasoningOverrideModel = ""
	r.reasoningOverrideEffort = ""
	r.modelMu.Unlock()
}

// ReasoningOverride returns the pinned reasoning override for the live
// (provider, model) pair, or "" when none is set or it was set for a
// different provider/model (a switch since then self-invalidates it).
func (r *Runner) ReasoningOverride() string {
	providerID := r.activeProviderID()
	r.modelMu.RLock()
	defer r.modelMu.RUnlock()
	if r.reasoningOverrideEffort == "" {
		return ""
	}
	if r.reasoningOverrideProviderID != providerID || r.reasoningOverrideModel != r.model {
		return ""
	}
	return r.reasoningOverrideEffort
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
	return r.pinned()
}

// pinned reads modelPinned under modelMu. The flag shares modelMu with model,
// providerDefaultModel, and the system-prompt fields it gates.
func (r *Runner) pinned() bool {
	r.modelMu.RLock()
	defer r.modelMu.RUnlock()
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
	if !r.pinned() {
		r.refreshModelFromMode()
	} else {
		r.rebuildSystem()
	}
	return r.Model()
}

// SetSettings updates settings used for mode routing (e.g. after reload). It
// replaces the pointer atomically under settingsMu; s must be a fresh document
// that the caller will not mutate afterwards (see settingsSnapshot's contract).
func (r *Runner) SetSettings(s *config.Settings) {
	r.settingsMu.Lock()
	r.settings = s
	r.settingsMu.Unlock()
	if !r.pinned() {
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
	r.modelMu.Lock()
	r.providerDefaultModel = model
	pinned := r.modelPinned
	r.modelMu.Unlock()
	if !pinned {
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

// auxGenerator resolves the provider/model for auxiliary, off-band model calls
// (goal evaluation and session titling). When sagittarius.goal.evaluatorProvider
// or evaluatorModel is configured, it builds a fresh generator for that pair via
// provider.NewContentGenerator against a settings clone with the pair forced
// active — deliberately bypassing the shared GeneratorCache, since the AD-058
// OpenAI-Responses lastResponseID chaining state lives on the generator instance
// and an aux call must never corrupt the main conversation's chaining. When
// neither is configured it falls back to the active generator, preserving the
// pre-existing behavior.
func (r *Runner) auxGenerator(ctx context.Context) (provider.ContentGenerator, error) {
	settings := r.settingsSnapshot()
	evalProvider, evalModel := auxEvaluatorTarget(settings)
	if evalProvider == "" && evalModel == "" {
		return r.generator()
	}

	clone := *settings
	active := config.NormalizeProviderID(evalProvider)
	if active == "" {
		active = clone.ActiveProvider()
	}
	if clone.Providers == nil {
		clone.Providers = &config.ProvidersSettings{}
	}
	prov := *clone.Providers
	prov.Extra = maps.Clone(prov.Extra)
	prov.Active = active
	if evalModel != "" {
		if err := setProviderInstanceModel(&prov, active, evalModel); err != nil {
			return nil, fmt.Errorf("aux generator: %w", err)
		}
	}
	clone.Providers = &prov

	gen, err := provider.NewContentGenerator(ctx, &clone)
	if err != nil {
		return nil, fmt.Errorf("aux generator: %w", err)
	}
	return gen, nil
}

// auxEvaluatorTarget reports the configured off-band evaluator provider/model,
// both empty when the user has not configured one.
func auxEvaluatorTarget(settings *config.Settings) (providerID, model string) {
	if settings == nil || settings.Sagittarius == nil || settings.Sagittarius.Goal == nil {
		return "", ""
	}
	return settings.Sagittarius.Goal.EvaluatorProvider, settings.Sagittarius.Goal.EvaluatorModel
}

// dedicatedAuxGenerator returns the configured off-band evaluator generator, or
// nil when none is configured. Unlike auxGenerator it never falls back to the
// primary generator: callers that run on every turn must not silently double
// the user's spend on the main model, and on openai-responses an extra call
// would advance the server-side response chain the real turn depends on.
func (r *Runner) dedicatedAuxGenerator(ctx context.Context) provider.ContentGenerator {
	p, m := auxEvaluatorTarget(r.settingsSnapshot())
	if p == "" && m == "" {
		return nil
	}
	gen, err := r.auxGenerator(ctx)
	if err != nil {
		slog.Debug("aux generator unavailable", "error", err)
		return nil
	}
	return gen
}

// setProviderInstanceModel forces Model on the instance block for providerID so
// ResolveEndpointConfig picks it up, mutating prov in place. It handles the
// built-in named fields, the typed custom map, and the raw Extra passthrough
// (where unknown provider instance blocks live).
func setProviderInstanceModel(prov *config.ProvidersSettings, providerID, model string) error {
	setModel := func(inst *config.ProviderInstanceConfig) *config.ProviderInstanceConfig {
		if inst == nil {
			inst = &config.ProviderInstanceConfig{}
		}
		c := *inst
		c.Model = model
		return &c
	}
	switch providerID {
	case string(config.BuiltInOpenAI):
		prov.OpenAI = setModel(prov.OpenAI)
	case string(config.BuiltInGeminiAPIKey):
		prov.GeminiAPIKey = setModel(prov.GeminiAPIKey)
	case string(config.BuiltInOpenAIResponses):
		prov.OpenAIResponses = setModel(prov.OpenAIResponses)
	default:
		// Custom and preset providers resolve their instance from the raw Extra
		// passthrough (provider.providerInstance); update Model inside the JSON
		// blob. The typed Custom map holds the definition, not the instance.
		raw := prov.Extra[providerID]
		var inst config.ProviderInstanceConfig
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &inst); err != nil {
				return fmt.Errorf("unmarshal provider instance: %w", err)
			}
		}
		inst.Model = model
		b, err := json.Marshal(inst)
		if err != nil {
			return fmt.Errorf("marshal provider instance: %w", err)
		}
		if prov.Extra == nil {
			prov.Extra = map[string]json.RawMessage{}
		}
		prov.Extra[providerID] = b
	}
	return nil
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

	// Expand "@path" and "@skill:name" references into the message parts sent to
	// the model. The scrollback and session history keep the raw text the user
	// typed; only the model-bound parts gain the injected file content and skill
	// instructions. A resolution failure (missing file, directory, binary,
	// outside workspace, unknown skill) aborts the turn with a surfaced error
	// rather than silently dropping context.
	parts, err := atmention.Expand(r.workspace, userInput, r.skillResolver())
	if err != nil {
		r.turnActive.Store(false)
		ch := make(chan ui.StreamEvent, 2)
		ch <- ui.StreamEvent{Type: ui.StreamError, Err: err}
		ch <- ui.StreamEvent{Type: ui.StreamDone}
		close(ch)
		return ch, nil
	}

	r.verboseLog.LogTurnStart(userInput)

	// Check conversational read-only intent. The classifier runs on every turn,
	// so it only gets a generator when the user configured a dedicated one.
	intent := classifyReadOnlyIntent(ctx, userInput, r.dedicatedAuxGenerator(ctx))
	if intent != IntentNeutral {
		changed := false
		r.modelMu.Lock()
		if intent == IntentLock && !r.readOnlyConversational {
			r.readOnlyConversational = true
			changed = true
		} else if intent == IntentUnlock && r.readOnlyConversational {
			r.readOnlyConversational = false
			changed = true
		}
		locked := r.readOnlyConversational
		r.modelMu.Unlock()
		if changed {
			if r.sessionRecorder != nil {
				if err := r.sessionRecorder.SetReadOnlyConversational(locked); err != nil {
					slog.Warn("record conversational read-only lock", "error", err)
				}
			}
			r.applyModeSystemSuffix()
		}
	}

	// Fire BeforeAgent hook before adding user prompt to history.
	hookResults, _ := r.FireHookEvent(ctx, hooks.EventBeforeAgent, "", func(inp *hooks.HookInput) {
		inp.Prompt = userInput
	}, nil)

	for _, res := range hookResults {
		if res.Output != nil {
			if res.Output.IsBlocking() || res.Output.ShouldStop() {
				r.turnActive.Store(false)
				reason := res.Output.EffectiveReason("Request denied by hook")
				ch := make(chan ui.StreamEvent, 2)
				ch <- ui.StreamEvent{Type: ui.StreamError, Err: errors.New(reason)}
				ch <- ui.StreamEvent{Type: ui.StreamDone}
				close(ch)
				return ch, nil
			}
			if addCtx := res.Output.AdditionalContext(); addCtx != "" {
				parts = append(parts, provider.Part{Text: "\n\n" + addCtx})
			}
		}
	}

	r.setState(StateIdle)
	r.metrics.recordTurn()
	r.resetEditLoopStats()
	r.recordBranch()
	r.historyMu.Lock()
	r.history = append(r.history, provider.Message{
		Role:  provider.RoleUser,
		Parts: parts,
	})
	r.hookTurnIndex++
	r.historyMu.Unlock()
	if r.sessionRecorder != nil {
		r.sessionRecorder.RecordUserMessage(userInput)
	}

	out := make(chan ui.StreamEvent, 8)
	go r.runAgentLoop(ctx, userInput, out)
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

// runAgentLoop drives one turn to completion. userInput is this turn's prompt,
// carried through so the AfterAgent/FirstTurn hook payloads describe the exchange
// that just finished rather than the first one in the session.
func (r *Runner) runAgentLoop(ctx context.Context, userInput string, out chan<- ui.StreamEvent) {
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
	// Accumulates the assistant's text across this turn's rounds so the fire-once
	// auto-title sees the whole reply, not just the final round.
	var turnReply strings.Builder

	emit := func(ev ui.StreamEvent) {
		select {
		case <-ctx.Done():
		case out <- ev:
		}
	}

outerLoop:
	for {
		for round := 0; round < maxRounds; round++ {
			r.prepareContext(ctx)
			if !r.pinned() {
				r.refreshModelFromMode()
			}
			req := r.buildGenerateRequest()
			r.storeLastRequest(req)
			currentModel := r.Model()
			currentProvider := r.activeProviderID()
			currentMode := r.InteractionMode().String()

			if r.verboseLog != nil {
				body, dbgErr := r.debugRequestBody(req)
				r.verboseLog.LogRequest(round, body, dbgErr)
			}

			respCh, err := gen.GenerateContentStream(ctx, req)
			if err != nil {
				r.verboseLog.LogError(err)
				out <- ui.StreamEvent{Type: ui.StreamError, Err: err}
				return
			}

			toolCalls, modelText, modelParts, streamUsage, streamErr := r.consumeStream(ctx, respCh, out)
			if streamErr != nil {
				r.verboseLog.LogError(streamErr)
				return
			}
			r.verboseLog.LogResponse(round, modelText, toolCalls, streamUsage)
			if modelText != "" {
				turnReply.WriteString(modelText)
			}
			// Record token usage: prefer provider-reported counts; fall back to heuristics.
			if streamUsage != nil {
				r.metrics.recordTurnUsage(currentProvider, currentModel, currentMode, r.agentKind(),
					streamUsage.InputTokens, streamUsage.OutputTokens,
					streamUsage.CostUSD, streamUsage.CostKnown)
			} else {
				inTok := estimateMessageTokens(req.Messages)
				outTok := 0
				if modelText != "" {
					outTok = contextmgmt.EstimateTokens([]provider.Part{{Text: modelText}})
				}
				r.metrics.recordTurnUsage(currentProvider, currentModel, currentMode, r.agentKind(),
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
				if r.evaluateGoalTurn(ctx, out, modelText) {
					continue outerLoop
				}
				r.maybeAutoTitle(ctx, r.firstUserText(), turnReply.String())
				r.fireAfterAgentHooks(ctx, userInput, turnReply.String(), out)
				r.setState(StateDone)
				out <- ui.StreamEvent{Type: ui.StreamDone}
				return
			}

			r.setState(StateAwaitingTools)

			responses, err := r.toolScheduler().Execute(ctx, toolCalls, emit)
			if err != nil {
				r.verboseLog.LogError(err)
				out <- ui.StreamEvent{Type: ui.StreamError, Err: err}
				return
			}
			for _, resp := range responses {
				r.verboseLog.LogToolResult(resp)
			}
			r.metrics.recordTools(len(toolCalls), countToolFailures(responses))
			r.appendFunctionResponses(responses)
			if config.VerifySuggestAfterWrite(r.settingsSnapshot(), nil) && !verifyHinted && containsSuccessfulWrite(responses) {
				verifyHinted = true
				out <- ui.StreamEvent{Type: ui.StreamInfo, Text: verifyReminder}
			}

			// AD-080 auto-check pipeline. Only continue the loop (an extra
			// model round-trip) when runPostWriteChecks actually injected
			// feedback for the model to react to; a clean check result never
			// costs a round.
			if config.VerifyAutoCheckAfterWrite(r.settingsSnapshot(), nil) {
				if writtenPaths := extractWrittenPathsFromHistory(r); len(writtenPaths) > 0 {
					if r.runPostWriteChecks(ctx, emit, writtenPaths) {
						r.setState(StateStreaming)
						continue
					}
				}
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

	r.fireAfterAgentHooks(ctx, userInput, turnReply.String(), out)
	r.setState(StateDone)
	r.verboseLog.LogInfo("max tool rounds exceeded")
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
	// Snapshot under the lock, run PrepareTurn (which may do network I/O for
	// compression) off-lock, then publish the result under the lock. Keeping the
	// critical sections to header reads/writes avoids holding historyMu across I/O.
	r.historyMu.RLock()
	current := r.history
	counter := r.turnCounter
	r.historyMu.RUnlock()

	prepared, err := mgr.PrepareTurn(ctx, current, counter)

	r.historyMu.Lock()
	r.turnCounter++
	if prepared != nil {
		r.history = prepared
	}
	r.historyMu.Unlock()
	if err != nil {
		// PrepareTurn already logged; proceed with the (best-effort) history.
		return
	}
	r.syncContextGauge()
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
	settings := r.settingsSnapshot()
	providerID := r.activeProviderID()
	r.historyMu.RLock()
	messages := append([]provider.Message(nil), r.history...)
	r.historyMu.RUnlock()
	req := &provider.GenerateRequest{
		Model:             model,
		SystemInstruction: system,
		Messages:          messages,
		Tools:             r.toolRegistry().ListDeclarationsForMode(r.InteractionMode()),
	}
	// Resolve temperature against the live model so mid-session model changes
	// (mode routing) apply the right sampling without rebuilding the generator.
	req.Temperature = config.ResolveEffectiveTemperature(settings, providerID, model)
	// Request readable thoughts when the thinking box is enabled; the Gemini
	// adapter uses this to set ThinkingConfig.IncludeThoughts; other adapters
	// ignore the field.
	req.IncludeThoughts = config.ResolveShowThinking(settings, providerID, model)
	// Resolve adaptive/fixed reasoning defaults per round so a mid-session
	// model or /reasoning change takes effect on the very next request; see
	// config.ResolveReasoningRequest for the precedence order.
	if resolution := config.ResolveReasoningRequest(settings, providerID, model, r.ReasoningOverride()); resolution != nil {
		req.Reasoning = &provider.ReasoningRequest{Effort: resolution.Effort, Enabled: resolution.Enabled}
	}
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
	r.modelMu.RLock()
	providerDefault := r.providerDefaultModel
	r.modelMu.RUnlock()
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

	toolNames := r.toolDeclarationNames()
	base := prompt.Build(prompt.Options{
		Personality: prompt.Personality(config.ResolvePersonality(settings, providerID, model)),
		Variant:     prompt.Variant(config.ResolveVariant(settings, providerID, model)),
		Identity: prompt.Identity{
			Model:        model,
			ProviderName: r.providerDisplayName(providerID),
		},
		ToolNames:      toolNames,
		Interactive:    r.interactive,
		IsGitRepo:      isGitRepo(r.workDir),
		OS:             runtime.GOOS,
		EditEnabled:    containsString(toolNames, tools.EditToolName),
		SymbolsEnabled: containsString(toolNames, tools.FindSymbolToolName),
		MemoryEnabled:  containsString(toolNames, tools.SaveMemoryToolName),
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
	// Only inject the interrogation directive while actively grilling. Paused
	// sessions must not steer the model to keep asking questions, and
	// summarizing/complete sessions are past the interview phase.
	if g := r.Grill(); g != nil && g.Status == grill.StatusActive {
		directive := grill.Directive(g.Topic, r.grillDirectiveConfig())
		if suffix != "" {
			suffix = strings.TrimRight(suffix, "\n") + "\n\n" + directive
		} else {
			suffix = directive
		}
	}
	// Standing session constraints go last (highest recency) in the suffix,
	// which is itself appended after systemBase (personality prompt + memory),
	// so they are the final text the model reads and outrank the mode suffix,
	// the grill directive, and the tool-invocation mandate baked into systemBase.
	if constraints := r.Constraints(); len(constraints) > 0 {
		directive := renderConstraintsDirective(constraints)
		if suffix != "" {
			suffix = strings.TrimRight(suffix, "\n") + "\n\n" + directive
		} else {
			suffix = directive
		}
	}

	// Add read-only gate directive if the posture or lock is active
	r.modelMu.RLock()
	roPosture := r.readOnlyPosture
	roConv := r.readOnlyConversational
	r.modelMu.RUnlock()

	if roPosture || roConv {
		directive := "**CRITICAL:** You are currently in READ-ONLY INSPECTION MODE. Mutating tools (writing files, making configuration changes, running non-inspection shell commands) are disabled and will be rejected. You MUST NOT attempt to use them. A text-only report of your findings is a correct and complete turn."
		if suffix != "" {
			suffix = strings.TrimRight(suffix, "\n") + "\n\n" + directive
		} else {
			suffix = directive
		}
	}

	r.modelMu.Lock()
	base := r.systemBase
	if suffix != "" {
		base = strings.TrimRight(base, "\n") + "\n\n" + suffix
	}
	r.system = base
	r.modelMu.Unlock()
}

// settingsSnapshot returns the current full settings under the settings lock.
//
// Contract: the returned *config.Settings is immutable once handed to the
// runner. settingsMu protects only the pointer swap (see SetSettings), not the
// pointed-to struct, so callers must treat the result as read-only. Mutators
// (dialogs, reload paths) build a fresh *Settings via config.Documents and
// publish it through SetSettings rather than editing the live object in place.
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
	// Preset fallback (e.g. an openai id resolving before migration).
	if def, ok := config.ProviderDefaults(providerID); ok && def.DisplayName != "" {
		return def.DisplayName
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

// containsString reports whether target is present in list.
func containsString(list []string, target string) bool {
	for _, v := range list {
		if v == target {
			return true
		}
	}
	return false
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
		tools.WithReadOnlyPolicy(r.readOnlyPolicy),
		tools.WithHooks(r.beforeToolHook, r.afterToolHook),
	)
	return opts
}

// readOnlyPolicy reports whether the agent should force read-only tool gating.
// It returns PolicyStrict for grill mode, PolicyInspect if a read-only lock is active,
// or PolicyNone otherwise.
func (r *Runner) readOnlyPolicy() tools.ReadOnlyPolicy {
	g := r.Grill()
	if g != nil && g.Status != grill.StatusSummarizing && g.Status != grill.StatusComplete {
		return tools.PolicyStrict
	}

	r.modelMu.RLock()
	posture := r.readOnlyPosture
	conv := r.readOnlyConversational
	r.modelMu.RUnlock()

	if posture || conv {
		return tools.PolicyInspect
	}

	return tools.PolicyNone
}

// grillDirectiveConfig resolves the interrogation directive tuning from
// sagittarius.grill settings, defaulting Recommend to true (the documented
// default) when unset.
func (r *Runner) grillDirectiveConfig() grill.DirectiveConfig {
	cfg := grill.DirectiveConfig{Recommend: true}
	if s := r.sagittariusSettings(); s != nil && s.Grill != nil {
		if s.Grill.MaxQuestions != nil {
			cfg.MaxQuestions = *s.Grill.MaxQuestions
		}
		if s.Grill.Recommend != nil {
			cfg.Recommend = *s.Grill.Recommend
		}
	}
	return cfg
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
	r.historyMu.Lock()
	r.history = append(r.history, provider.Message{
		Role:  provider.RoleModel,
		Parts: parts,
	})
	r.historyMu.Unlock()
	if r.sessionRecorder != nil {
		r.sessionRecorder.RecordModelMessage(text, toolCalls)
	}
}

// recordBranch samples the git branch for the workspace at the start of a turn
// and appends a $set line only when it changed since the last recorded value.
// It is best-effort: a non-git workspace yields "" (recorded once so the
// metadata reflects "no branch") and a recorder failure is never surfaced,
// since the branch is display-only metadata.
func (r *Runner) recordBranch() {
	if r.sessionRecorder == nil {
		return
	}
	branch := session.CurrentBranch(r.workDir)
	if branch == r.lastBranch {
		return
	}
	if err := r.sessionRecorder.SetBranch(branch); err == nil {
		r.lastBranch = branch
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
	r.historyMu.Lock()
	r.history = append(r.history, provider.Message{
		Role:  provider.RoleModel,
		Parts: parts,
	})
	r.historyMu.Unlock()
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
	r.historyMu.Lock()
	r.history = append(r.history, provider.Message{
		Role:  provider.RoleUser,
		Parts: parts,
	})
	r.historyMu.Unlock()
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
	r.historyMu.Lock()
	defer r.historyMu.Unlock()
	r.history = r.history[:0]
	r.turnCounter = 0
	r.hookTurnIndex = 0
}

// History returns a defensive copy of the current conversation history. The
// copy is shallow (Part slices are shared), which is sufficient for read-only
// consumers such as /chat share and /chat debug; callers must not mutate the
// returned messages in place. Safe to call concurrently with a streaming turn:
// historyMu guards the slice header against the turn goroutine's appends.
func (r *Runner) History() []provider.Message {
	r.historyMu.RLock()
	defer r.historyMu.RUnlock()
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
// or "" when none exists. historyMu is held for the read so it is safe to call
// concurrently with a streaming turn.
func (r *Runner) LastAssistantText() string {
	r.historyMu.RLock()
	defer r.historyMu.RUnlock()
	return lastAssistantText(r.history)
}

// firstUserText returns the text of the first user message in history, used as
// the request half of the exchange the auto-title is generated from.
func (r *Runner) firstUserText() string {
	r.historyMu.RLock()
	defer r.historyMu.RUnlock()
	for _, m := range r.history {
		if m.Role != provider.RoleUser {
			continue
		}
		var b strings.Builder
		for _, p := range m.Parts {
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

// ReplaceHistory swaps the in-memory conversation history for a copy of h,
// resets the context turn counter, and optionally sets the session grants.
// Between-turns contract, like ClearHistory.
func (r *Runner) ReplaceHistory(h []provider.Message, grants []string) {
	r.historyMu.Lock()
	r.history = append([]provider.Message(nil), h...)
	r.turnCounter = 0
	turns := countUserTurns(r.history)
	r.hookTurnIndex = turns
	r.historyMu.Unlock()
	if turns > 0 {
		r.firstTurnOnce.Do(func() {})
	}
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
// It must be called between turns. historyMu guards the snapshot and the
// publish; the compression call itself (which may do network I/O) runs off-lock.
func (r *Runner) ForceCompress(ctx context.Context) (contextmgmt.CompressionInfo, error) {
	r.historyMu.RLock()
	current := r.history
	r.historyMu.RUnlock()
	newHistory, info, err := r.contextManager().ForceCompress(ctx, current)
	if err != nil {
		return info, err
	}
	if newHistory != nil {
		r.historyMu.Lock()
		r.history = newHistory
		r.historyMu.Unlock()
		r.refreshContextGaugeAfterCompress(info)
	}
	return info, nil
}

// syncContextGauge re-estimates context fill from the live history so the footer
// reflects masking/compression without waiting for the next provider prompt_tokens.
func (r *Runner) syncContextGauge() {
	r.historyMu.RLock()
	msgs := r.history
	r.historyMu.RUnlock()
	if len(msgs) == 0 {
		return
	}
	tok := estimateMessageTokens(msgs)
	if tok > 0 {
		r.metrics.setContextTokens(tok)
	}
}

// refreshContextGaugeAfterCompress updates the footer gauge from compression
// telemetry when available, matching gemini-cli's post-compress token refresh.
func (r *Runner) refreshContextGaugeAfterCompress(info contextmgmt.CompressionInfo) {
	if info.NewTokenCount > 0 &&
		(info.Status == contextmgmt.Compressed || info.Status == contextmgmt.ContentTruncated) {
		r.metrics.setContextTokens(info.NewTokenCount)
		return
	}
	r.syncContextGauge()
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
		if !tools.IsFileMutatingTool(responses[i].Name) {
			continue
		}
		if _, failed := responses[i].Response["error"]; !failed {
			return true
		}
	}
	return false
}

func extractWrittenPathsFromHistory(r *Runner) []string {
	r.historyMu.RLock()
	defer r.historyMu.RUnlock()

	var paths []string
	if len(r.history) < 2 {
		return paths
	}

	// Find the most recent assistant turn
	var lastAsst *provider.Message
	for i := len(r.history) - 1; i >= 0; i-- {
		if r.history[i].Role == provider.RoleModel {
			lastAsst = &r.history[i]
			break
		}
	}
	if lastAsst == nil {
		return paths
	}

	// We only care about writes that just succeeded (no error in response).
	// We check the most recent user turn for the responses.
	var lastUser *provider.Message
	for i := len(r.history) - 1; i >= 0; i-- {
		if r.history[i].Role == provider.RoleUser {
			lastUser = &r.history[i]
			break
		}
	}

	if lastUser == nil {
		return paths
	}

	// Build a map of successful write_file call IDs
	successfulWrites := make(map[string]bool)
	for _, p := range lastUser.Parts {
		if p.FunctionResponse != nil && tools.IsFileMutatingTool(p.FunctionResponse.Name) {
			if _, failed := p.FunctionResponse.Response["error"]; !failed {
				successfulWrites[p.FunctionResponse.CallID] = true
			}
		}
	}

	// Now extract the paths from the assistant's calls that matched
	for _, p := range lastAsst.Parts {
		if p.FunctionCall != nil && tools.IsFileMutatingTool(p.FunctionCall.Name) {
			if successfulWrites[p.FunctionCall.ID] {
				if path, ok := p.FunctionCall.Args["file_path"].(string); ok && path != "" {
					paths = append(paths, path)
				}
			}
		}
	}

	return paths
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
	r.metrics.recordAuxUsage(prov, model, mode, r.agentKind(), inTok, outTok, costUSD, costKnown)
}

// setTitleAnnouncement records the title to announce to the user in prompt
// mode (see maybeAutoTitle). It is read by the UI layer via TitleAnnouncement
// and rendered as a passive composer line; it never blocks the composer.
func (r *Runner) setTitleAnnouncement(title string) {
	r.autoTitleMu.Lock()
	r.titleAnnouncement = title
	r.autoTitleMu.Unlock()
}

// TitleAnnouncement returns the pending auto-title announcement ("" when none)
// without consuming it, so the composer can render it on every frame. The TUI
// owns the shown-once / dismissed lifecycle: it latches the first non-empty
// value locally and clears the latch when the user acts (Ctrl+E rename,
// Enter-on-empty-input, or starting the next message). Peek semantics keep the
// value available even if a turn-end render races the latch setup.
func (r *Runner) TitleAnnouncement() string {
	r.autoTitleMu.Lock()
	defer r.autoTitleMu.Unlock()
	return r.titleAnnouncement
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
	r.firstTurnOnce = sync.Once{}
	if r.sessionRecorder != nil {
		r.sessionRecorder.Rotate()

		r.autoTitleMu.Lock()
		r.autoTitleDone = false
		r.titleAnnouncement = ""
		r.autoTitleMu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = r.FireHookEvent(ctx, hooks.EventSessionStart, "clear", func(inp *hooks.HookInput) {
			inp.SessionStartSource = "clear"
		}, nil)
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

// SessionFilePath returns the JSONL file path of the current session, or "".
func (r *Runner) SessionFilePath() string {
	if r == nil {
		return ""
	}
	if r.sessionRecorder != nil {
		return r.sessionRecorder.FilePath()
	}
	return ""
}

// TurnCounter returns the number of user turns in this conversation, reported to
// hooks as turn_index. It continues across --resume and /chat resume.
func (r *Runner) TurnCounter() int {
	if r == nil {
		return 0
	}
	r.historyMu.RLock()
	defer r.historyMu.RUnlock()
	return r.hookTurnIndex
}

// countUserTurns counts genuine user prompts in history. Tool results are also
// recorded with the user role, so a message whose parts are all function
// responses is not a turn.
func countUserTurns(h []provider.Message) int {
	n := 0
	for _, m := range h {
		if m.Role != provider.RoleUser {
			continue
		}
		for _, p := range m.Parts {
			if p.FunctionResponse == nil {
				n++
				break
			}
		}
	}
	return n
}

// SessionSummary returns the current session title ("" when untitled), used by
// the startup rename-nudge hint to know whether a content-bearing session has
// a name yet.
func (r *Runner) SessionSummary() string {
	if r.sessionRecorder != nil {
		return r.sessionRecorder.Summary()
	}
	return ""
}

// ForkSession copies the current conversation into a new session and switches
// the recorder onto it, returning the new session id and file path. The forked
// session carries the same history, the current summary with a " (fork)"
// suffix, the recorded branch, and any session grants. Fork-from-a-chosen-
// message is deferred; this is end-of-conversation only.
//
// The recorder is retargeted via Recorder.RotateTo(newID) so WriteHistory's
// metadata header stays the earliest line: the loader's latest-$set-wins merge
// means RotateTo's header (written after WriteHistory's) overrides only the
// header fields it carries (StartTime etc.), preserving the forked Summary.
func (r *Runner) ForkSession() (newSessionID, path string, err error) {
	if r.sessionRecorder == nil {
		return "", "", fmt.Errorf("no session recorder active")
	}
	history := r.History()
	if len(history) == 0 {
		return "", "", fmt.Errorf("no conversation to fork yet — send a message first")
	}
	workDir := r.WorkDir()
	if strings.TrimSpace(workDir) == "" {
		return "", "", fmt.Errorf("no workspace available")
	}
	chatsDir, err := session.ChatsDir(workDir)
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(chatsDir, 0o700); err != nil {
		return "", "", fmt.Errorf("create chats dir: %w", err)
	}

	newID := session.NewSessionID()
	summary := r.sessionRecorder.Summary()
	if strings.TrimSpace(summary) != "" {
		summary += " (fork)"
	}
	filename := session.FilenameForSessionID(newID)
	path = filepath.Join(chatsDir, filename)
	if err := session.WriteHistory(path, newID, "", summary, history, r.SessionGrants()); err != nil {
		return "", "", err
	}
	// Retarget the recorder onto the new file; subsequent turns append there.
	r.sessionRecorder.RotateToFile(newID, filename)
	// Re-assert the forked summary and branch via $set lines so they survive
	// regardless of the recorder's fresh header.
	if summary != "" {
		_ = r.sessionRecorder.SetSummary(summary)
	}
	if r.lastBranch != "" {
		_ = r.sessionRecorder.SetBranch(r.lastBranch)
	}
	return newID, path, nil
}

// RenameSession sets the current session's title in its JSONL metadata.
// The caller (slash handler) sanitizes the title before invoking this.
func (r *Runner) RenameSession(title string) error {
	if r.sessionRecorder == nil {
		return fmt.Errorf("no session recorder active")
	}
	return r.sessionRecorder.SetSummary(title)
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

func (r *Runner) agentKind() string {
	if r.sessionRecorder != nil {
		return r.sessionRecorder.Kind()
	}
	return "main"
}
