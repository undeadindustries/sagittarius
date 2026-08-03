package agent

import (
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
)

func TestApplySettingValueEditAndSubagents(t *testing.T) {
	s := &config.Settings{}

	// Test Edit toggle
	if err := applySettingValue(s, "sagittarius.edit.enabled", "true"); err != nil {
		t.Fatalf("apply edit.enabled: %v", err)
	}
	if s.Sagittarius == nil || s.Sagittarius.Edit == nil || s.Sagittarius.Edit.Enabled == nil || *s.Sagittarius.Edit.Enabled != true {
		t.Errorf("edit.enabled was not set to true")
	}

	// Clear Edit toggle
	if err := clearSettingValue(s, "sagittarius.edit.enabled"); err != nil {
		t.Fatalf("clear edit.enabled: %v", err)
	}
	if s.Sagittarius.Edit.Enabled != nil {
		t.Errorf("edit.enabled was not cleared")
	}

	// Test Subagents toggle
	if err := applySettingValue(s, "sagittarius.subagents.enabled", "true"); err != nil {
		t.Fatalf("apply subagents.enabled: %v", err)
	}
	if s.Sagittarius == nil || s.Sagittarius.Subagents == nil || s.Sagittarius.Subagents.Enabled == nil || *s.Sagittarius.Subagents.Enabled != true {
		t.Errorf("subagents.enabled was not set to true")
	}

	// Clear Subagents toggle
	if err := clearSettingValue(s, "sagittarius.subagents.enabled"); err != nil {
		t.Fatalf("clear subagents.enabled: %v", err)
	}
	if s.Sagittarius.Subagents.Enabled != nil {
		t.Errorf("subagents.enabled was not cleared")
	}
}
