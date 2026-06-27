package agent

import (
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

// MapStreamResponse converts one provider chunk to zero or more UI stream events.
func MapStreamResponse(resp provider.StreamResponse) []ui.StreamEvent {
	if resp.Error != nil {
		return []ui.StreamEvent{{Type: ui.StreamError, Err: resp.Error}}
	}

	var events []ui.StreamEvent
	if resp.ReasoningDelta != "" {
		events = append(events, ui.StreamEvent{
			Type: ui.StreamReasoningDelta,
			Text: resp.ReasoningDelta,
		})
	}
	if resp.TextDelta != "" {
		events = append(events, ui.StreamEvent{
			Type: ui.StreamTextDelta,
			Text: resp.TextDelta,
		})
	}
	// Tool calls are NOT turned into StreamToolStart here: the scheduler emits
	// the canonical start (carrying the argument summary and call id) when it
	// begins executing each call. Emitting one here too produced a duplicate
	// "⚙ tool" line per invocation in both the TUI and headless stream-json.
	if resp.Done {
		events = append(events, ui.StreamEvent{Type: ui.StreamDone})
	}
	return events
}
