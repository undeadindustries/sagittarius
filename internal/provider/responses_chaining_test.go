package provider

import "testing"

// TestResponsesChainingIDIsPerInstance verifies the Responses API chaining id
// (previous_response_id) is scoped to each generator instance, not a process
// global. Two logical sessions must never chain off each other's response id.
func TestResponsesChainingIDIsPerInstance(t *testing.T) {
	t.Parallel()

	newGen := func() *OpenAIResponsesGenerator {
		g, err := NewOpenAIResponsesGenerator(OpenAIResponsesConfig{
			BaseURL:             "https://api.openai.com/v1",
			Model:               "gpt-5",
			UseResponseChaining: true,
		})
		if err != nil {
			t.Fatalf("NewOpenAIResponsesGenerator: %v", err)
		}
		return g
	}

	g1 := newGen()
	g2 := newGen()

	g1.setLastResponseID("resp_111")
	if got := g2.lastID(); got != "" {
		t.Errorf("g2 leaked g1's chaining id: got %q, want empty", got)
	}

	g2.setLastResponseID("resp_222")
	if got := g1.lastID(); got != "resp_111" {
		t.Errorf("g1 chaining id mutated by g2: got %q, want resp_111", got)
	}
	if got := g2.lastID(); got != "resp_222" {
		t.Errorf("g2 chaining id = %q, want resp_222", got)
	}

	// The error path clears only the owning instance.
	g1.setLastResponseID("")
	if got := g2.lastID(); got != "resp_222" {
		t.Errorf("clearing g1 affected g2: got %q, want resp_222", got)
	}
}
