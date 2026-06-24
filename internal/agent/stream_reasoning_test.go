package agent

import (
	"testing"

	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

func TestMapStreamResponseReasoning(t *testing.T) {
	t.Parallel()
	evs := MapStreamResponse(provider.StreamResponse{ReasoningDelta: "rationale"})
	if len(evs) != 1 {
		t.Fatalf("got %d events, want 1: %#v", len(evs), evs)
	}
	if evs[0].Type != ui.StreamReasoningDelta || evs[0].Text != "rationale" {
		t.Fatalf("unexpected event: %#v", evs[0])
	}
}

func TestMapStreamResponseReasoningAndTextSeparate(t *testing.T) {
	t.Parallel()
	evs := MapStreamResponse(provider.StreamResponse{ReasoningDelta: "why", TextDelta: "answer"})
	if len(evs) != 2 {
		t.Fatalf("got %d events, want 2: %#v", len(evs), evs)
	}
	if evs[0].Type != ui.StreamReasoningDelta || evs[1].Type != ui.StreamTextDelta {
		t.Fatalf("reasoning should precede text: %#v", evs)
	}
}
