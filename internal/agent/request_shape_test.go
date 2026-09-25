package agent

import (
	"testing"

	"github.com/undeadindustries/sagittarius/internal/provider"
)

func TestTrimTrailingModelText(t *testing.T) {
	t.Parallel()
	user := provider.Message{Role: provider.RoleUser, Parts: []provider.Part{{Text: "question"}}}
	modelText := provider.Message{Role: provider.RoleModel, Parts: []provider.Part{{Text: "Got it."}}}
	modelCall := provider.Message{Role: provider.RoleModel, Parts: []provider.Part{{
		FunctionCall: &provider.ToolCall{ID: "c1", Name: "read_file"},
	}}}
	toolResult := provider.Message{Role: provider.RoleUser, Parts: []provider.Part{{
		FunctionResponse: &provider.FunctionResponse{Name: "read_file", CallID: "c1"},
	}}}

	tests := []struct {
		name        string
		in          []provider.Message
		wantLen     int
		wantTrimmed int
	}{
		{"trailing assistant text is trimmed", []provider.Message{user, modelText}, 1, 1},
		{"several trailing assistant texts are trimmed", []provider.Message{user, modelText, modelText}, 1, 2},
		{"trailing tool call is kept", []provider.Message{user, modelCall}, 2, 0},
		{"trailing user message is unchanged", []provider.Message{user, modelText, user}, 3, 0},
		{"trailing tool result is unchanged", []provider.Message{user, modelCall, toolResult}, 3, 0},
		{"lone message is never emptied", []provider.Message{modelText}, 1, 0},
		{"empty request is unchanged", nil, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, trimmed := trimTrailingModelText(tc.in)
			if len(got) != tc.wantLen || trimmed != tc.wantTrimmed {
				t.Fatalf("len = %d trimmed = %d, want len %d trimmed %d", len(got), trimmed, tc.wantLen, tc.wantTrimmed)
			}
		})
	}
}

// TestBuildGenerateRequestNeverEndsOnAssistantText checks the guard at the
// request boundary without touching the runner's own history.
func TestBuildGenerateRequestNeverEndsOnAssistantText(t *testing.T) {
	t.Parallel()
	r, err := NewRunner(RunnerConfig{Generator: &fakeGenerator{}, Model: "m", WorkDir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	history := []provider.Message{
		{Role: provider.RoleUser, Parts: []provider.Part{{Text: "summary"}}},
		{Role: provider.RoleModel, Parts: []provider.Part{{Text: "Got it."}}},
	}
	r.ReplaceHistory(history, nil)

	req := r.buildGenerateRequest()
	if last := req.Messages[len(req.Messages)-1]; last.Role != provider.RoleUser {
		t.Fatalf("request ends on role %q, want user", last.Role)
	}
	if got := len(r.History()); got != len(history) {
		t.Fatalf("history len = %d, want %d: the guard must not rewrite history", got, len(history))
	}
}
