package agent

import (
	"errors"

	"github.com/undeadindustries/sagittarius/internal/grill"
)

// ErrNoGrillSession is returned by UpdateGrill when no grill session is active.
var ErrNoGrillSession = errors.New("no active grill session")

// errGrillNoop lets an UpdateGrill mutator signal "nothing changed" so the
// update skips persistence and prompt recomposition. It is never surfaced.
var errGrillNoop = errors.New("grill: no change")

// Grill returns a deep copy of the active grill-me session, or nil when none is
// running. The copy means callers never share the runner's live pointer, so
// reading its fields after the lock is released cannot race a concurrent
// UpdateGrill (which mutates the live session in place under the write lock).
func (r *Runner) Grill() *grill.Session {
	r.grillMu.RLock()
	defer r.grillMu.RUnlock()
	return r.activeGrill.Clone()
}

// SetGrill replaces the active grill-me session (nil clears it), persists the
// change to the session recorder, and recomposes the system instruction so the
// interrogation directive reflects the new state. A clone is stored so the
// caller cannot mutate runner state through the passed pointer afterwards. The
// scheduler's read-only gate (tools.WithReadOnlyGate) reads the session live
// via Grill(), so it does not need rebuilding here.
func (r *Runner) SetGrill(g *grill.Session) {
	clone := g.Clone()
	r.grillMu.Lock()
	r.activeGrill = clone
	snap := r.activeGrill.ToSnapshot()
	r.grillMu.Unlock()
	if r.sessionRecorder != nil {
		_ = r.sessionRecorder.SetGrill(snap)
	}
	r.applyModeSystemSuffix()
}

// UpdateGrill atomically applies fn to the live grill session under the write
// lock, then (if fn made a change) persists the snapshot and recomposes the
// system instruction. This is the race-safe way to mutate individual fields of
// an in-flight session: it serializes with concurrent-safe control commands
// (e.g. /grill pause running on a background goroutine) and with recordDecision
// mid-turn, so neither loses the other's write. fn must not retain the pointer.
// Returns ErrNoGrillSession when no session is active, or any error fn returns
// (fn may return errGrillNoop to abort persistence when it changed nothing).
func (r *Runner) UpdateGrill(fn func(*grill.Session) error) error {
	r.grillMu.Lock()
	g := r.activeGrill
	if g == nil {
		r.grillMu.Unlock()
		return ErrNoGrillSession
	}
	if err := fn(g); err != nil {
		r.grillMu.Unlock()
		if errors.Is(err, errGrillNoop) {
			return nil
		}
		return err
	}
	snap := g.ToSnapshot()
	r.grillMu.Unlock()
	if r.sessionRecorder != nil {
		_ = r.sessionRecorder.SetGrill(snap)
	}
	r.applyModeSystemSuffix()
	return nil
}
