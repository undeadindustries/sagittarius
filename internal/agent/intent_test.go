package agent

import (
	"context"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/provider"
)

// fixedAuxGenerator replays one canned response, standing in for the off-band
// classifier model so the aux-verdict parsing can be tested without a network.
type fixedAuxGenerator struct{ reply string }

func (g fixedAuxGenerator) GenerateContentStream(context.Context, *provider.GenerateRequest) (<-chan provider.StreamResponse, error) {
	ch := make(chan provider.StreamResponse, 2)
	ch <- provider.StreamResponse{TextDelta: g.reply}
	ch <- provider.StreamResponse{Done: true}
	close(ch)
	return ch, nil
}

func TestClassifyReadOnlyIntentPhrases(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  IntentVerdict
	}{
		{"plain unlock", "go ahead", IntentUnlock},
		{"unlock mid sentence", "ok, do it now please", IntentUnlock},
		{"lock", "just discuss this, don't change anything yet", IntentLock},
		{"unrelated", "what does this function return?", IntentNeutral},

		// Negated unlock phrases must not lift the gate.
		{"negated contraction", "don't go ahead with that", IntentNeutral},
		{"negated do not", "do not proceed until I say so", IntentNeutral},
		{"negated cannot", "we cannot proceed yet", IntentNeutral},
		{"negated let's not", "let's not proceed with changes", IntentNeutral},
		{"negated never", "never apply it automatically", IntentNeutral},

		// A question is a request for information, not a grant.
		{"question form", "should I proceed?", IntentNeutral},

		// Quoted text is stripped before matching.
		{"quoted unlock", `what happens if I say "go ahead"?`, IntentNeutral},

		// A negation binds only within its own sentence, so a retraction in a
		// following sentence still grants.
		{"negated then granted", "don't proceed. Actually, go ahead", IntentUnlock},
		// ...but a negation in the same clause wins, since failing closed only
		// costs the user a second, clearer "go ahead".
		{"negation same clause", "do not touch it, go ahead and just read", IntentNeutral},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyReadOnlyIntent(context.Background(), tc.input, nil); got != tc.want {
				t.Fatalf("classifyReadOnlyIntent(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

// TestClassifyReadOnlyIntentAuxVerdict is the regression test for UNLOCK being
// read as LOCK: "LOCK" is a substring of "UNLOCK", so a naive Contains check in
// the wrong order inverts every grant the aux classifier issues.
func TestClassifyReadOnlyIntentAuxVerdict(t *testing.T) {
	tests := []struct {
		reply string
		want  IntentVerdict
	}{
		{"UNLOCK", IntentUnlock},
		{"unlock", IntentUnlock},
		{" UNLOCK\n", IntentUnlock},
		{"LOCK", IntentLock},
		{"NEUTRAL", IntentNeutral},
	}

	// The input must contain a gate keyword to reach the aux generator at all,
	// and must not match either phrase table.
	const input = "how much of this would you change here"

	for _, tc := range tests {
		t.Run(tc.reply, func(t *testing.T) {
			got := classifyReadOnlyIntent(context.Background(), input, fixedAuxGenerator{reply: tc.reply})
			if got != tc.want {
				t.Fatalf("aux reply %q classified as %v, want %v", tc.reply, got, tc.want)
			}
		})
	}
}
