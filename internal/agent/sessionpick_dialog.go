package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/undeadindustries/sagittarius/internal/session"
	"github.com/undeadindustries/sagittarius/internal/ui/sessionpickdialog"
)

// SessionPickDialogDeps returns the adapter the /chat resume switcher loads its
// menu from. The menu reads the same sessions, checkpoints, and liveness the
// /chat resume command uses, so a picked row resolves identically when typed.
func (a *App) SessionPickDialogDeps() sessionpickdialog.Deps {
	return &sessionPickDialogDeps{app: a}
}

type sessionPickDialogDeps struct {
	app *App
}

// Load gathers the menu's data in one pass: sessions with their liveness,
// checkpoint tags, and the session being recorded. It runs off the UI goroutine
// (the dialog starts it as a command), so the liveness probes may block briefly.
func (d *sessionPickDialogDeps) Load(ctx context.Context) (sessionpickdialog.Catalog, error) {
	_ = ctx
	if d.app == nil || d.app.runner == nil {
		return sessionpickdialog.Catalog{}, fmt.Errorf("runner not available")
	}
	h := &appHooks{app: d.app}
	infos, err := h.ListSessions()
	if err != nil {
		return sessionpickdialog.Catalog{}, err
	}
	tags, err := h.ListCheckpoints()
	if err != nil {
		return sessionpickdialog.Catalog{}, err
	}
	// The runner's own registry dir, the same one SwitchSession probes, so the
	// menu's Live marker and the switch refusal can never disagree.
	livenessDir := d.app.runner.livenessDir

	currentID := h.currentSessionID()
	cat := sessionpickdialog.Catalog{CurrentID: currentID, Checkpoints: tags}
	for _, info := range infos {
		cat.Sessions = append(cat.Sessions, sessionEntryFromInfo(info, livenessDir, currentID))
	}
	return cat, nil
}

// sessionEntryFromInfo converts a listed session into a menu row. The current
// session is never probed: this process holds its lock, so a probe would always
// report it live and say nothing new. Only a definite ProbeLive marks a row as
// open elsewhere; an unverifiable row stays selectable and the switch itself
// refuses it with the reason.
func sessionEntryFromInfo(info session.SessionInfo, livenessDir, currentID string) sessionpickdialog.SessionEntry {
	entry := sessionpickdialog.SessionEntry{
		Index:        info.Index,
		ID:           info.ID,
		Title:        info.DisplayName,
		MessageCount: info.MessageCount,
		Branch:       info.Branch,
	}
	if t, err := time.Parse(time.RFC3339Nano, info.LastUpdated); err == nil {
		entry.LastUpdated = t
	}
	if info.ID != currentID && livenessDir != "" {
		if state, _ := session.Probe(livenessDir, info.ID); state == session.ProbeLive {
			entry.Live = true
		}
	}
	return entry
}
