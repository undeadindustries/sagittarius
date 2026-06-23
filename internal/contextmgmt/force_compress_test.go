package contextmgmt

import (
	"context"
	"strings"
	"testing"
)

func TestForceCompress(t *testing.T) {
	t.Parallel()
	// A short summary (initial + verify) keeps the new token count well below the
	// original, so a forced compression of a multi-message history must report a
	// Compressed status and shrink the history.
	q := &queuedSummarizer{responses: []string{"summary", "summary"}}
	history := []Message{
		msg("user", strings.Repeat("alpha ", 50)),
		msg("model", strings.Repeat("beta ", 50)),
		msg("user", strings.Repeat("gamma ", 50)),
		msg("model", strings.Repeat("delta ", 50)),
	}

	m := NewManager(ManagerConfig{
		Enabled:              true,
		ContextLimit:         50,
		CompressionThreshold: 0.4,
		PreserveFraction:     0.3,
		Summarize:            q.fn,
	})

	newHistory, info, err := m.ForceCompress(context.Background(), cloneHistory(history))
	if err != nil {
		t.Fatalf("ForceCompress: %v", err)
	}
	if info.Status != Compressed {
		t.Fatalf("status = %v, want Compressed", info.Status)
	}
	if newHistory == nil {
		t.Fatal("expected non-nil compressed history")
	}
	if len(newHistory) >= len(history) {
		t.Fatalf("compressed history len = %d, want < %d", len(newHistory), len(history))
	}
}

func TestForceCompressUnavailable(t *testing.T) {
	t.Parallel()
	history := []Message{msg("user", "hello"), msg("model", "hi")}

	m := NewManager(ManagerConfig{
		Enabled:   false,
		Summarize: (&queuedSummarizer{responses: []string{"summary", "summary"}}).fn,
	})
	if m.CompressionAvailable() {
		t.Fatal("disabled manager must report compression unavailable")
	}

	newHistory, info, err := m.ForceCompress(context.Background(), cloneHistory(history))
	if err != nil {
		t.Fatalf("ForceCompress: %v", err)
	}
	if info.Status != CompressionNoOp {
		t.Fatalf("status = %v, want CompressionNoOp", info.Status)
	}
	if len(newHistory) != len(history) {
		t.Fatalf("history len = %d, want unchanged %d", len(newHistory), len(history))
	}

	var nilManager *Manager
	if nilManager.CompressionAvailable() {
		t.Fatal("nil manager must report compression unavailable")
	}
}
