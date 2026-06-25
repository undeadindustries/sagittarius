package provider

import (
	"context"
	"iter"
	"testing"
	"time"

	"google.golang.org/genai"
)

// foreverStreamer yields text responses indefinitely until the consumer (the
// generator goroutine) stops iterating. It models a provider that never reaches
// a natural end, exposing any send that ignores context cancellation.
type foreverStreamer struct{}

func (foreverStreamer) GenerateContentStream(
	_ context.Context,
	_ string,
	_ []*genai.Content,
	_ *genai.GenerateContentConfig,
) iter.Seq2[*genai.GenerateContentResponse, error] {
	return func(yield func(*genai.GenerateContentResponse, error) bool) {
		for {
			resp := &genai.GenerateContentResponse{
				Candidates: []*genai.Candidate{{
					Content: &genai.Content{Parts: []*genai.Part{{Text: "tick"}}},
				}},
			}
			if !yield(resp, nil) {
				return
			}
		}
	}
}

// TestGeminiStreamStopsOnCancel asserts the Gemini adapter's producer goroutine
// returns (closing its channel) when the context is cancelled, even against a
// stream that never ends. Before the ctx-aware send fix the bare `ch <- ...`
// would block forever on a stopped consumer — a goroutine leak. The test fails
// (times out) under the old behavior and passes once every send is ctx-aware.
func TestGeminiStreamStopsOnCancel(t *testing.T) {
	t.Parallel()

	gen, err := NewGeminiGenerator(context.Background(), GeminiConfig{
		Model:    "test-model",
		Streamer: foreverStreamer{},
		// Timeout 0: only the request context can stop the stream, so this
		// isolates context-cancellation behavior from the internal deadline.
	})
	if err != nil {
		t.Fatalf("NewGeminiGenerator: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := gen.GenerateContentStream(ctx, &GenerateRequest{
		Messages: []Message{{Role: RoleUser, Parts: []Part{{Text: "hi"}}}},
	})
	if err != nil {
		cancel()
		t.Fatalf("GenerateContentStream: %v", err)
	}

	// Confirm streaming started before cancelling.
	if _, ok := <-ch; !ok {
		cancel()
		t.Fatal("expected at least one chunk before cancel")
	}
	cancel()

	// Drain remaining chunks; the channel must close promptly once the producer
	// observes the cancellation. A timeout means the goroutine leaked.
	done := make(chan struct{})
	go func() {
		for range ch {
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("gemini stream goroutine did not exit after cancel (leak)")
	}
}
