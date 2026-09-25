package contextmgmt

import (
	"reflect"
	"strings"
	"testing"
)

// charEstimate counts text runes plus a fixed cost per tool part, so a test can
// place the budget exactly between two history shapes.
func charEstimate(parts []Part) int {
	const toolPartCost = 10
	total := 0
	for i := range parts {
		total += len([]rune(parts[i].Text))
		if parts[i].FunctionCall != nil || parts[i].FunctionResponse != nil {
			total += toolPartCost
		}
	}
	return total
}

func frMsg(name string) Message {
	return Message{Role: RoleUser, Parts: []Part{{FunctionResponse: &FunctionResponse{
		Name: name, Response: map[string]any{"output": "ok"},
	}}}}
}

func texts(history []Message) []string {
	out := make([]string, 0, len(history))
	for _, m := range history {
		switch {
		case len(m.Parts) == 0:
			out = append(out, "")
		case m.Parts[0].FunctionCall != nil:
			out = append(out, "call:"+m.Parts[0].FunctionCall.Name)
		case m.Parts[0].FunctionResponse != nil:
			out = append(out, "resp:"+m.Parts[0].FunctionResponse.Name)
		default:
			out = append(out, m.Parts[0].Text)
		}
	}
	return out
}

func TestTruncateHistoryToFit(t *testing.T) {
	t.Parallel()
	big := strings.Repeat("x", 100)
	// The compressed shape: summary + canned ack, older turns, then the current
	// turn (prompt plus its tool rounds). S, A, P each cost 1; each tool part 10.
	compressed := []Message{
		msg("user", "S"), msg("model", "A"),
		msg("user", big), msg("model", big),
		msg("user", big), msg("model", big),
		msg("user", "P"), fcMsg("read_file"), frMsg("read_file"),
	}
	// Leading 2 + current turn = 1+1+1+10+10.
	const currentTurnCost = 23

	tests := []struct {
		name        string
		history     []Message
		target      int
		wantTexts   []string
		wantDropped int
		wantOver    bool
	}{
		{
			name:        "drops every older turn but keeps the current one",
			history:     compressed,
			target:      currentTurnCost,
			wantTexts:   []string{"S", "A", "P", "call:read_file", "resp:read_file"},
			wantDropped: 4,
		},
		{
			name:        "drops only as many turns as needed",
			history:     compressed,
			target:      currentTurnCost + 200,
			wantTexts:   []string{"S", "A", big, big, "P", "call:read_file", "resp:read_file"},
			wantDropped: 2,
		},
		{
			name: "drops a tool-heavy turn whole, never orphaning a response",
			history: []Message{
				msg("user", "S"), msg("model", "A"),
				msg("user", big), fcMsg("grep"), frMsg("grep"), fcMsg("read_file"), frMsg("read_file"), msg("model", big),
				msg("user", "P"),
			},
			target:      3,
			wantTexts:   []string{"S", "A", "P"},
			wantDropped: 6,
		},
		{
			name: "oversized current prompt is kept and reported over target",
			history: []Message{
				msg("user", "S"), msg("model", "A"),
				msg("user", "old"), msg("model", "reply"),
				msg("user", big),
			},
			target:      10,
			wantTexts:   []string{"S", "A", big},
			wantDropped: 2,
			wantOver:    true,
		},
		{
			name: "mid-turn after compression has nothing droppable",
			history: []Message{
				msg("user", "S"), msg("model", "A"),
				fcMsg("grep"), frMsg("grep"), fcMsg("grep"), frMsg("grep"),
			},
			target:    1,
			wantTexts: []string{"S", "A", "call:grep", "resp:grep", "call:grep", "resp:grep"},
			wantOver:  true,
		},
		{
			name: "response paired with a leading call stays with it",
			history: []Message{
				msg("user", "first"), fcMsg("grep"), frMsg("grep"), msg("model", big),
				msg("user", big), msg("model", big),
				msg("user", "P"),
			},
			target:      40,
			wantTexts:   []string{"first", "call:grep", "resp:grep", "P"},
			wantDropped: 3,
		},
		{
			name:      "short history is unchanged",
			history:   []Message{msg("user", big), msg("model", big)},
			target:    1,
			wantTexts: []string{big, big},
			wantOver:  true,
		},
		{
			name:      "non-positive target is unchanged",
			history:   compressed,
			target:    0,
			wantTexts: texts(compressed),
		},
		{
			name:      "fits already, nothing dropped",
			history:   compressed,
			target:    10_000,
			wantTexts: texts(compressed),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res := TruncateHistoryToFit(tc.history, tc.target, charEstimate)
			if got := texts(res.NewHistory); !reflect.DeepEqual(got, tc.wantTexts) {
				t.Fatalf("history = %q, want %q", got, tc.wantTexts)
			}
			if res.DroppedCount != tc.wantDropped {
				t.Errorf("DroppedCount = %d, want %d", res.DroppedCount, tc.wantDropped)
			}
			if want := charEstimate(flattenParts(res.NewHistory)); res.NewTokenCount != want {
				t.Errorf("NewTokenCount = %d, want %d", res.NewTokenCount, want)
			}
			if tc.target > 0 && (res.NewTokenCount > tc.target) != tc.wantOver {
				t.Errorf("over target = %v, want %v (tokens %d, target %d)",
					res.NewTokenCount > tc.target, tc.wantOver, res.NewTokenCount, tc.target)
			}
		})
	}
}

// TestTruncateHistoryToFitNeverEndsOnCannedAck pins the reported failure: a
// compressed history whose recent turns are too large used to lose everything
// after the summary ack, so the request ended on an assistant message.
func TestTruncateHistoryToFitNeverEndsOnCannedAck(t *testing.T) {
	t.Parallel()
	big := strings.Repeat("y", 500)
	history := buildCompressedHistory("summary", []Message{
		msg("user", big), msg("model", big),
		msg("user", big), msg("model", big),
		msg("user", "current prompt"),
	})
	res := TruncateHistoryToFit(history, 1, charEstimate)
	last := res.NewHistory[len(res.NewHistory)-1]
	if last.Role != RoleUser || last.Parts[0].Text != "current prompt" {
		t.Fatalf("last message = %+v, want the current user prompt", last)
	}
}

func TestTruncateHistoryToFitDoesNotMutateInput(t *testing.T) {
	t.Parallel()
	big := strings.Repeat("z", 100)
	history := []Message{
		msg("user", "S"), msg("model", "A"),
		msg("user", big), msg("model", big),
		msg("user", "P"),
	}
	before := texts(history)
	res := TruncateHistoryToFit(history, 5, charEstimate)
	if res.DroppedCount == 0 {
		t.Fatal("expected a drop so the mutation check is meaningful")
	}
	if got := texts(history); !reflect.DeepEqual(got, before) {
		t.Fatalf("input mutated: %q, want %q", got, before)
	}
}
