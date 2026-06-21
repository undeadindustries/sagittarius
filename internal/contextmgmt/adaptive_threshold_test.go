package contextmgmt

import "testing"

const testSession = "test-session"

func TestAdaptiveThresholdReturnsBaseWithoutSamples(t *testing.T) {
	t.Parallel()
	tr := NewAdaptiveTracker()
	got := tr.EffectiveCompressionThreshold(0.5, EffectiveThresholdOptions{
		SessionID:        testSession,
		CurrentTurnIndex: 10,
	})
	if got != 0.5 {
		t.Fatalf("threshold = %v, want 0.5", got)
	}
}

func TestAdaptiveThresholdTightensOnWeakCompressions(t *testing.T) {
	t.Parallel()
	tr := NewAdaptiveTracker()
	tr.RecordCompressionResult(testSession, 10_000, 9_500, 1)
	tr.RecordCompressionResult(testSession, 10_000, 9_000, 2)
	got := tr.EffectiveCompressionThreshold(0.5, EffectiveThresholdOptions{
		SessionID:        testSession,
		CurrentTurnIndex: 100,
	})
	if got >= 0.5 {
		t.Errorf("threshold = %v, want < 0.5", got)
	}
	if got < AdaptiveThresholdFloor {
		t.Errorf("threshold = %v, want >= floor %v", got, AdaptiveThresholdFloor)
	}
}

func TestAdaptiveThresholdNoTightenOnEffectiveCompressions(t *testing.T) {
	t.Parallel()
	tr := NewAdaptiveTracker()
	tr.RecordCompressionResult(testSession, 10_000, 3_000, 1)
	tr.RecordCompressionResult(testSession, 10_000, 2_000, 2)
	got := tr.EffectiveCompressionThreshold(0.5, EffectiveThresholdOptions{
		SessionID:        testSession,
		CurrentTurnIndex: 100,
	})
	if got != 0.5 {
		t.Errorf("threshold = %v, want 0.5", got)
	}
}

func TestAdaptiveThresholdRespectsFloor(t *testing.T) {
	t.Parallel()
	tr := NewAdaptiveTracker()
	for i := 0; i < 10; i++ {
		tr.RecordCompressionResult(testSession, 10_000, 9_900, i)
	}
	got := tr.EffectiveCompressionThreshold(0.4, EffectiveThresholdOptions{
		SessionID:        testSession,
		CurrentTurnIndex: 1_000,
		Floor:            0.35,
	})
	if got != 0.35 {
		t.Errorf("threshold = %v, want 0.35", got)
	}
}

func TestAdaptiveThresholdRespectsCooldown(t *testing.T) {
	t.Parallel()
	tr := NewAdaptiveTracker()
	tr.RecordCompressionResult(testSession, 10_000, 9_500, 1)
	tr.RecordCompressionResult(testSession, 10_000, 9_500, 2)

	first := tr.EffectiveCompressionThreshold(0.5, EffectiveThresholdOptions{
		SessionID: testSession, CurrentTurnIndex: 5, CooldownTurns: 5,
	})
	if first >= 0.5 {
		t.Fatalf("first = %v, want < 0.5", first)
	}
	second := tr.EffectiveCompressionThreshold(0.5, EffectiveThresholdOptions{
		SessionID: testSession, CurrentTurnIndex: 6, CooldownTurns: 5,
	})
	if second != 0.5 {
		t.Errorf("second = %v, want 0.5 (cooldown)", second)
	}
	third := tr.EffectiveCompressionThreshold(0.5, EffectiveThresholdOptions{
		SessionID: testSession, CurrentTurnIndex: 12, CooldownTurns: 5,
	})
	if third >= 0.5 {
		t.Errorf("third = %v, want < 0.5 (cooldown elapsed)", third)
	}
}

func TestAdaptiveThresholdUserOverride(t *testing.T) {
	t.Parallel()
	tr := NewAdaptiveTracker()
	tr.RecordCompressionResult(testSession, 10_000, 9_500, 1)
	tr.RecordCompressionResult(testSession, 10_000, 9_500, 2)
	got := tr.EffectiveCompressionThreshold(0.5, EffectiveThresholdOptions{
		SessionID:           testSession,
		CurrentTurnIndex:    100,
		UserOverridePresent: true,
	})
	if got != 0.5 {
		t.Errorf("threshold = %v, want 0.5", got)
	}
}

func TestAdaptiveThresholdMissingSession(t *testing.T) {
	t.Parallel()
	tr := NewAdaptiveTracker()
	tr.RecordCompressionResult(testSession, 10_000, 9_500, 1)
	got := tr.EffectiveCompressionThreshold(0.5, EffectiveThresholdOptions{
		SessionID:        "",
		CurrentTurnIndex: 100,
	})
	if got != 0.5 {
		t.Errorf("threshold = %v, want 0.5", got)
	}
}

func TestAdaptiveThresholdRingBufferCap(t *testing.T) {
	t.Parallel()
	tr := NewAdaptiveTracker()
	for i := 0; i < 20; i++ {
		tr.RecordCompressionResult(testSession, 10_000, 9_500, i)
	}
	if n := tr.ringLen(testSession); n != ringBufferSize {
		t.Errorf("ring length = %d, want %d", n, ringBufferSize)
	}
}

func TestAdaptiveThresholdIgnoresInvalidSamples(t *testing.T) {
	t.Parallel()
	tr := NewAdaptiveTracker()
	tr.RecordCompressionResult(testSession, 0, 100, 1)
	tr.RecordCompressionResult(testSession, 100, -1, 1)
	tr.RecordCompressionResult("", 100, 100, 1)
	if n := tr.ringLen(testSession); n != 0 {
		t.Errorf("ring length = %d, want 0", n)
	}
}
