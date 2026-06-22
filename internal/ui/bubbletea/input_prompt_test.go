package bubbletea

import "testing"

func TestInputPromptForMode(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"agent":  "Agent> ",
		"plan":   "Plan> ",
		"ask":    "Ask> ",
		"debug":  "Debug> ",
		"":       "Agent> ",
		" PLAN ": "Plan> ",
	}
	for in, want := range cases {
		if got := inputPromptForMode(in); got != want {
			t.Errorf("inputPromptForMode(%q) = %q, want %q", in, got, want)
		}
	}
}
