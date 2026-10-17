package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/goal"
	"github.com/undeadindustries/sagittarius/internal/grill"
	"github.com/undeadindustries/sagittarius/internal/hooks"
	"github.com/undeadindustries/sagittarius/internal/session"
)

// sessionSwitchHookTimeout bounds the SessionStart hook a switch fires. It
// matches the /clear path so a slow hook cannot hold the turn guard for long.
const sessionSwitchHookTimeout = 5 * time.Second

// Refusals SwitchSession reports. Each names a state the user can act on, so the
// slash layer can show them verbatim.
var (
	// ErrSwitchDuringTurn means a turn is running. The switch refuses rather than
	// waits, because waiting would queue a state swap behind a model response.
	ErrSwitchDuringTurn = errors.New("a turn is in progress; wait for it to finish or press Esc, then switch")
	// ErrSwitchToCurrent means the target is the session already being recorded.
	ErrSwitchToCurrent = errors.New("already in this session")
	// ErrSessionLiveElsewhere means another running process holds the target's
	// liveness lock. Two writers on one JSONL would interleave lines.
	ErrSessionLiveElsewhere = errors.New("session is open in another terminal")
	// ErrSessionLockUnknown means liveness could not be proven either way (no
	// lock registry, I/O error, or a lost race). Treated as live, as in resume.
	ErrSessionLockUnknown = errors.New("cannot verify the session is idle; it may be open elsewhere")
)

// SwitchSession points the live runner at an existing session. The target's
// history and standing state (goal, grill, constraints, scratchpad, read-only
// posture) replace the current ones, and later turns append to the target's file.
// The departing session is marked clean and keeps its file intact.
//
// The turn guard is held for the whole swap, so no turn can start mid-switch.
// Snapshots are not retargeted: /undo keeps covering this process's writes,
// because the snapshot index is bound at startup, the same as after /clear.
func (r *Runner) SwitchSession(res *session.SelectionResult) error {
	if res == nil || res.Record == nil {
		return errors.New("switch session: no session selected")
	}
	if !r.turnActive.CompareAndSwap(false, true) {
		return ErrSwitchDuringTurn
	}
	defer r.turnActive.Store(false)

	if r.sessionRecorder == nil {
		return errors.New("switch session: session recording is disabled")
	}
	id := strings.TrimSpace(res.Record.SessionID)
	if id == "" || strings.TrimSpace(res.SessionPath) == "" {
		return errors.New("switch session: selected session has no id or file")
	}
	if id == r.CurrentSessionID() {
		return ErrSwitchToCurrent
	}

	release, err := r.acquireSwitchLock(id)
	if err != nil {
		return err
	}

	// Close out the departing session before the recorder is retargeted: the
	// marker must land in its file, not the target's. The inputs were validated
	// above, so AttachToFile cannot fail after this point.
	if err := r.sessionRecorder.SetCleanExit(); err != nil {
		slog.Warn("switch session: marking departing session clean failed", "err", err)
	}
	if err := r.sessionRecorder.AttachToFile(res.SessionPath, id, res.Record.Summary); err != nil {
		release()
		return fmt.Errorf("switch session: %w", err)
	}
	r.swapLiveness(release, id)

	r.restoreSwitchedState(res.Record)
	return nil
}

// acquireSwitchLock takes the target session's liveness lock, refusing when
// another process holds it. The probe runs first only to name the holder's PID;
// the acquire is what actually excludes a concurrent writer.
func (r *Runner) acquireSwitchLock(id string) (func(), error) {
	if r.livenessDir == "" {
		return nil, ErrSessionLockUnknown
	}
	state, info := session.Probe(r.livenessDir, id)
	switch state {
	case session.ProbeLive:
		return nil, fmt.Errorf("%w (pid %d)", ErrSessionLiveElsewhere, info.PID)
	case session.ProbeUnknown:
		return nil, ErrSessionLockUnknown
	}
	release := session.Acquire(r.livenessDir, session.LivenessInfo{SessionID: id})
	if release == nil {
		return nil, ErrSessionLockUnknown
	}
	return release, nil
}

// swapLiveness drops the departing session's lock and records the new holder.
// The old lock is released after the new one is held, so there is no window in
// which neither session is registered.
func (r *Runner) swapLiveness(release func(), id string) {
	r.releaseLiveness()
	r.livenessMu.Lock()
	r.livenessRelease = release
	r.livenessKey = id
	r.livenessMu.Unlock()
}

// releaseLiveness drops the held liveness lock, if any. It is idempotent, so
// Close and a switch can both call it.
func (r *Runner) releaseLiveness() {
	r.livenessMu.Lock()
	release := r.livenessRelease
	r.livenessRelease = nil
	r.livenessMu.Unlock()
	if release != nil {
		release()
	}
}

// restoreSwitchedState loads the target's conversation and standing state into
// the live runner, then recomposes the system instruction and fires SessionStart.
// It runs only while the turn guard is held.
func (r *Runner) restoreSwitchedState(rec *session.ConversationRecord) {
	r.resetSessionScopedRuntime()
	history := session.ConvertToProviderHistory(rec)
	r.ReplaceHistory(history, rec.SessionGrants)
	r.restoreStandingState(rec)
	r.autoTitleMu.Lock()
	r.autoTitleDone = strings.TrimSpace(rec.Summary) != ""
	r.titleAnnouncement = ""
	r.autoTitleMu.Unlock()
	r.applyModeSystemSuffix()

	ctx, cancel := context.WithTimeout(context.Background(), sessionSwitchHookTimeout)
	defer cancel()
	_, _ = r.FireHookEvent(ctx, hooks.EventSessionStart, "resume", func(inp *hooks.HookInput) {
		inp.SessionStartSource = "resume"
	}, nil)
}

// restoreStandingState replaces each piece of session-scoped standing state with
// the target's. A field the target never set is cleared, not inherited, so the
// departing session's goal or constraints cannot leak into the new one.
func (r *Runner) restoreStandingState(rec *session.ConversationRecord) {
	activeGoal := goal.FromSnapshot(rec.Goal)
	if activeGoal != nil {
		activeGoal.TurnCount = 0
		activeGoal.TokensBaseline = r.TotalSessionTokens()
	}
	r.goalMu.Lock()
	r.activeGoal = activeGoal
	r.goalMu.Unlock()

	activeGrill := grill.FromSnapshot(rec.Grill)
	r.grillMu.Lock()
	r.activeGrill = activeGrill
	r.grillMu.Unlock()

	r.constraintsMu.Lock()
	r.constraints = append([]string(nil), rec.Constraints...)
	r.constraintsMu.Unlock()

	r.scratchpadMu.Lock()
	r.scratchpad = sanitizeScratchpad(rec.Scratchpad)
	r.scratchpadRendered = r.scratchpad
	r.scratchpadClearedNoticePending = false
	r.scratchpadMu.Unlock()

	posture := r.readOnlyForRecord(rec)
	r.modelMu.Lock()
	r.readOnlyPosture = posture
	r.modelMu.Unlock()
}

// readOnlyForRecord returns the read-only posture a resumed session should run
// with. A session that never recorded one falls back to the configured default,
// the same rule --resume applies at launch.
func (r *Runner) readOnlyForRecord(rec *session.ConversationRecord) bool {
	if rec.ReadOnly != nil {
		return *rec.ReadOnly
	}
	return config.ResolveReadOnly(r.settingsSnapshot(), false)
}

// resetSessionScopedRuntime clears per-conversation runtime state that must not
// carry from one conversation into the next. /clear and a session switch both
// need it, so both call this rather than repeating the list.
func (r *Runner) resetSessionScopedRuntime() {
	r.firstTurnOnce = sync.Once{}
	r.resetSubagentAttempts()
	r.contextManager().ResetCompressionFailure()
	r.resetResponseChain()
	r.prefixDet.reset()
	r.metrics.resetCacheBreaks()
}

// resetResponseChain drops the Responses API previous_response_id when the
// generator keeps one. Other wire formats carry no server-side state, so the
// type assertion simply does not match for them.
func (r *Runner) resetResponseChain() {
	gen, err := r.generator()
	if err != nil {
		return
	}
	if chain, ok := gen.(interface{ ResetResponseChain() }); ok {
		chain.ResetResponseChain()
	}
}
