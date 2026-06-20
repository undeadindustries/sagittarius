package ui

import (
	"context"
	"errors"
)

// ErrNotRunning indicates a UI method was called while Run is not active.
var ErrNotRunning = errors.New("ui: not running")

// Options configures terminal UI behavior.
type Options struct {
	// ScreenReader disables animations and alt-screen usage (fork --screen-reader stub).
	ScreenReader bool
	// Headless runs without a TTY renderer (tests and CI only).
	Headless bool
	// BannerTitle is shown in the header (default "Sagittarius").
	BannerTitle string
	// Version is shown beside the banner (e.g. from internal/version).
	Version string
	// InitialStatus seeds the footer before the first turn (Phase 07 agent loop).
	InitialStatus StatusBar
}

// UI is the library-agnostic terminal interface. Agent code must depend on this
// interface only — not on Bubble Tea or other TUI libraries.
type UI interface {
	// Run starts the interactive session and blocks until quit or ctx cancel.
	Run(ctx context.Context, app App) error

	// RenderStream appends a streaming event to the scrollback viewport.
	RenderStream(delta StreamEvent) error

	// SetStatus updates the footer status bar.
	SetStatus(status StatusBar) error

	// PromptInput blocks for the next user line (used when an external driver
	// owns the loop; the default Run path collects input internally).
	PromptInput() (string, error)

	// ShowError displays a user-visible error without terminating the session.
	ShowError(err error) error
}
