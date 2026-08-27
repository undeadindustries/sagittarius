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

func TestApplySettingValueDefaultMode(t *testing.T) {
	s := &config.Settings{}

	if err := applySettingValue(s, "sagittarius.defaultMode", "plan"); err != nil {
		t.Fatalf("apply defaultMode: %v", err)
	}
	if s.Sagittarius == nil || s.Sagittarius.DefaultMode != "plan" {
		t.Errorf("defaultMode = %q, want plan", s.Sagittarius.DefaultMode)
	}

	if err := clearSettingValue(s, "sagittarius.defaultMode"); err != nil {
		t.Fatalf("clear defaultMode: %v", err)
	}
	if s.Sagittarius.DefaultMode != "" {
		t.Errorf("defaultMode was not cleared, got %q", s.Sagittarius.DefaultMode)
	}
}

func TestApplySettingValueGoal(t *testing.T) {
	s := &config.Settings{}

	if err := applySettingValue(s, "sagittarius.goal.evaluatorModel", "qwen/qwen3.5-122b"); err != nil {
		t.Fatalf("apply evaluatorModel: %v", err)
	}
	if s.Sagittarius.Goal.EvaluatorModel != "qwen/qwen3.5-122b" {
		t.Fatalf("evaluatorModel = %q", s.Sagittarius.Goal.EvaluatorModel)
	}

	if err := applySettingValue(s, "sagittarius.goal.evaluatorProvider", "openrouter"); err != nil {
		t.Fatalf("apply evaluatorProvider: %v", err)
	}
	if s.Sagittarius.Goal.EvaluatorProvider != "openrouter" {
		t.Fatalf("evaluatorProvider = %q", s.Sagittarius.Goal.EvaluatorProvider)
	}

	if err := applySettingValue(s, "sagittarius.goal.evaluatorTimeout", "180"); err != nil {
		t.Fatalf("apply evaluatorTimeout: %v", err)
	}
	if s.Sagittarius.Goal.EvaluatorTimeout == nil || *s.Sagittarius.Goal.EvaluatorTimeout != 180 {
		t.Fatalf("evaluatorTimeout = %v", s.Sagittarius.Goal.EvaluatorTimeout)
	}

	if err := applySettingValue(s, "sagittarius.goal.maxTurns", "10"); err != nil {
		t.Fatalf("apply maxTurns: %v", err)
	}
	if s.Sagittarius.Goal.MaxTurns == nil || *s.Sagittarius.Goal.MaxTurns != 10 {
		t.Fatalf("maxTurns = %v", s.Sagittarius.Goal.MaxTurns)
	}

	if err := clearSettingValue(s, "sagittarius.goal.evaluatorModel"); err != nil {
		t.Fatalf("clear evaluatorModel: %v", err)
	}
	if s.Sagittarius.Goal.EvaluatorModel != "" {
		t.Fatalf("evaluatorModel not cleared: %q", s.Sagittarius.Goal.EvaluatorModel)
	}
	if err := clearSettingValue(s, "sagittarius.goal.evaluatorTimeout"); err != nil {
		t.Fatalf("clear evaluatorTimeout: %v", err)
	}
	if s.Sagittarius.Goal.EvaluatorTimeout != nil {
		t.Fatal("evaluatorTimeout not cleared")
	}
}

func TestApplySettingValueMaxToolRounds(t *testing.T) {
	s := &config.Settings{}

	if err := applySettingValue(s, "sagittarius.maxToolRounds", "0"); err != nil {
		t.Fatalf("apply 0: %v", err)
	}
	if s.Sagittarius.MaxToolRounds == nil || *s.Sagittarius.MaxToolRounds != 0 {
		t.Fatalf("MaxToolRounds = %v, want 0", s.Sagittarius.MaxToolRounds)
	}

	if err := applySettingValue(s, "sagittarius.maxToolRounds", "250"); err != nil {
		t.Fatalf("apply 250: %v", err)
	}
	if s.Sagittarius.MaxToolRounds == nil || *s.Sagittarius.MaxToolRounds != 250 {
		t.Fatalf("MaxToolRounds = %v, want 250", s.Sagittarius.MaxToolRounds)
	}

	if err := applySettingValue(s, "sagittarius.maxToolRounds", "-1"); err == nil {
		t.Fatal("expected error for negative maxToolRounds")
	}
	if s.Sagittarius.MaxToolRounds == nil || *s.Sagittarius.MaxToolRounds != 250 {
		t.Fatalf("rejected value mutated MaxToolRounds to %v", s.Sagittarius.MaxToolRounds)
	}
}

func TestApplySettingValueDefaultModeRejectsUnknownValue(t *testing.T) {
	s := &config.Settings{}

	if err := applySettingValue(s, "sagittarius.defaultMode", "yolo"); err == nil {
		t.Fatal("expected an error for an unrecognized mode name")
	}
	if s.Sagittarius != nil && s.Sagittarius.DefaultMode != "" {
		t.Errorf("expected no mutation on a rejected value, got %q", s.Sagittarius.DefaultMode)
	}
}
