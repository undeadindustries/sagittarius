package prompteval

import (
	"strings"
	"testing"
)

func TestParseVerboseLog(t *testing.T) {
	sampleLog := `
===== 2024-01-01T12:00:00Z | IN  user message =====
Look at my-file.txt and tell me what you would change.
Do not change anything yet.
===== 2024-01-01T12:00:01Z | OUT request (round 1) =====
{"contents": "..."}
===== 2024-01-01T12:00:05Z | IN  model response (round 1) =====
text:
I will check it out.
tool_call: read_file({"path":"my-file.txt"}) id=call_123
===== 2024-01-01T12:00:06Z | IN  user message =====
OK, go ahead.
===== 2024-01-01T12:00:07Z | IN  model response (round 1) =====
tool_call: edit({"new_string":"hello","old_string":"hi","path":"my-file.txt"}) id=call_456
`

	log, err := ParseVerboseLog(strings.NewReader(sampleLog))
	if err != nil {
		t.Fatalf("ParseVerboseLog failed: %v", err)
	}

	if len(log.Turns) != 2 {
		t.Fatalf("Expected 2 turns, got %d", len(log.Turns))
	}

	turn1 := log.Turns[0]
	if !strings.Contains(turn1.UserMessage, "Look at my-file.txt") {
		t.Errorf("Turn 1 user message unexpected: %q", turn1.UserMessage)
	}
	if len(turn1.ToolCalls) != 1 {
		t.Fatalf("Turn 1 expected 1 tool call, got %d", len(turn1.ToolCalls))
	}
	if tc := turn1.ToolCalls[0]; tc.Name != "read_file" || tc.Args != `{"path":"my-file.txt"}` || tc.ID != "call_123" {
		t.Errorf("Turn 1 tool call unexpected: %+v", tc)
	}

	turn2 := log.Turns[1]
	if turn2.UserMessage != "OK, go ahead." {
		t.Errorf("Turn 2 user message unexpected: %q", turn2.UserMessage)
	}
	if len(turn2.ToolCalls) != 1 {
		t.Fatalf("Turn 2 expected 1 tool call, got %d", len(turn2.ToolCalls))
	}
	if tc := turn2.ToolCalls[0]; tc.Name != "edit" || !strings.Contains(tc.Args, "new_string") || tc.ID != "call_456" {
		t.Errorf("Turn 2 tool call unexpected: %+v", tc)
	}
}
