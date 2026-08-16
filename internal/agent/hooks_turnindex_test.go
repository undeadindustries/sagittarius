package agent

import (
	"testing"

	"github.com/undeadindustries/sagittarius/internal/provider"
)

func TestCountUserTurns(t *testing.T) {
	t.Parallel()

	resp := &provider.FunctionResponse{Name: "read_file"}
	cases := []struct {
		name string
		in   []provider.Message
		want int
	}{
		{name: "empty", in: nil, want: 0},
		{
			name: "two exchanges",
			in: []provider.Message{
				{Role: provider.RoleUser, Parts: []provider.Part{{Text: "one"}}},
				{Role: provider.RoleModel, Parts: []provider.Part{{Text: "reply"}}},
				{Role: provider.RoleUser, Parts: []provider.Part{{Text: "two"}}},
				{Role: provider.RoleModel, Parts: []provider.Part{{Text: "reply"}}},
			},
			want: 2,
		},
		{
			// Tool results are recorded with the user role but are not user turns.
			name: "tool results are not turns",
			in: []provider.Message{
				{Role: provider.RoleUser, Parts: []provider.Part{{Text: "one"}}},
				{Role: provider.RoleModel, Parts: []provider.Part{{Text: "calling"}}},
				{Role: provider.RoleUser, Parts: []provider.Part{{FunctionResponse: resp}}},
				{Role: provider.RoleModel, Parts: []provider.Part{{Text: "done"}}},
			},
			want: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := countUserTurns(tc.in); got != tc.want {
				t.Fatalf("countUserTurns = %d, want %d", got, tc.want)
			}
		})
	}
}

// A resumed session must keep counting turns where it left off, otherwise a hook
// gating on "every Nth turn" restarts its cycle on every resume.
func TestResumedSessionContinuesTurnIndex(t *testing.T) {
	t.Parallel()

	history := []provider.Message{
		{Role: provider.RoleUser, Parts: []provider.Part{{Text: "one"}}},
		{Role: provider.RoleModel, Parts: []provider.Part{{Text: "reply"}}},
		{Role: provider.RoleUser, Parts: []provider.Part{{Text: "two"}}},
		{Role: provider.RoleModel, Parts: []provider.Part{{Text: "reply"}}},
	}

	runner, err := NewRunner(RunnerConfig{
		Generator:      &fakeGenerator{},
		Model:          "test-model",
		WorkDir:        t.TempDir(),
		Interactive:    false,
		InitialHistory: history,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	if got := runner.TurnCounter(); got != 2 {
		t.Fatalf("TurnCounter after resume = %d, want 2", got)
	}

	// FirstTurn must already be spent: the first turn happened before the resume.
	fired := false
	runner.firstTurnOnce.Do(func() { fired = true })
	if fired {
		t.Fatal("FirstTurn would fire again on a resumed session")
	}
}

// /chat resume replaces history in place and must behave the same way.
func TestReplaceHistorySeedsTurnIndex(t *testing.T) {
	t.Parallel()

	runner, err := NewRunner(RunnerConfig{
		Generator:   &fakeGenerator{},
		Model:       "test-model",
		WorkDir:     t.TempDir(),
		Interactive: false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	if got := runner.TurnCounter(); got != 0 {
		t.Fatalf("fresh TurnCounter = %d, want 0", got)
	}

	runner.ReplaceHistory([]provider.Message{
		{Role: provider.RoleUser, Parts: []provider.Part{{Text: "one"}}},
		{Role: provider.RoleModel, Parts: []provider.Part{{Text: "reply"}}},
		{Role: provider.RoleUser, Parts: []provider.Part{{Text: "two"}}},
	}, nil)

	if got := runner.TurnCounter(); got != 2 {
		t.Fatalf("TurnCounter after ReplaceHistory = %d, want 2", got)
	}

	runner.ClearHistory()
	if got := runner.TurnCounter(); got != 0 {
		t.Fatalf("TurnCounter after ClearHistory = %d, want 0", got)
	}
}
