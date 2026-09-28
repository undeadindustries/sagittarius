package tools

import (
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/provider"
)

// TestDelegationPromptGate pins the AD-154 pre-dispatch gate: placeholder and
// template-marker delegation prompts are denied before any child launches,
// with a code-span exemption for literal code.
func TestDelegationPromptGate(t *testing.T) {
	t.Parallel()

	detailed := "Implement the repository layer in app/db/: connection management with WAL, " +
		"the schema, and CRUD queries, per the shared contract."

	tests := []struct {
		name     string
		prompt   string
		contract string
		wantDeny string // substring; empty means the call must run
	}{
		{"detailed prompt passes", detailed, "", ""},
		{"bare todo", "todo", "", "bare placeholder"},
		{"bare task number", "Task 3", "", "bare placeholder"},
		{"bare tbd", "  TBD  ", "", "bare placeholder"},
		{"curly marker in prose", "Implement {module_name} for the auth system", "", "unexpanded template marker"},
		{"angle marker in prose", "Write the output to <file name> and test it", "", "unexpanded template marker"},
		{"two-word curly marker", "Implement {module name} for auth", "", "unexpanded template marker"},
		{"marker in contract", detailed, "Money lives in {amount field}", "unexpanded template marker"},
		{"marker inside inline code", "Write `func render(u {user_name}) error` in render.go", "", ""},
		{"marker inside a fenced block", "Implement the template renderer.\n\n```\nHello {user_name}, welcome to <page title>.\n```\n\nCover it with tests.", "", ""},
		{"one-word angle bracket is prose", "Render a <div> per row and test it", "", ""},
		{"one-word curly is prose", "Use the {id} field as the key", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			args := map[string]any{
				TaskParamDescription: "probe",
				TaskParamPrompt:      tc.prompt,
			}
			if tc.contract != "" {
				args[CodeTaskParamContract] = tc.contract
			}
			got := delegationPromptProblem([]provider.ToolCall{
				{ID: "c1", Name: TaskToolName, Args: args},
			})
			if tc.wantDeny == "" && got != "" {
				t.Errorf("denied: %s", got)
			}
			if tc.wantDeny != "" && !strings.Contains(got, tc.wantDeny) {
				t.Errorf("problem = %q, want it to name %q", got, tc.wantDeny)
			}
		})
	}
}

// TestDelegationPromptGateDeniesWholeBatch: one bad call denies every subagent
// slot in the batch — a partial launch would leave the batch's contract
// half-applied.
func TestDelegationPromptGateDeniesWholeBatch(t *testing.T) {
	t.Parallel()

	stub := &codeTaskStub{}
	responses := executeForTest(t, contractScheduler(stub), []provider.ToolCall{
		{ID: "c1", Name: CodeTaskToolName, Args: map[string]any{
			TaskParamDescription: "good",
			TaskParamPrompt:      "Implement the db layer per the contract, in app/db/.",
		}},
		{ID: "c2", Name: TaskToolName, Args: map[string]any{
			TaskParamDescription: "bad",
			TaskParamPrompt:      "todo",
		}},
	})
	if stub.executed.Load() != 0 {
		t.Fatal("a child launched despite the placeholder prompt in its sibling")
	}
	if len(responses) != 2 {
		t.Fatalf("responses = %d, want both slots", len(responses))
	}
	for _, resp := range responses {
		if errText, _ := resp.Response["error"].(string); !strings.Contains(errText, "bare placeholder") {
			t.Errorf("response %s = %v, want the placeholder denial", resp.CallID, resp.Response)
		}
	}
}

// TestDelegationPromptGateSingleResearchCall: the gate applies to a single
// task call too — a template marker is broken regardless of batch size.
func TestDelegationPromptGateSingleResearchCall(t *testing.T) {
	t.Parallel()

	stub := &sleepTaskStub{}
	s := concurrencyScheduler(stub)
	responses := executeForTest(t, s, []provider.ToolCall{
		{ID: "t1", Name: TaskToolName, Args: map[string]any{
			TaskParamDescription: "research",
			TaskParamPrompt:      "Summarize {module name}'s API surface",
		}},
	})
	if stub.maxSeen.Load() != 0 {
		t.Fatal("the research child launched with a template marker in its prompt")
	}
	if errText, _ := responses[0].Response["error"].(string); !strings.Contains(errText, "unexpanded template marker") {
		t.Errorf("response = %v, want the marker denial", responses[0].Response)
	}
}
