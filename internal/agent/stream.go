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
	if resp.TextDelta != "" {
		events = append(events, ui.StreamEvent{
			Type: ui.StreamTextDelta,
			Text: resp.TextDelta,
		})
	}
	for _, call := range resp.ToolCalls {
		events = append(events, ui.StreamEvent{
			Type:     ui.StreamToolStart,
			ToolName: call.Name,
		})
	}
	if resp.Done {
		events = append(events, ui.StreamEvent{Type: ui.StreamDone})
	}
	return events
}
