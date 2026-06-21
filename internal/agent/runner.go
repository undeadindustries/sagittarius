package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/contextmgmt"
	"github.com/undeadindustries/sagittarius/internal/modes"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/session"
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
	// Settings enables interaction-mode model routing. Nil disables mode overrides.
	Settings *config.Settings
	// InitialMode seeds the session interaction mode. The zero value
	// (modes.ModeAgent) is authoritative, not "unset": callers that want the
	// settings default must resolve it via modes.DefaultFromSettings and pass
	// the result (cmd/sagittarius does this). This keeps an explicit ModeAgent
	// from being silently overridden by sagittarius.defaultMode.
	InitialMode modes.Mode
	// ModelPinned skips mode-based routing when true (CLI -m override).
	ModelPinned bool
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
	system               string
	systemBase           string
	approval             ApprovalMode
	interactive          bool
	workDir              string
	regMu                sync.RWMutex
	registry             *tools.Registry
	scheduler            *tools.Scheduler
	history              []provider.Message
	ctxMgrMu             sync.RWMutex
	ctxMgr               *contextmgmt.Manager
	turnCounter          int
	state                State
	stateMu              sync.RWMutex
	lastRequest          *provider.GenerateRequest
	lastRequestMu        sync.RWMutex
	sessionRecorder      *session.Recorder
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

	system, err := DiscoverSystemInstruction(workDir)
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
	registry := tools.NewBuiltinRegistry(ws)
	policy := approvalToPolicy(mode)
	scheduler := tools.NewScheduler(registry, policy, cfg.Interactive)

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
		systemBase:           system,
		system:               system,
		approval:             mode,
		interactive:          cfg.Interactive,
		workDir:              ws.Root(),
		registry:             registry,
		scheduler:            scheduler,
		ctxMgr:               cfg.ContextManager,
		state:                StateIdle,
		sessionRecorder:      cfg.SessionRecorder,
		history:              history,
	}
	if !cfg.ModelPinned {
		runner.refreshModelFromMode()
		runner.applyModeSystemSuffix()
	}
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

// Model returns the configured model id for this runner.
func (r *Runner) Model() string {
	r.modelMu.RLock()
	defer r.modelMu.RUnlock()
	return r.model
}

// ModelPinned reports whether CLI or explicit pinning bypasses mode routing.
func (r *Runner) ModelPinned() bool {
	return r.modelPinned
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
		r.applyModeSystemSuffix()
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
		r.applyModeSystemSuffix()
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

	r.setState(StateIdle)
	r.history = append(r.history, provider.Message{
		Role:  provider.RoleUser,
		Parts: []provider.Part{{Text: userInput}},
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
	defer close(out)

	gen, err := r.generator()
	if err != nil {
		out <- ui.StreamEvent{Type: ui.StreamError, Err: err}
		return
	}

	r.setState(StateStreaming)

	for round := 0; round < tools.MaxToolRounds; round++ {
		r.prepareContext(ctx)
		if !r.modelPinned {
			r.refreshModelFromMode()
		}
		req := r.buildGenerateRequest()
		r.storeLastRequest(req)

		respCh, err := gen.GenerateContentStream(ctx, req)
		if err != nil {
			out <- ui.StreamEvent{Type: ui.StreamError, Err: err}
			return
		}

		toolCalls, modelText, streamErr := r.consumeStream(ctx, respCh, out)
		if streamErr != nil {
			return
		}

		r.appendModelMessage(modelText, toolCalls)

		if len(toolCalls) == 0 {
			r.setState(StateDone)
			out <- ui.StreamEvent{Type: ui.StreamDone}
			return
		}

		r.setState(StateAwaitingTools)
		emit := func(ev ui.StreamEvent) {
			select {
			case <-ctx.Done():
			case out <- ev:
			}
		}

		responses, err := r.toolScheduler().Execute(ctx, toolCalls, emit)
		if err != nil {
			out <- ui.StreamEvent{Type: ui.StreamError, Err: err}
			return
		}
		r.appendFunctionResponses(responses)
		r.setState(StateStreaming)
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
	return &provider.GenerateRequest{
		Model:             model,
		SystemInstruction: system,
		Messages:          append([]provider.Message(nil), r.history...),
		Tools:             r.toolRegistry().ListDeclarations(),
	}
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
}

func (r *Runner) applyModeSystemSuffix() {
	suffix := modes.SystemPromptSuffix(r.InteractionMode(), r.sagittariusSettings())
	base := r.systemBase
	if suffix != "" {
		base = strings.TrimRight(base, "\n") + "\n\n" + suffix
	}
	r.system = base
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
) ([]provider.ToolCall, string, error) {
	var modelText strings.Builder
	var toolCalls []provider.ToolCall
	streamDone := false

	for !streamDone {
		select {
		case <-ctx.Done():
			out <- ui.StreamEvent{Type: ui.StreamError, Err: ctx.Err()}
			return nil, "", ctx.Err()
		case resp, ok := <-respCh:
			if !ok {
				streamDone = true
				continue
			}
			if resp.Error != nil {
				out <- ui.StreamEvent{Type: ui.StreamError, Err: resp.Error}
				return nil, "", resp.Error
			}
			if resp.TextDelta != "" {
				modelText.WriteString(resp.TextDelta)
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
					return nil, "", ctx.Err()
				case out <- ev:
				}
			}
		}
	}

	return toolCalls, modelText.String(), nil
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

// RotateSession starts a new session-recording file, abandoning the current
// one. Paired with ClearHistory by /clear so post-clear turns are recorded to a
// fresh session instead of being appended to the cleared conversation. No-op
// when session recording is disabled.
func (r *Runner) RotateSession() {
	if r.sessionRecorder != nil {
		r.sessionRecorder.Rotate()
	}
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
