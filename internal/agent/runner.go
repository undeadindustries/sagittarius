package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/undeadindustries/sagittarius/internal/contextmgmt"
	"github.com/undeadindustries/sagittarius/internal/provider"
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
}

// Runner orchestrates conversation history and provider streaming for the agent loop.
type Runner struct {
	genMu         sync.RWMutex
	gen           provider.ContentGenerator
	genErr        error
	model         string
	system        string
	approval      ApprovalMode
	interactive   bool
	workDir       string
	registry      *tools.Registry
	scheduler     *tools.Scheduler
	history       []provider.Message
	ctxMgr        *contextmgmt.Manager
	turnCounter   int
	state         State
	stateMu       sync.RWMutex
	lastRequest   *provider.GenerateRequest
	lastRequestMu sync.RWMutex
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

	return &Runner{
		gen:         cfg.Generator,
		model:       cfg.Model,
		system:      system,
		approval:    mode,
		interactive: cfg.Interactive,
		workDir:     ws.Root(),
		registry:    registry,
		scheduler:   scheduler,
		ctxMgr:      cfg.ContextManager,
		state:       StateIdle,
	}, nil
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
	return r.model
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

		responses, err := r.scheduler.Execute(ctx, toolCalls, emit)
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
	if r.ctxMgr == nil {
		return
	}
	prepared, err := r.ctxMgr.PrepareTurn(ctx, r.history, r.turnCounter)
	r.turnCounter++
	if prepared != nil {
		r.history = prepared
	}
	if err != nil {
		// PrepareTurn already logged; proceed with the (best-effort) history.
		return
	}
}

func (r *Runner) buildGenerateRequest() *provider.GenerateRequest {
	return &provider.GenerateRequest{
		Model:             r.model,
		SystemInstruction: r.system,
		Messages:          append([]provider.Message(nil), r.history...),
		Tools:             r.registry.ListDeclarations(),
	}
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
}

func (r *Runner) setState(state State) {
	r.stateMu.Lock()
	r.state = state
	r.stateMu.Unlock()
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
