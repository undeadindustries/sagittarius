package googlechat

import (
	"context"
	"testing"
	"time"
)

func TestDecodeEventStandard(t *testing.T) {
	raw := []byte(`{
		"type": "MESSAGE",
		"eventTime": "2026-09-09T05:00:00Z",
		"space": {
			"name": "spaces/DM1",
			"singleUserBotDm": true
		},
		"message": {
			"name": "spaces/DM1/messages/m1",
			"text": "create hello.txt",
			"sender": {
				"name": "users/123",
				"email": "rob@undeadindustries.com"
			}
		},
		"user": {
			"name": "users/123",
			"email": "rob@undeadindustries.com"
		}
	}`)

	ev, err := DecodeEvent(raw)
	if err != nil {
		t.Fatalf("DecodeEvent failed: %v", err)
	}

	if ev.Type != EventMessage {
		t.Errorf("got type %q, want MESSAGE", ev.Type)
	}
	if ev.Space == nil || !ev.Space.SingleUserBotDm {
		t.Errorf("expected SingleUserBotDm=true")
	}
	if ev.Message == nil || ev.Message.Text != "create hello.txt" {
		t.Errorf("expected message text 'create hello.txt'")
	}
}

func TestDecodeEventWorkspaceEventsEnvelope(t *testing.T) {
	raw := []byte(`{
		"chat": {
			"messagePayload": {
				"type": "CARD_CLICKED",
				"action": {
					"actionMethodName": "confirm_tool",
					"parameters": [
						{"key": "call_id", "value": "c1"},
						{"key": "decision", "value": "once"}
					]
				},
				"user": {
					"name": "users/123"
				}
			}
		}
	}`)

	ev, err := DecodeEvent(raw)
	if err != nil {
		t.Fatalf("DecodeEvent failed: %v", err)
	}

	if ev.Type != EventCardClicked {
		t.Errorf("got type %q, want CARD_CLICKED", ev.Type)
	}
	if ev.Action == nil || ev.Action.ActionMethodName != "confirm_tool" {
		t.Errorf("expected action confirm_tool")
	}
	params := ev.Action.ParameterMap()
	if params["decision"] != "once" {
		t.Errorf("expected parameter decision=once, got %v", params["decision"])
	}
}

func TestFakeSubscriber(t *testing.T) {
	sub := NewFakeSubscriber()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	received := make(chan *Event, 5)
	done := make(chan error, 1)

	go func() {
		done <- sub.Receive(ctx, func(ctx context.Context, ev *Event) error {
			received <- ev
			return nil
		})
	}()

	ev := &Event{Type: EventMessage}
	sub.Push(ev)

	select {
	case got := <-received:
		if got.Type != EventMessage {
			t.Errorf("got event type %v, want MESSAGE", got.Type)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for event")
	}

	sub.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Receive returned error: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for Receive to finish")
	}
}
