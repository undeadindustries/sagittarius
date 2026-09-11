package hub

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

// DefaultQueueCap is the maximum number of queued turns in front of HandleInput.
const DefaultQueueCap = 5

// ErrQueueFull is returned when the inbound turn queue exceeds capacity.
var ErrQueueFull = errors.New("input queue is full (max 5 queued messages). Please wait for the current turn to complete")

// Child represents a registered child UI renderer with an optional attribution prefix.
type Child struct {
	UI          ui.UI
	Attribution string // e.g. "(via Google Chat) " or "(via Terminal) "
}

// Hub is a composite ui.UI that fans events to multiple child renderers,
// serializes user input through a bounded queue to prevent overlapping runner turns,
// and cross-echoes turns to children with attribution without double-rendering.
type Hub struct {
	mu       sync.RWMutex
	children []Child
	realApp  ui.App
	queue    chan *queuedTurn
	queueCap int

	workerCtx    context.Context
	workerCancel context.CancelFunc
	workerDone   chan struct{}
}

type queuedTurn struct {
	ctx         context.Context
	input       string
	originator  ui.UI
	attribution string
	eventCh     chan ui.StreamEvent
	errCh       chan error
}

// New constructs a Hub with the given child UIs.
func New(children ...Child) *Hub {
	ctx, cancel := context.WithCancel(context.Background())
	h := &Hub{
		children:     children,
		queueCap:     DefaultQueueCap,
		queue:        make(chan *queuedTurn, DefaultQueueCap),
		workerCtx:    ctx,
		workerCancel: cancel,
		workerDone:   make(chan struct{}),
	}
	go h.queueWorker()
	return h
}

// AddChild registers an additional child UI and its attribution prefix.
func (h *Hub) AddChild(child ui.UI, attribution string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.children = append(h.children, Child{
		UI:          child,
		Attribution: attribution,
	})
}

// Close stops the background worker.
func (h *Hub) Close() {
	h.workerCancel()
	<-h.workerDone
}

// Run starts all child UIs concurrently and blocks until the first one quits or ctx cancels.
func (h *Hub) Run(ctx context.Context, app ui.App) error {
	h.mu.Lock()
	h.realApp = app
	children := append([]Child(nil), h.children...)
	h.mu.Unlock()

	if len(children) == 0 {
		return nil
	}

	childCtx, childCancel := context.WithCancel(ctx)
	defer childCancel()

	errCh := make(chan error, len(children))
	for _, c := range children {
		child := c
		childApp := h.AppFor(child.UI)
		go func() {
			err := child.UI.Run(childCtx, childApp)
			errCh <- err
			childCancel() // Stop siblings when one exits
		}()
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

// RenderStream fans out a streaming event to all registered children.
func (h *Hub) RenderStream(delta ui.StreamEvent) error {
	h.mu.RLock()
	children := append([]Child(nil), h.children...)
	h.mu.RUnlock()

	var firstErr error
	for _, c := range children {
		if err := c.UI.RenderStream(delta); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// SetStatus fans out a status bar update to all registered children.
func (h *Hub) SetStatus(status ui.StatusBar) error {
	h.mu.RLock()
	children := append([]Child(nil), h.children...)
	h.mu.RUnlock()

	var firstErr error
	for _, c := range children {
		if err := c.UI.SetStatus(status); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// ShowError displays an error to all registered children.
func (h *Hub) ShowError(err error) error {
	h.mu.RLock()
	children := append([]Child(nil), h.children...)
	h.mu.RUnlock()

	var firstErr error
	for _, c := range children {
		if sErr := c.UI.ShowError(err); sErr != nil && firstErr == nil {
			firstErr = sErr
		}
	}
	return firstErr
}

// PromptInput returns ErrNotRunning as Hub does not directly prompt.
func (h *Hub) PromptInput() (string, error) {
	return "", ui.ErrNotRunning
}

// AppFor returns a ui.App adapter scoped to a specific child UI.
// User input submitted through this adapter:
// 1. Passes through the serialized input queue.
// 2. Cross-echoes to every other child with attribution as StreamScrollback.
// 3. Streams agent events back to the originator and tees them to all other children.
func (h *Hub) AppFor(originator ui.UI) ui.App {
	h.mu.RLock()
	attribution := ""
	for _, c := range h.children {
		if c.UI == originator {
			attribution = c.Attribution
			break
		}
	}
	h.mu.RUnlock()

	return &childAppProxy{
		hub:         h,
		originator:  originator,
		attribution: attribution,
	}
}

type childAppProxy struct {
	hub         *Hub
	originator  ui.UI
	attribution string
}

func (p *childAppProxy) HandleInput(ctx context.Context, input string) (<-chan ui.StreamEvent, error) {
	return p.hub.submitInput(ctx, input, p.originator, p.attribution)
}

func (h *Hub) submitInput(ctx context.Context, input string, originator ui.UI, attribution string) (<-chan ui.StreamEvent, error) {
	h.mu.RLock()
	app := h.realApp
	h.mu.RUnlock()

	if app == nil {
		return nil, ui.ErrNotRunning
	}

	turn := &queuedTurn{
		ctx:         ctx,
		input:       input,
		originator:  originator,
		attribution: attribution,
		eventCh:     make(chan ui.StreamEvent, 100),
		errCh:       make(chan error, 1),
	}

	select {
	case h.queue <- turn:
	default:
		return nil, ErrQueueFull
	}

	// Wait for turn to be picked up by worker and started or rejected
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-turn.errCh:
		if err != nil {
			return nil, err
		}
		return turn.eventCh, nil
	}
}

func (h *Hub) queueWorker() {
	defer close(h.workerDone)

	for {
		select {
		case <-h.workerCtx.Done():
			return
		case turn := <-h.queue:
			// If turn context was already cancelled while in queue, skip it
			if turn.ctx.Err() != nil {
				turn.errCh <- turn.ctx.Err()
				continue
			}

			h.executeTurn(turn)
		}
	}
}

func (h *Hub) executeTurn(turn *queuedTurn) {
	h.mu.RLock()
	app := h.realApp
	children := append([]Child(nil), h.children...)
	h.mu.RUnlock()

	if app == nil {
		turn.errCh <- ui.ErrNotRunning
		return
	}

	// 1. Cross-echo originating user turn to every other child
	echoText := turn.input
	if turn.attribution != "" {
		echoText = fmt.Sprintf("%s%s", turn.attribution, turn.input)
	}
	echoEvent := ui.StreamEvent{
		Type:           ui.StreamScrollback,
		ScrollbackRole: ui.ScrollbackUser,
		Text:           echoText,
	}
	for _, c := range children {
		if c.UI != turn.originator {
			_ = c.UI.RenderStream(echoEvent)
		}
	}

	// 2. Call real App.HandleInput
	streamCh, err := app.HandleInput(turn.ctx, turn.input)
	if err != nil {
		turn.errCh <- err
		return
	}

	// Turn accepted and started
	turn.errCh <- nil

	// 3. Drain streamCh, send to originator via turn.eventCh, and tee to other children
	for ev := range streamCh {
		turn.eventCh <- ev

		for _, c := range children {
			if c.UI != turn.originator {
				_ = c.UI.RenderStream(ev)
			}
		}
	}
	close(turn.eventCh)
}
