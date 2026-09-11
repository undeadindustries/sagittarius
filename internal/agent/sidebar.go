package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/contextmgmt"
	"github.com/undeadindustries/sagittarius/internal/modes"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/session"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

const (
	sessionKindSidebar   = "sidebar"
	sidebarMaxToolRounds = 6
	sidebarCancelWait    = 2 * time.Second
	sidebarCharter       = "The main agent is waiting on a condition. Answer the user's question with read-only tools if you need them. You cannot cancel, shorten, or replace the wait. Do not call wait_until."
)

var errSidebarBusy = errors.New("a question is already being answered")

type sidebarExchange struct {
	question string
	answer   string
}

func (r *Runner) AskSidebar(ctx context.Context, question string) (<-chan ui.StreamEvent, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return nil, fmt.Errorf("sidebar question is empty")
	}
	if r.isSubagent() {
		return nil, fmt.Errorf("sidebar questions are not available inside a subagent")
	}

	childCtx, cancel := context.WithCancel(ctx)
	r.sidebarMu.Lock()
	if r.sidebarCancel != nil {
		r.sidebarMu.Unlock()
		cancel()
		return nil, errSidebarBusy
	}
	r.sidebarCancel = cancel
	r.sidebarWg.Add(1)
	r.sidebarMu.Unlock()

	out := make(chan ui.StreamEvent, 16)
	go func() {
		defer r.sidebarWg.Done()
		defer cancel()
		defer close(out)
		defer func() {
			r.sidebarMu.Lock()
			r.sidebarCancel = nil
			r.sidebarMu.Unlock()
		}()

		answer, err := r.runSidebar(childCtx, question, out)
		if err != nil && !errors.Is(err, context.Canceled) {
			select {
			case out <- ui.StreamEvent{Type: ui.StreamError, Err: err}:
			case <-childCtx.Done():
			}
			return
		}
		if strings.TrimSpace(answer) != "" || err == nil {
			r.bufferSidebar(question, answer)
		}
		select {
		case out <- ui.StreamEvent{Type: ui.StreamDone}:
		case <-childCtx.Done():
		}
	}()
	return out, nil
}

func (r *Runner) runSidebar(ctx context.Context, question string, out chan<- ui.StreamEvent) (string, error) {
	sub, err := r.newSidebarRunner(ctx)
	if err != nil {
		return "", err
	}
	defer func() {
		if closeErr := sub.Close(); closeErr != nil {
			slog.Debug("sidebar: close", "err", closeErr)
		}
	}()

	stream, err := sub.RunTurn(ctx, question)
	if err != nil {
		return "", fmt.Errorf("sidebar turn: %w", err)
	}
	var answer strings.Builder
	for {
		select {
		case <-ctx.Done():
			return answer.String(), ctx.Err()
		case ev, ok := <-stream:
			if !ok {
				return answer.String(), nil
			}
			if ev.Type == ui.StreamTextDelta {
				answer.WriteString(ev.Text)
			}
			if ev.Type == ui.StreamError && ev.Err != nil {
				return answer.String(), ev.Err
			}
			if ev.Type == ui.StreamDone {
				continue
			}
			select {
			case out <- ev:
			case <-ctx.Done():
				return answer.String(), ctx.Err()
			}
		}
	}
}

func (r *Runner) newSidebarRunner(ctx context.Context) (*Runner, error) {
	gen, err := r.newSubagentGenerator(ctx, r.settingsSnapshot())
	if err != nil {
		return nil, fmt.Errorf("sidebar generator: %w", err)
	}

	subID := uuid.New().String()
	root := r.workspace.Root()
	outputDir, _ := session.ChatsDir(root)
	ctxMgr := contextmgmt.NewManager(contextmgmt.ManagerConfig{
		Enabled:   true,
		SessionID: subID,
		OutputDir: outputDir,
	})

	var rec *session.Recorder
	if r.sessionRecorder != nil {
		if chatsDir, err := session.ChatsDir(root); err == nil {
			rec = session.NewRecorder(chatsDir, subID, session.ProjectHash(root), sessionKindSidebar)
		}
	}

	settings := r.settingsSnapshot()
	rounds := sidebarMaxToolRounds
	child, err := NewRunner(RunnerConfig{
		Generator:             gen,
		Model:                 r.Model(),
		ModelPinned:           true,
		WorkDir:               root,
		ApprovalMode:          ApprovalYolo,
		Interactive:           false,
		ContextManager:        ctxMgr,
		SessionRecorder:       rec,
		Settings:              settings,
		ProjectBoundary:       r.projectBoundary,
		InitialMode:           modes.ModeAsk,
		Runtime:               r.runtime,
		SpillDir:              r.spillDir,
		ScriptToolEnabled:     config.ScriptToolEnabled(settings, nil),
		MaxToolRoundsOverride: &rounds,
		OmitSessionTools:      true,
		InitialHistory:        r.historyForSidebar(),
		AgentID:               subID,
		FileState:             r.fileState,
		SubagentCharter:       sidebarCharter,
		SubagentGenerator:     r.newSubagentGenerator,
	})
	if err != nil {
		return nil, fmt.Errorf("sidebar runner: %w", err)
	}
	return child, nil
}

func (r *Runner) historyForSidebar() []provider.Message {
	r.historyMu.RLock()
	defer r.historyMu.RUnlock()
	return stripUnpairedTrailingToolCalls(append([]provider.Message(nil), r.history...))
}

func stripUnpairedTrailingToolCalls(hist []provider.Message) []provider.Message {
	if len(hist) == 0 {
		return hist
	}
	last := hist[len(hist)-1]
	if last.Role == provider.RoleModel && messageHasToolCalls(last) {
		return hist[:len(hist)-1]
	}
	return hist
}

func messageHasToolCalls(msg provider.Message) bool {
	for _, p := range msg.Parts {
		if p.FunctionCall != nil {
			return true
		}
	}
	return false
}

func (r *Runner) bufferSidebar(question, answer string) {
	r.sidebarMu.Lock()
	defer r.sidebarMu.Unlock()
	r.pendingSidebar = append(r.pendingSidebar, sidebarExchange{
		question: question,
		answer:   answer,
	})
}

func (r *Runner) flushPendingSidebar() {
	r.sidebarMu.Lock()
	pending := r.pendingSidebar
	r.pendingSidebar = nil
	r.sidebarMu.Unlock()
	if len(pending) == 0 {
		return
	}

	r.historyMu.Lock()
	for _, ex := range pending {
		r.history = append(r.history, provider.Message{
			Role:  provider.RoleUser,
			Parts: []provider.Part{{Text: ex.question}},
		})
		if strings.TrimSpace(ex.answer) != "" {
			r.history = append(r.history, provider.Message{
				Role:  provider.RoleModel,
				Parts: []provider.Part{{Text: ex.answer}},
			})
		}
	}
	r.historyMu.Unlock()

	if r.sessionRecorder == nil {
		return
	}
	for _, ex := range pending {
		r.sessionRecorder.RecordUserMessage(ex.question)
		if strings.TrimSpace(ex.answer) != "" {
			r.sessionRecorder.RecordModelMessage(ex.answer, nil)
		}
	}
}

func (r *Runner) cancelSidebar(wait time.Duration) {
	r.sidebarMu.Lock()
	cancel := r.sidebarCancel
	r.sidebarMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if wait <= 0 {
		return
	}
	done := make(chan struct{})
	go func() {
		r.sidebarWg.Wait()
		close(done)
	}()
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		slog.Warn("sidebar: still running after cancel wait")
	}
}

// used by tests to inspect buffered-but-not-yet-flushed exchanges
func (r *Runner) pendingSidebarCount() int {
	r.sidebarMu.Lock()
	defer r.sidebarMu.Unlock()
	return len(r.pendingSidebar)
}
