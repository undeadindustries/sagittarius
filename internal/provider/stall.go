package provider

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrConnectTimeout and ErrFirstTokenTimeout classify a StallError so callers
// can tell a stalled provider from a user cancel with errors.Is.
var (
	ErrConnectTimeout    = errors.New("provider connect timeout")
	ErrFirstTokenTimeout = errors.New("provider first-token timeout")
)

// StallError reports that a provider stopped making progress before the
// model produced anything. It names the setting that controls the budget so
// a user on a slow local server knows what to raise.
type StallError struct {
	Kind       error
	ProviderID string
	After      time.Duration
}

func (e *StallError) Error() string {
	id := e.ProviderID
	if id == "" {
		id = "<id>"
	}
	if errors.Is(e.Kind, ErrConnectTimeout) {
		return fmt.Sprintf("provider %s did not respond within %s (raise providers.%s.connectTimeout, or set it to 0 to disable)",
			displayProvider(e.ProviderID), e.After, id)
	}
	return fmt.Sprintf("no output from %s after %s (raise providers.%s.firstTokenTimeout, or set it to 0 to disable)",
		displayProvider(e.ProviderID), e.After, id)
}

func (e *StallError) Unwrap() error { return e.Kind }

func displayProvider(id string) string {
	if id == "" {
		return "the provider"
	}
	return id
}

type stallStage int

const (
	stallIdle stallStage = iota
	stallConnecting
	stallAwaitingOutput
	stallDone
)

// stallGuard bounds the two waits a streaming request can hang in without
// the whole-stream timeout noticing: waiting for response headers (connect)
// and, after headers, waiting for the first model output. Prefill of a long
// prompt on local hardware legitimately takes minutes, so both budgets are
// configurable and a zero budget disables that stage. Once output starts the
// guard is inert: a long generation is never cut off by it.
type stallGuard struct {
	ctx        context.Context
	cancel     context.CancelCauseFunc
	providerID string
	connect    time.Duration
	firstToken time.Duration

	mu    sync.Mutex
	stage stallStage
	timer *time.Timer
}

// newStallGuard derives the guarded context from parent. Callers must use
// guard.ctx for the request and call stop when the request ends.
func newStallGuard(parent context.Context, providerID string, connect, firstToken time.Duration) *stallGuard {
	ctx, cancel := context.WithCancelCause(parent)
	return &stallGuard{ctx: ctx, cancel: cancel, providerID: providerID, connect: connect, firstToken: firstToken}
}

// armConnect starts the connect budget. Call it just before sending.
func (g *stallGuard) armConnect() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.stage != stallIdle {
		return
	}
	g.stage = stallConnecting
	g.startLocked(g.connect, ErrConnectTimeout)
}

// headersReceived ends the connect budget and starts the first-output budget.
func (g *stallGuard) headersReceived() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.stage >= stallAwaitingOutput {
		return
	}
	g.stopLocked()
	g.stage = stallAwaitingOutput
	g.startLocked(g.firstToken, ErrFirstTokenTimeout)
}

// firstOutput disarms the guard for the rest of the request.
func (g *stallGuard) firstOutput() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.stage == stallDone {
		return
	}
	g.stopLocked()
	g.stage = stallDone
}

// stop disarms the guard and releases its context.
func (g *stallGuard) stop() {
	g.firstOutput()
	g.cancel(nil)
}

// classify returns the StallError when the guard fired, otherwise err. It
// checks even when err is nil: a parser that stops on a canceled send returns
// cleanly, and that must not read as a finished reply. A user cancel of the
// parent context is left as-is.
func (g *stallGuard) classify(err error) error {
	var stall *StallError
	if errors.As(context.Cause(g.ctx), &stall) {
		return stall
	}
	return err
}

func (g *stallGuard) startLocked(budget time.Duration, kind error) {
	if budget <= 0 {
		return
	}
	stall := &StallError{Kind: kind, ProviderID: g.providerID, After: budget}
	g.timer = time.AfterFunc(budget, func() { g.cancel(stall) })
}

func (g *stallGuard) stopLocked() {
	if g.timer != nil {
		g.timer.Stop()
		g.timer = nil
	}
}
