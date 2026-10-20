package agent

import (
	"errors"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/provider"
)

func TestParseReportedOutcome(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, in, outcome, reason, body string
	}{
		{"completed", "OUTCOME: completed\nFound three callers.", reportedOutcomeCompleted, "", "Found three callers."},
		{"partial", "OUTCOME: partial\nDid two of three.", reportedOutcomePartial, "", "Did two of three."},
		{"failed with reason", "OUTCOME: failed: lease denied main.go\nDetails.", reportedOutcomeFailed, "lease denied main.go", "Details."},
		{"failed no reason", "OUTCOME: failed", reportedOutcomeFailed, "", ""},
		{"case and leading blank lines", "\n\n  outcome:  Completed  \nok", reportedOutcomeCompleted, "", "ok"},
		{"backticked", "`OUTCOME: partial`\nrest", reportedOutcomePartial, "", "rest"},
		{"missing", "Found three callers.", reportedOutcomeUnreported, "", "Found three callers."},
		{"unknown value", "OUTCOME: maybe\nrest", reportedOutcomeUnreported, "", "OUTCOME: maybe\nrest"},
		{"not first line", "Summary first.\nOUTCOME: failed: x", reportedOutcomeUnreported, "", "Summary first.\nOUTCOME: failed: x"},
		{"empty", "", reportedOutcomeUnreported, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			outcome, reason, body := parseReportedOutcome(tt.in)
			if outcome != tt.outcome || reason != tt.reason || body != tt.body {
				t.Errorf("parseReportedOutcome(%q) = (%q, %q, %q), want (%q, %q, %q)",
					tt.in, outcome, reason, body, tt.outcome, tt.reason, tt.body)
			}
		})
	}
}

func TestApplyReportedOutcomeIsDowngradeOnly(t *testing.T) {
	t.Parallel()
	tests := []struct{ harness, outcome, want string }{
		{subagentStatusCompleted, reportedOutcomeCompleted, subagentStatusCompleted},
		{subagentStatusCompleted, reportedOutcomeUnreported, subagentStatusCompleted},
		{subagentStatusCompleted, reportedOutcomePartial, subagentStatusIncomplete},
		{subagentStatusCompleted, reportedOutcomeFailed, subagentStatusFailed},
		{subagentStatusFailed, reportedOutcomeCompleted, subagentStatusFailed},
		{subagentStatusIncomplete, reportedOutcomeCompleted, subagentStatusIncomplete},
		{subagentStatusCanceled, reportedOutcomeCompleted, subagentStatusCanceled},
	}
	for _, tt := range tests {
		if got := applyReportedOutcome(tt.harness, tt.outcome); got != tt.want {
			t.Errorf("applyReportedOutcome(%q, %q) = %q, want %q", tt.harness, tt.outcome, got, tt.want)
		}
	}
}

func TestBuildSubagentResultReportedOutcome(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		final       string
		runErr      error
		wantStatus  string
		wantOutcome string
		wantSummary string
		wantError   string
		wantNext    bool
	}{
		{"reported completed", "OUTCOME: completed\nAnswer.", nil,
			subagentStatusCompleted, reportedOutcomeCompleted, "Answer.", "", false},
		{"reported partial", "OUTCOME: partial\nHalf done.", nil,
			subagentStatusIncomplete, reportedOutcomePartial, "Half done.", "", false},
		{"reported failed", "OUTCOME: failed: no such package\nLooked everywhere.", nil,
			subagentStatusFailed, reportedOutcomeFailed, "Looked everywhere.",
			"subagent reported failure: no such package", false},
		{"unreported", "Answer without a line.", nil,
			subagentStatusCompleted, reportedOutcomeUnreported, "Answer without a line.", "", true},
		{"harness failure beats reported success", "OUTCOME: completed\nAll good.", errors.New("boom"),
			subagentStatusFailed, reportedOutcomeCompleted, "All good.", "boom", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, child := resultTestChild(t, config.SubagentResearch)
			child.runner.ReplaceHistory([]provider.Message{
				{Role: provider.RoleUser, Parts: []provider.Part{{Text: "question"}}},
				{Role: provider.RoleModel, Parts: []provider.Part{{Text: tt.final}}},
			}, nil)
			got := buildSubagentResult(child, config.SubagentResearch, tt.runErr, 1, 0)
			if got["status"] != tt.wantStatus {
				t.Errorf("status = %v, want %v", got["status"], tt.wantStatus)
			}
			if got["reported_outcome"] != tt.wantOutcome {
				t.Errorf("reported_outcome = %v, want %v", got["reported_outcome"], tt.wantOutcome)
			}
			if got["summary"] != tt.wantSummary || got["result"] != tt.wantSummary {
				t.Errorf("summary/result = %v/%v, want %q", got["summary"], got["result"], tt.wantSummary)
			}
			if errText, _ := got["error"].(string); errText != tt.wantError {
				t.Errorf("error = %q, want %q", errText, tt.wantError)
			}
			if _, ok := got["next_step"]; ok != tt.wantNext {
				t.Errorf("next_step present = %v, want %v (%v)", ok, tt.wantNext, got["next_step"])
			}
		})
	}
}
