package config

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSetUIUpdateCheckedAt(t *testing.T) {
	s := &Settings{Raw: map[string]json.RawMessage{}}

	// Set
	now := time.Now().Truncate(time.Second) // JSON doesn't do nanoseconds well
	if err := s.SetUIUpdateCheckedAt(now); err != nil {
		t.Fatalf("SetUIUpdateCheckedAt: %v", err)
	}

	ui := s.UI()
	if ui.UpdateCheckedAt == "" {
		t.Fatalf("UpdateCheckedAt is empty")
	}
	parsed, err := time.Parse(time.RFC3339, ui.UpdateCheckedAt)
	if err != nil {
		t.Fatalf("Parse time: %v", err)
	}
	if !parsed.Equal(now) {
		t.Errorf("expected %v, got %v", now, parsed)
	}

	// Clear
	if err := s.SetUIUpdateCheckedAt(time.Time{}); err != nil {
		t.Fatalf("SetUIUpdateCheckedAt(zero): %v", err)
	}

	ui = s.UI()
	if ui.UpdateCheckedAt != "" {
		t.Errorf("expected empty UpdateCheckedAt, got %s", ui.UpdateCheckedAt)
	}
}
