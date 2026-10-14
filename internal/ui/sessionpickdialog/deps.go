// Package sessionpickdialog implements the /chat resume switcher overlay: one
// menu listing this project's sessions and saved checkpoints. Picking a row
// submits the matching /chat resume line, so the switch itself runs through the
// same slash path a typed command does. The dialog never touches the session
// store directly; loading goes through Deps so it never imports agent or slash
// (preserves the AD-004 seam).
package sessionpickdialog

import (
	"context"
	"time"
)

// SessionEntry is one resumable session in the project, as the menu shows it.
type SessionEntry struct {
	// Index is the 1-based position in /chat list, so the row reads "#N".
	Index int
	// ID is the full session id. Picking the row submits it, which is stable
	// across /chat list renumbering.
	ID string
	// Title is the session's display name (first prompt or summary).
	Title string
	// LastUpdated is when the session was last written; zero when unknown.
	LastUpdated time.Time
	// MessageCount is the number of recorded messages.
	MessageCount int
	// Branch is the git branch recorded with the session, or "".
	Branch string
	// Live reports that another running process holds the session's liveness
	// lock. Switching would put two writers on one file, so the row refuses.
	Live bool
}

// Catalog is everything the menu shows, loaded in one call so the list and the
// current-session marker can never disagree.
type Catalog struct {
	Sessions    []SessionEntry
	Checkpoints []string
	// CurrentID is the session id the runner is recording to right now.
	CurrentID string
}

// Deps supplies the menu's data. Load runs off the UI goroutine, so it may do
// file I/O and liveness probes.
type Deps interface {
	Load(ctx context.Context) (Catalog, error)
}
