package provider

import "testing"

// TestResetResponseChainDropsPreviousID pins the switch/clear contract: after a
// history swap the next Responses request must not chain onto the old
// conversation's server-side response id.
func TestResetResponseChainDropsPreviousID(t *testing.T) {
	t.Parallel()
	g := &OpenAIResponsesGenerator{}
	g.setLastResponseID("resp_old_conversation")
	if got := g.lastID(); got != "resp_old_conversation" {
		t.Fatalf("precondition: lastID = %q, want the seeded id", got)
	}

	g.ResetResponseChain()

	if got := g.lastID(); got != "" {
		t.Errorf("lastID after reset = %q, want empty", got)
	}
}
