// Package bubbletea implements ui.UI using Charm Bracelet Bubble Tea.
package bubbletea

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

// Terminal implements ui.UI with Bubble Tea. Construct via NewTerminal.
type Terminal struct {
	opts ui.Options

	mu      sync.Mutex
	running bool
	program *tea.Program
	model   *model
}

// NewTerminal returns a Bubble Tea backed ui.UI.
func NewTerminal(opts ui.Options) *Terminal {
	if opts.BannerTitle == "" {
		opts.BannerTitle = "Sagittarius"
	}
	return &Terminal{opts: opts}
}

// Run starts the interactive session and blocks until quit or ctx cancellation.
func (t *Terminal) Run(ctx context.Context, app ui.App) error {
	if app == nil {
		return errors.New("bubbletea run: app is required")
	}

	t.mu.Lock()
	if t.running {
		t.mu.Unlock()
		return errors.New("bubbletea run: already running")
	}
	t.running = true
	t.mu.Unlock()

	defer func() {
		t.mu.Lock()
		t.running = false
		t.program = nil
		t.model = nil
		t.mu.Unlock()
	}()

	m := newModel(t.opts, app, t)
	m.ctx = ctx
	t.mu.Lock()
	t.model = m
	t.mu.Unlock()

	var progOpts []tea.ProgramOption
	progOpts = append(progOpts, tea.WithContext(ctx))
	if t.opts.Headless {
		progOpts = append(progOpts, tea.WithoutRenderer(), tea.WithInput(strings.NewReader("")))
	} else if !t.opts.ScreenReader {
		// Alt-screen hides the terminal's native scrollback. Mouse reporting is
		// left OFF by default so the terminal's native click-drag text selection
		// keeps working (users copy/paste from the transcript); the conversation
		// viewport is driven by keyboard scroll (PgUp/PgDn/Shift+arrows). Mouse
		// wheel scrolling is opt-in via Alt+M or the /mouse command, which sends
		// tea.EnableMouseCellMotion at runtime.
		progOpts = append(progOpts, tea.WithAltScreen())
	}

	p := tea.NewProgram(m, progOpts...)
	t.mu.Lock()
	t.program = p
	t.mu.Unlock()

	_, err := p.Run()

	// Print the goodbye summary to the restored (normal) screen after the
	// alt-screen program tears down, so it persists in the user's scrollback.
	if !t.opts.Headless && m.exitSummary != "" {
		fmt.Print("\n" + m.exitSummary)
	}

	if err != nil {
		return fmt.Errorf("bubbletea run: %w", err)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}

// RenderStream sends a stream event to the active program.
func (t *Terminal) RenderStream(delta ui.StreamEvent) error {
	t.mu.Lock()
	p := t.program
	m := t.model
	t.mu.Unlock()
	if p == nil {
		return ui.ErrNotRunning
	}
	gen := uint64(0)
	if m != nil {
		gen = m.activeStreamGen
	}
	p.Send(streamEventMsg{gen: gen, event: delta})
	return nil
}

// SetStatus updates the footer via the active program.
func (t *Terminal) SetStatus(status ui.StatusBar) error {
	t.mu.Lock()
	p := t.program
	t.mu.Unlock()
	if p == nil {
		return ui.ErrNotRunning
	}
	p.Send(statusMsg{status: status})
	return nil
}

// PromptInput is not used while Run owns the input loop (Phase 07 may wire this).
func (t *Terminal) PromptInput() (string, error) {
	return "", ui.ErrNotRunning
}

// ShowError displays an error banner in the scrollback area.
func (t *Terminal) ShowError(err error) error {
	if err == nil {
		return nil
	}
	return t.RenderStream(ui.StreamEvent{Type: ui.StreamError, Err: err})
}
