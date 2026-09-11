package googlechat

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"cloud.google.com/go/pubsub/v2"
	"google.golang.org/api/option"
)

// Subscriber defines the interface for receiving inbound Google Chat events.
type Subscriber interface {
	Receive(ctx context.Context, handler func(ctx context.Context, ev *Event) error) error
}

// DecodeEvent parses a Google Chat interaction event from raw JSON bytes.
// Handles standard Google Chat event payloads and wrapped event structures.
func DecodeEvent(data []byte) (*Event, error) {
	var ev Event
	if err := json.Unmarshal(data, &ev); err != nil {
		return nil, fmt.Errorf("decode chat event: %w", err)
	}

	// If standard event fields are present, return directly.
	if ev.Type != "" || ev.Space != nil || ev.Message != nil || ev.Action != nil {
		return &ev, nil
	}

	// Handle possible workspace events envelope where payload is in chat.messagePayload
	var envelope struct {
		Event *Event `json:"event"`
		Chat  *struct {
			MessagePayload *Event `json:"messagePayload"`
		} `json:"chat"`
	}
	if err := json.Unmarshal(data, &envelope); err == nil {
		if envelope.Event != nil {
			return envelope.Event, nil
		}
		if envelope.Chat != nil && envelope.Chat.MessagePayload != nil {
			return envelope.Chat.MessagePayload, nil
		}
	}

	return &ev, nil
}

// PubSubSubscriber receives Google Chat events via Google Cloud Pub/Sub pull subscription.
type PubSubSubscriber struct {
	projectID       string
	subscriptionID  string
	credentialsFile string
}

// NewPubSubSubscriber constructs a PubSubSubscriber.
func NewPubSubSubscriber(projectID, subscriptionID, credentialsFile string) *PubSubSubscriber {
	return &PubSubSubscriber{
		projectID:       projectID,
		subscriptionID:  subscriptionID,
		credentialsFile: credentialsFile,
	}
}

// Receive starts a pull loop on the Pub/Sub subscription and passes decoded events to handler.
func (s *PubSubSubscriber) Receive(ctx context.Context, handler func(ctx context.Context, ev *Event) error) error {
	if s.projectID == "" || s.subscriptionID == "" {
		return fmt.Errorf("pubsub subscriber: projectID and subscriptionID are required")
	}

	var opts []option.ClientOption
	if s.credentialsFile != "" {
		// WithAuthCredentialsFile pins the credential type to service account;
		// plain WithCredentialsFile is deprecated because it loads any
		// credential configuration without validating its type.
		opts = append(opts, option.WithAuthCredentialsFile(option.ServiceAccount, ExpandPath(s.credentialsFile)))
	}

	client, err := pubsub.NewClient(ctx, s.projectID, opts...)
	if err != nil {
		return fmt.Errorf("create pubsub client: %w", err)
	}
	defer func() { _ = client.Close() }()

	sub := client.Subscriber(s.subscriptionID)

	err = sub.Receive(ctx, func(msgCtx context.Context, msg *pubsub.Message) {
		ev, decErr := DecodeEvent(msg.Data)
		if decErr != nil {
			// Malformed message: nack or ack to avoid poison pill
			msg.Nack()
			return
		}

		if handleErr := handler(msgCtx, ev); handleErr != nil {
			msg.Nack()
			return
		}
		msg.Ack()
	})

	if err != nil && ctx.Err() == nil {
		return fmt.Errorf("pubsub receive: %w", err)
	}
	return nil
}

// FakeSubscriber is an in-memory test double for Subscriber.
type FakeSubscriber struct {
	mu     sync.Mutex
	events chan *Event
	err    error
}

// NewFakeSubscriber creates a FakeSubscriber with a buffered event channel.
func NewFakeSubscriber() *FakeSubscriber {
	return &FakeSubscriber{
		events: make(chan *Event, 50),
	}
}

// Push sends an event to the subscriber.
func (f *FakeSubscriber) Push(ev *Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events <- ev
}

// SetError injects an error to be returned by Receive.
func (f *FakeSubscriber) SetError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

// Close closes the event channel.
func (f *FakeSubscriber) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	close(f.events)
}

// Receive consumes events from the in-memory channel until context cancellation or close.
func (f *FakeSubscriber) Receive(ctx context.Context, handler func(ctx context.Context, ev *Event) error) error {
	f.mu.Lock()
	if f.err != nil {
		err := f.err
		f.mu.Unlock()
		return err
	}
	ch := f.events
	f.mu.Unlock()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-ch:
			if !ok {
				return nil
			}
			if err := handler(ctx, ev); err != nil {
				return err
			}
		}
	}
}
