package contextmgmt

import (
	"math"
	"sync"
)

// Adaptive compression threshold constants ported from adaptiveThreshold.ts.
const (
	// ringBufferSize bounds the per-session compression-ratio history.
	ringBufferSize = 5
	// weakRatio classifies a compression as weak when the new token count
	// exceeds this fraction of the original (freed less than 15%).
	weakRatio = 0.85
	// AdaptiveThresholdFloor is the lowest threshold adaptation will produce.
	AdaptiveThresholdFloor = 0.35
	// tighteningStep tightens the threshold per weak sample observed.
	tighteningStep = 0.05
	// DefaultAdaptiveCooldownTurns is the minimum turns between tightenings.
	DefaultAdaptiveCooldownTurns = 5
	// maxWeakSamplesPerTighten caps the per-pass tightening magnitude.
	maxWeakSamplesPerTighten = 3
)

type compressionSample struct {
	originalTokenCount int
	newTokenCount      int
	turnIndex          int
}

type sessionState struct {
	ring             []compressionSample
	lastAdaptiveTurn int
	hasTightened     bool
}

// AdaptiveTracker maintains per-session compression-ratio history and returns a
// tightened compression threshold when recent compressions were weak. It is
// safe for concurrent use. Unlike the fork's module-scoped map, state lives on
// the instance so tests and sessions are isolated (golang: no package-level
// mutable shared state).
type AdaptiveTracker struct {
	mu       sync.Mutex
	sessions map[string]*sessionState
}

// NewAdaptiveTracker returns an empty tracker.
func NewAdaptiveTracker() *AdaptiveTracker {
	return &AdaptiveTracker{sessions: make(map[string]*sessionState)}
}

// EffectiveThresholdOptions configures EffectiveCompressionThreshold.
type EffectiveThresholdOptions struct {
	// SessionID keys the per-session ring buffer.
	SessionID string
	// CurrentTurnIndex is a monotonic counter used for cooldown.
	CurrentTurnIndex int
	// UserOverridePresent disables adaptation when the operator pinned a value.
	UserOverridePresent bool
	// CooldownTurns overrides DefaultAdaptiveCooldownTurns when > 0.
	CooldownTurns int
	// Floor overrides AdaptiveThresholdFloor when > 0.
	Floor float64
}

// RecordCompressionResult appends a compression outcome to the session ring.
// Invalid inputs (non-positive original, negative new, empty session) are
// ignored. The ring is capped at ringBufferSize entries.
func (t *AdaptiveTracker) RecordCompressionResult(sessionID string, originalTokenCount, newTokenCount, turnIndex int) {
	if sessionID == "" || originalTokenCount <= 0 || newTokenCount < 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	state := t.getOrInit(sessionID)
	state.ring = append(state.ring, compressionSample{originalTokenCount, newTokenCount, turnIndex})
	if len(state.ring) > ringBufferSize {
		state.ring = state.ring[len(state.ring)-ringBufferSize:]
	}
}

// EffectiveCompressionThreshold returns base, possibly tightened toward the
// floor when recent compressions were weak and the cooldown has elapsed.
func (t *AdaptiveTracker) EffectiveCompressionThreshold(base float64, opts EffectiveThresholdOptions) float64 {
	if math.IsNaN(base) || math.IsInf(base, 0) {
		return base
	}
	if opts.UserOverridePresent || opts.SessionID == "" {
		return base
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	state, ok := t.sessions[opts.SessionID]
	if !ok || len(state.ring) == 0 {
		return base
	}

	cooldown := opts.CooldownTurns
	if cooldown <= 0 {
		cooldown = DefaultAdaptiveCooldownTurns
	}
	floor := opts.Floor
	if floor <= 0 {
		floor = AdaptiveThresholdFloor
	}

	if state.hasTightened && opts.CurrentTurnIndex-state.lastAdaptiveTurn < cooldown {
		return base
	}

	weakCount := 0
	for _, s := range state.ring {
		if float64(s.newTokenCount) > float64(s.originalTokenCount)*weakRatio {
			weakCount++
		}
	}
	if weakCount == 0 {
		return base
	}

	steps := weakCount
	if steps > maxWeakSamplesPerTighten {
		steps = maxWeakSamplesPerTighten
	}
	tightened := math.Max(floor, base-tighteningStep*float64(steps))
	if tightened < base {
		state.lastAdaptiveTurn = opts.CurrentTurnIndex
		state.hasTightened = true
	}
	return tightened
}

// Reset clears state for a session. Call on new chat or session end.
func (t *AdaptiveTracker) Reset(sessionID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.sessions, sessionID)
}

// ringLen returns the number of samples held for a session (test helper).
func (t *AdaptiveTracker) ringLen(sessionID string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	if state, ok := t.sessions[sessionID]; ok {
		return len(state.ring)
	}
	return 0
}

func (t *AdaptiveTracker) getOrInit(sessionID string) *sessionState {
	state, ok := t.sessions[sessionID]
	if !ok {
		state = &sessionState{}
		t.sessions[sessionID] = state
	}
	return state
}
