package demo

import (
	"context"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

// EchoApp is the Phase 04 demo App — echoes user input until Phase 07 wires LLM.
type EchoApp struct{}

// HandleInput implements ui.App by echoing the user line back as a stream.
func (EchoApp) HandleInput(ctx context.Context, input string) (<-chan ui.StreamEvent, error) {
	ch := make(chan ui.StreamEvent, 2)
	go func() {
		defer close(ch)
		select {
		case <-ctx.Done():
			return
		default:
		}
		text := strings.TrimSpace(input)
		if text == "" {
			ch <- ui.StreamEvent{Type: ui.StreamDone}
			return
		}
		ch <- ui.StreamEvent{
			Type: ui.StreamTextDelta,
			Text: "Echo: " + text + "\n",
		}
		ch <- ui.StreamEvent{Type: ui.StreamDone}
	}()
	return ch, nil
}
