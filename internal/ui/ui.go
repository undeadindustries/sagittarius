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
	// Notice is an optional startup message shown in the scrollback (e.g. a
	// missing-API-key warning) so interactive sessions can open and recover.
	Notice string
	// ThemeName selects the color theme ("default" or "greyscale"). Empty means
	// the default purple theme. Resolved in the bubbletea layer via the
	// internal/ui/theme package; the agent layer never inspects it.
	ThemeName string
	// NoColor forces the greyscale theme regardless of ThemeName (NO_COLOR env).
	NoColor bool
	// HideBanner suppresses the ASCII launch logo (settings ui.hideBanner).
	HideBanner bool
	// HideTips suppresses the welcome tips block (settings ui.hideTips).
	HideTips bool
	// ShowThinking seeds the live thinking-box visibility from the resolved
	// startup setting (ui.showThinking / per-provider override). Ctrl+T toggles
	// it during the session.
	ShowThinking bool
	// NeedsOnboarding opens the first-run provider setup overlay before the
	// first chat turn when no provider or API key is configured.
	NeedsOnboarding bool
	// LoadedMemoryFiles are the AGENTS.md paths that contributed to the system
	// instruction; shown on the welcome banner so the user knows what context
	// was loaded. Empty omits the line.
	LoadedMemoryFiles []string
	// InitialScrollback seeds the TUI with a restored conversation on startup.
	// Used by --resume so the user can see the prior turns, not just have them
	// silently loaded into the model's context.
	InitialScrollback []ScrollbackEntry
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
