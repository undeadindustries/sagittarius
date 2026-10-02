package trajectory

import (
	"testing"

	"github.com/undeadindustries/sagittarius/internal/session"
)

func TestFromSession_BasicAndValidation(t *testing.T) {
	inTok := 100
	outTok := 50
	cachedTok := 20
	reasoningTok := 10
	cost := 0.002
	dur := int64(15)

	rec := &session.ConversationRecord{
		SessionID:    "sess-123",
		Kind:         "main",
		AgentVersion: "0.20.1",
		Messages: []session.MessageRecord{
			{
				ID:        "msg-1",
				Timestamp: "2026-10-02T05:00:00Z",
				Type:      session.MessageTypeUser,
				Origin:    "user",
				Content:   []session.Part{{Text: "Hello assistant"}},
			},
			{
				ID:        "msg-2",
				Timestamp: "2026-10-02T05:00:01Z",
				Type:      session.MessageTypeModel,
				Content: []session.Part{
					{Text: "I will read the file."},
					{FunctionCall: &session.FunctionCallPart{
						ID:   "call-1",
						Name: "read_file",
						Args: map[string]interface{}{"file_path": "hello.txt"},
					}},
				},
				Round: &session.RoundTelemetry{
					Provider:         "openai",
					Model:            "gpt-4o",
					Mode:             "agent",
					InputTokens:      inTok,
					OutputTokens:     outTok,
					CachedTokens:     cachedTok,
					ReasoningTokens:  reasoningTok,
					CostUSD:          cost,
					CostKnown:        true,
					LatencyMs:        450,
					LLMCalls:         1,
					SystemPromptHash: "abc123hash",
				},
			},
			{
				ID:        "msg-3",
				Timestamp: "2026-10-02T05:00:02Z",
				Type:      session.MessageTypeUser,
				Content: []session.Part{
					{FunctionResponse: &session.FuncResponsePart{
						ID:       "call-1",
						Name:     "read_file",
						Response: map[string]interface{}{"content": "file contents"},
					}},
				},
				ToolResults: []session.ToolResultTelemetry{
					{
						ID:         "call-1",
						Name:       "read_file",
						DurationMs: dur,
						Status:     "ok",
					},
				},
			},
		},
		Events: []session.EventRecord{
			{
				Type:      "truncation",
				Timestamp: "2026-10-02T05:00:03Z",
				Data:      map[string]any{"dropped_count": 1},
			},
		},
	}

	traj, err := FromSession(rec, nil, Options{RedactSecrets: true})
	if err != nil {
		t.Fatalf("FromSession failed: %v", err)
	}

	if traj.SchemaVersion != "1.7" {
		t.Errorf("expected schema version 1.7, got %s", traj.SchemaVersion)
	}
	if len(traj.Steps) != 3 { // user, agent (folded func), system ($event)
		t.Fatalf("expected 3 steps, got %d", len(traj.Steps))
	}

	valErrs := Validate(traj)
	if len(valErrs) > 0 {
		t.Fatalf("validation failed: %v", valErrs)
	}

	analysis := Analyze(traj)
	if analysis.TotalSteps != 3 {
		t.Errorf("expected 3 total steps in analysis, got %d", analysis.TotalSteps)
	}
	if analysis.ToolHealth.TotalCalls != 1 {
		t.Errorf("expected 1 tool call, got %d", analysis.ToolHealth.TotalCalls)
	}
}

func TestValidate_NegativeCases(t *testing.T) {
	traj := &Trajectory{
		SchemaVersion: "1.7",
		SessionID:     "test-sess",
		Agent:         Agent{Name: "test-agent"},
		Steps: []Step{
			{
				StepID: 2, // invalid sequence
				Source: StepSourceUser,
				ToolCalls: []ToolCall{ // user cannot have tool calls
					{CallID: "c1", ToolName: "test"},
				},
			},
		},
	}

	errs := Validate(traj)
	if len(errs) < 2 {
		t.Errorf("expected at least 2 validation errors, got %d: %v", len(errs), errs)
	}
}

func TestFromSession_Redaction(t *testing.T) {
	rec := &session.ConversationRecord{
		SessionID: "sess-redact",
		Messages: []session.MessageRecord{
			{
				ID:        "msg-1",
				Timestamp: "2026-10-02T05:00:00Z",
				Type:      session.MessageTypeUser,
				Content:   []session.Part{{Text: "Here is my secret: sk-1234567890abcdefghijklmnop"}},
			},
		},
	}

	traj, err := FromSession(rec, nil, Options{RedactSecrets: true})
	if err != nil {
		t.Fatalf("FromSession failed: %v", err)
	}

	if len(traj.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(traj.Steps))
	}
	if traj.Steps[0].Message != "Here is my secret: [REDACTED API KEY]" {
		t.Errorf("expected redacted secret, got %q", traj.Steps[0].Message)
	}
}
