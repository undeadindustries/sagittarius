package googlechat

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	gcapi "github.com/undeadindustries/sagittarius/internal/googlechat"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

type fakeApp struct {
	mu        sync.Mutex
	inputs    []string
	handlerFn func(ctx context.Context, input string) (<-chan ui.StreamEvent, error)
}

func (a *fakeApp) HandleInput(ctx context.Context, input string) (<-chan ui.StreamEvent, error) {
	a.mu.Lock()
	a.inputs = append(a.inputs, input)
	fn := a.handlerFn
	a.mu.Unlock()

	if fn != nil {
		return fn(ctx, input)
	}

	ch := make(chan ui.StreamEvent, 3)
	ch <- ui.StreamEvent{Type: ui.StreamReasoningDelta, Text: "thinking..."}
	ch <- ui.StreamEvent{Type: ui.StreamTextDelta, Text: "Hello from Sagittarius!"}
	ch <- ui.StreamEvent{Type: ui.StreamDone}
	close(ch)
	return ch, nil
}

func TestGoogleChatUI_TurnLifecycleAndFinalAnswer(t *testing.T) {
	fakeClient := gcapi.NewFakeClient()
	fakeSub := gcapi.NewFakeSubscriber()

	cfg := Config{
		SpaceID:         "spaces/DM123",
		AuthorizedUsers: []string{"users/user1", "user1@example.com"},
		Client:          fakeClient,
		Subscriber:      fakeSub,
		Debounce:        10 * time.Millisecond,
	}
	u := New(cfg)

	app := &fakeApp{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- u.Run(ctx, app)
	}()

	// Send an authorized user message
	ev := &gcapi.Event{
		Type: gcapi.EventMessage,
		Space: &gcapi.Space{
			Name:            "spaces/DM123",
			SingleUserBotDm: true,
		},
		User: &gcapi.User{
			Name:  "users/user1",
			Email: "user1@example.com",
		},
		Message: &gcapi.Message{
			Name: "spaces/DM123/messages/msg-1",
			Text: "hello agent",
		},
	}
	fakeSub.Push(ev)

	// Wait for turn to finish processing
	time.Sleep(100 * time.Millisecond)

	deleted := fakeClient.DeletedMessages()
	created := fakeClient.CreatedMessages()

	// 1. Status placeholder was created and then DELETED in defer
	if len(deleted) == 0 {
		t.Fatalf("expected status message to be deleted in defer cleanup, but deleted count is 0")
	}

	// 2. Final answer message was posted
	foundAnswer := false
	for _, m := range created {
		if strings.Contains(m.Text, "Hello from Sagittarius!") {
			foundAnswer = true
			break
		}
	}
	if !foundAnswer {
		t.Fatalf("expected final answer message with 'Hello from Sagittarius!', got created: %+v", created)
	}
}

func TestGoogleChatUI_CancelledAndSilentTurnsCleanUpPlaceholder(t *testing.T) {
	fakeClient := gcapi.NewFakeClient()
	fakeSub := gcapi.NewFakeSubscriber()

	cfg := Config{
		SpaceID:         "spaces/DM123",
		AuthorizedUsers: []string{"users/user1"},
		Client:          fakeClient,
		Subscriber:      fakeSub,
		Debounce:        10 * time.Millisecond,
	}
	u := New(cfg)

	turnStarted := make(chan struct{})
	allowFinish := make(chan struct{})

	app := &fakeApp{
		handlerFn: func(ctx context.Context, input string) (<-chan ui.StreamEvent, error) {
			close(turnStarted)
			ch := make(chan ui.StreamEvent, 1)
			go func() {
				select {
				case <-ctx.Done():
				case <-allowFinish:
				}
				// Emit no text (silent/cancelled)
				ch <- ui.StreamEvent{Type: ui.StreamDone}
				close(ch)
			}()
			return ch, nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = u.Run(ctx, app)
	}()

	ev := &gcapi.Event{
		Type: gcapi.EventMessage,
		Space: &gcapi.Space{
			Name:            "spaces/DM123",
			SingleUserBotDm: true,
		},
		User: &gcapi.User{
			Name: "users/user1",
		},
		Message: &gcapi.Message{
			Name: "spaces/DM123/messages/msg-silent",
			Text: "do something silent",
		},
	}
	fakeSub.Push(ev)

	<-turnStarted

	// Cancel the turn via /stop
	stopEv := &gcapi.Event{
		Type: gcapi.EventMessage,
		Space: &gcapi.Space{
			Name:            "spaces/DM123",
			SingleUserBotDm: true,
		},
		User: &gcapi.User{
			Name: "users/user1",
		},
		Message: &gcapi.Message{
			Name: "spaces/DM123/messages/msg-stop",
			Text: "/stop",
		},
	}
	fakeSub.Push(stopEv)

	time.Sleep(100 * time.Millisecond)

	deleted := fakeClient.DeletedMessages()
	created := fakeClient.CreatedMessages()

	// Verify status message was deleted
	if len(deleted) == 0 {
		t.Fatalf("expected status placeholder to be deleted even on cancelled/silent turn")
	}

	// Verify "Turn canceled." was posted
	foundCancel := false
	for _, m := range created {
		if strings.Contains(m.Text, "Turn canceled.") || strings.Contains(m.Text, "Turn stopped.") {
			foundCancel = true
			break
		}
	}
	if !foundCancel {
		t.Errorf("expected 'Turn canceled.' or 'Turn stopped.' message, got created: %+v", created)
	}
}

func TestGoogleChatUI_ToolResultSanitizedAndCapped(t *testing.T) {
	fakeClient := gcapi.NewFakeClient()
	fakeSub := gcapi.NewFakeSubscriber()

	cfg := Config{
		SpaceID:         "spaces/DM123",
		AuthorizedUsers: []string{"users/user1"},
		Client:          fakeClient,
		Subscriber:      fakeSub,
		MaxResultRunes:  50,
	}
	u := New(cfg)

	app := &fakeApp{
		handlerFn: func(ctx context.Context, input string) (<-chan ui.StreamEvent, error) {
			ch := make(chan ui.StreamEvent, 3)
			ch <- ui.StreamEvent{
				Type:     ui.StreamToolStart,
				ToolName: "fetch_credentials",
			}
			ch <- ui.StreamEvent{
				Type:     ui.StreamToolResult,
				ToolName: "fetch_credentials",
				Text:     "sk-123456789012345678901234567890 this is a very long response that exceeds the 50 characters limit easily",
			}
			ch <- ui.StreamEvent{Type: ui.StreamDone}
			close(ch)
			return ch, nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = u.Run(ctx, app)
	}()

	fakeSub.Push(&gcapi.Event{
		Type:  gcapi.EventMessage,
		Space: &gcapi.Space{Name: "spaces/DM123", SingleUserBotDm: true},
		User:  &gcapi.User{Name: "users/user1"},
		Message: &gcapi.Message{
			Name: "spaces/DM123/messages/msg-tool",
			Text: "run tool",
		},
	})

	time.Sleep(100 * time.Millisecond)

	created := fakeClient.CreatedMessages()

	foundToolResult := false
	for _, m := range created {
		if strings.Contains(m.Text, "Tool `fetch_credentials` result:") {
			foundToolResult = true
			if strings.Contains(m.Text, "sk-1234567890") {
				t.Errorf("secret sk- was NOT redacted: %s", m.Text)
			}
			if !strings.Contains(m.Text, "[REDACTED API KEY]") {
				t.Errorf("missing [REDACTED API KEY]: %s", m.Text)
			}
			if !strings.Contains(m.Text, "[... truncated") {
				t.Errorf("missing truncation notice: %s", m.Text)
			}
		}
	}
	if !foundToolResult {
		t.Fatalf("tool result message was not created: %+v", created)
	}
}

func TestGoogleChatUI_StreamOpenDialogRefusal(t *testing.T) {
	fakeClient := gcapi.NewFakeClient()
	fakeSub := gcapi.NewFakeSubscriber()

	cfg := Config{
		SpaceID:         "spaces/DM123",
		AuthorizedUsers: []string{"users/user1"},
		Client:          fakeClient,
		Subscriber:      fakeSub,
	}
	u := New(cfg)

	app := &fakeApp{
		handlerFn: func(ctx context.Context, input string) (<-chan ui.StreamEvent, error) {
			ch := make(chan ui.StreamEvent, 2)
			ch <- ui.StreamEvent{
				Type:   ui.StreamOpenDialog,
				Dialog: ui.DialogModes,
			}
			ch <- ui.StreamEvent{Type: ui.StreamDone}
			close(ch)
			return ch, nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = u.Run(ctx, app)
	}()

	fakeSub.Push(&gcapi.Event{
		Type:  gcapi.EventMessage,
		Space: &gcapi.Space{Name: "spaces/DM123", SingleUserBotDm: true},
		User:  &gcapi.User{Name: "users/user1"},
		Message: &gcapi.Message{
			Name: "spaces/DM123/messages/msg-dialog",
			Text: "/modes",
		},
	})

	time.Sleep(100 * time.Millisecond)

	created := fakeClient.CreatedMessages()

	foundRefusal := false
	for _, m := range created {
		if strings.Contains(m.Text, "Dialog overlays are not available over Google Chat") {
			foundRefusal = true
			break
		}
	}
	if !foundRefusal {
		t.Errorf("expected dialog refusal message, got: %+v", created)
	}
}

func TestGoogleChatUI_InteractiveConfirmCardAndClick(t *testing.T) {
	fakeClient := gcapi.NewFakeClient()
	fakeSub := gcapi.NewFakeSubscriber()

	cfg := Config{
		SpaceID:         "spaces/DM123",
		AuthorizedUsers: []string{"users/user1"},
		Client:          fakeClient,
		Subscriber:      fakeSub,
		ConfirmTimeout:  500 * time.Millisecond,
	}
	u := New(cfg)

	replyCh := make(chan ui.ConfirmDecision, 1)

	app := &fakeApp{
		handlerFn: func(ctx context.Context, input string) (<-chan ui.StreamEvent, error) {
			ch := make(chan ui.StreamEvent, 1)
			ch <- ui.StreamEvent{
				Type:         ui.StreamToolConfirm,
				ToolName:     "write_file",
				Text:         "writing to main.go",
				ConfirmReply: replyCh,
			}
			return ch, nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = u.Run(ctx, app)
	}()

	fakeSub.Push(&gcapi.Event{
		Type:  gcapi.EventMessage,
		Space: &gcapi.Space{Name: "spaces/DM123", SingleUserBotDm: true},
		User:  &gcapi.User{Name: "users/user1"},
		Message: &gcapi.Message{
			Name: "spaces/DM123/messages/msg-write",
			Text: "write code",
		},
	})

	time.Sleep(50 * time.Millisecond)

	// Verify confirm card was posted
	var cardMsg *gcapi.Message
	for _, m := range fakeClient.CreatedMessages() {
		if len(m.CardsV2) > 0 && strings.HasPrefix(m.CardsV2[0].CardID, "confirm-") {
			cardMsg = m
			break
		}
	}

	if cardMsg == nil {
		t.Fatalf("confirm card was not created")
	}

	confirmID := cardMsg.CardsV2[0].CardID

	// Simulate user clicking "Allow once" button
	clickEv := &gcapi.Event{
		Type:  gcapi.EventCardClicked,
		Space: &gcapi.Space{Name: "spaces/DM123", SingleUserBotDm: true},
		User:  &gcapi.User{Name: "users/user1"},
		Message: &gcapi.Message{
			Name: cardMsg.Name,
		},
		Action: &gcapi.FormAction{
			ActionMethodName: "confirm_tool",
			Parameters: []gcapi.ActionParameter{
				{Key: "id", Value: confirmID},
				{Key: "decision", Value: "once"},
			},
		},
	}
	fakeSub.Push(clickEv)

	select {
	case decision := <-replyCh:
		if decision != ui.ConfirmOnce {
			t.Errorf("decision = %v, want %v", decision, ui.ConfirmOnce)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("timed out waiting for ConfirmDecision")
	}
}

func TestGoogleChatUI_InteractiveConfirmTimeoutDeny(t *testing.T) {
	fakeClient := gcapi.NewFakeClient()
	fakeSub := gcapi.NewFakeSubscriber()

	cfg := Config{
		SpaceID:         "spaces/DM123",
		AuthorizedUsers: []string{"users/user1"},
		Client:          fakeClient,
		Subscriber:      fakeSub,
		ConfirmTimeout:  50 * time.Millisecond,
	}
	u := New(cfg)

	replyCh := make(chan ui.ConfirmDecision, 1)

	app := &fakeApp{
		handlerFn: func(ctx context.Context, input string) (<-chan ui.StreamEvent, error) {
			ch := make(chan ui.StreamEvent, 1)
			ch <- ui.StreamEvent{
				Type:         ui.StreamToolConfirm,
				ToolName:     "run_shell_command",
				Text:         "rm -rf /tmp/foo",
				ConfirmReply: replyCh,
			}
			return ch, nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = u.Run(ctx, app)
	}()

	fakeSub.Push(&gcapi.Event{
		Type:  gcapi.EventMessage,
		Space: &gcapi.Space{Name: "spaces/DM123", SingleUserBotDm: true},
		User:  &gcapi.User{Name: "users/user1"},
		Message: &gcapi.Message{
			Name: "spaces/DM123/messages/msg-shell",
			Text: "run command",
		},
	})

	select {
	case decision := <-replyCh:
		if decision != ui.ConfirmDeny {
			t.Errorf("decision on timeout = %v, want %v", decision, ui.ConfirmDeny)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timed out waiting for ConfirmDeny on timeout")
	}
}

func TestGoogleChatUI_SecurityRejections(t *testing.T) {
	fakeClient := gcapi.NewFakeClient()
	fakeSub := gcapi.NewFakeSubscriber()

	cfg := Config{
		SpaceID:         "spaces/DM123",
		AuthorizedUsers: []string{"users/authorized-user"},
		Client:          fakeClient,
		Subscriber:      fakeSub,
	}
	u := New(cfg)

	app := &fakeApp{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = u.Run(ctx, app)
	}()

	// 1. Group space rejected
	fakeSub.Push(&gcapi.Event{
		Type: gcapi.EventMessage,
		Space: &gcapi.Space{
			Name:            "spaces/GROUP999",
			SingleUserBotDm: false, // Group space!
		},
		User: &gcapi.User{Name: "users/authorized-user"},
		Message: &gcapi.Message{
			Name: "spaces/GROUP999/messages/msg-grp",
			Text: "hello from group",
		},
	})

	// 2. Unauthorized sender rejected
	fakeSub.Push(&gcapi.Event{
		Type: gcapi.EventMessage,
		Space: &gcapi.Space{
			Name:            "spaces/DM123",
			SingleUserBotDm: true,
		},
		User: &gcapi.User{Name: "users/attacker"},
		Message: &gcapi.Message{
			Name: "spaces/DM123/messages/msg-attack",
			Text: "steal secrets",
		},
	})

	// 3. Duplicate message rejected
	fakeSub.Push(&gcapi.Event{
		Type: gcapi.EventMessage,
		Space: &gcapi.Space{
			Name:            "spaces/DM123",
			SingleUserBotDm: true,
		},
		User: &gcapi.User{Name: "users/authorized-user"},
		Message: &gcapi.Message{
			Name: "spaces/DM123/messages/msg-dup",
			Text: "duplicate message",
		},
	})
	fakeSub.Push(&gcapi.Event{
		Type: gcapi.EventMessage,
		Space: &gcapi.Space{
			Name:            "spaces/DM123",
			SingleUserBotDm: true,
		},
		User: &gcapi.User{Name: "users/authorized-user"},
		Message: &gcapi.Message{
			Name: "spaces/DM123/messages/msg-dup",
			Text: "duplicate message",
		},
	})

	time.Sleep(100 * time.Millisecond)

	app.mu.Lock()
	inputs := append([]string(nil), app.inputs...)
	app.mu.Unlock()

	// Only 1 turn should have been processed (the first "duplicate message")
	if len(inputs) != 1 || inputs[0] != "duplicate message" {
		t.Fatalf("expected exactly 1 processed input ('duplicate message'), got %d: %v", len(inputs), inputs)
	}
}

func TestGoogleChatUI_StopControlCancelsAllLiveTurns(t *testing.T) {
	fakeClient := gcapi.NewFakeClient()
	fakeSub := gcapi.NewFakeSubscriber()

	cfg := Config{
		SpaceID:         "spaces/DM123",
		AuthorizedUsers: []string{"users/user1"},
		Client:          fakeClient,
		Subscriber:      fakeSub,
		Debounce:        10 * time.Millisecond,
	}
	u := New(cfg)

	turn1Started := make(chan struct{})
	turn2Started := make(chan struct{})
	var turn1Ctx, turn2Ctx context.Context
	var mu sync.Mutex

	app := &fakeApp{
		handlerFn: func(ctx context.Context, input string) (<-chan ui.StreamEvent, error) {
			mu.Lock()
			if turn1Ctx == nil {
				turn1Ctx = ctx
				close(turn1Started)
			} else {
				turn2Ctx = ctx
				close(turn2Started)
			}
			mu.Unlock()

			ch := make(chan ui.StreamEvent, 1)
			go func() {
				<-ctx.Done()
				ch <- ui.StreamEvent{Type: ui.StreamDone}
				close(ch)
			}()
			return ch, nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = u.Run(ctx, app)
	}()

	// Push first turn
	fakeSub.Push(&gcapi.Event{
		Type:  gcapi.EventMessage,
		Space: &gcapi.Space{Name: "spaces/DM123", SingleUserBotDm: true},
		User:  &gcapi.User{Name: "users/user1"},
		Message: &gcapi.Message{
			Name: "spaces/DM123/messages/msg-1",
			Text: "turn 1",
		},
	})
	<-turn1Started

	// Push second turn concurrently
	fakeSub.Push(&gcapi.Event{
		Type:  gcapi.EventMessage,
		Space: &gcapi.Space{Name: "spaces/DM123", SingleUserBotDm: true},
		User:  &gcapi.User{Name: "users/user1"},
		Message: &gcapi.Message{
			Name: "spaces/DM123/messages/msg-2",
			Text: "turn 2",
		},
	})
	<-turn2Started

	// Send /stop command
	fakeSub.Push(&gcapi.Event{
		Type:  gcapi.EventMessage,
		Space: &gcapi.Space{Name: "spaces/DM123", SingleUserBotDm: true},
		User:  &gcapi.User{Name: "users/user1"},
		Message: &gcapi.Message{
			Name: "spaces/DM123/messages/msg-stop",
			Text: "/stop",
		},
	})

	// Both turns should be cancelled
	select {
	case <-turn1Ctx.Done():
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("turn 1 context was not cancelled by /stop")
	}

	select {
	case <-turn2Ctx.Done():
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("turn 2 context was not cancelled by /stop")
	}

	time.Sleep(100 * time.Millisecond)

	// Check response message
	created := fakeClient.CreatedMessages()
	foundStopMsg := false
	for _, m := range created {
		if strings.Contains(m.Text, "Stopped 2 turns.") || strings.Contains(m.Text, "Turn stopped.") {
			foundStopMsg = true
			break
		}
	}
	if !foundStopMsg {
		t.Errorf("expected stopped confirmation message, got: %+v", created)
	}
}

func TestGoogleChatUI_StopWhenNoTurnRunning(t *testing.T) {
	fakeClient := gcapi.NewFakeClient()
	fakeSub := gcapi.NewFakeSubscriber()

	cfg := Config{
		SpaceID:         "spaces/DM123",
		AuthorizedUsers: []string{"users/user1"},
		Client:          fakeClient,
		Subscriber:      fakeSub,
	}
	u := New(cfg)

	app := &fakeApp{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = u.Run(ctx, app)
	}()

	fakeSub.Push(&gcapi.Event{
		Type:  gcapi.EventMessage,
		Space: &gcapi.Space{Name: "spaces/DM123", SingleUserBotDm: true},
		User:  &gcapi.User{Name: "users/user1"},
		Message: &gcapi.Message{
			Name: "spaces/DM123/messages/msg-stop-idle",
			Text: "/stop",
		},
	})

	time.Sleep(100 * time.Millisecond)

	created := fakeClient.CreatedMessages()
	foundIdleNotice := false
	for _, m := range created {
		if strings.Contains(m.Text, "No turn is currently running.") {
			foundIdleNotice = true
			break
		}
	}
	if !foundIdleNotice {
		t.Fatalf("expected 'No turn is currently running.', got created: %+v", created)
	}
}

func TestGoogleChatUI_RenderStreamTerminalMirroring(t *testing.T) {
	fakeClient := gcapi.NewFakeClient()
	fakeSub := gcapi.NewFakeSubscriber()

	cfg := Config{
		SpaceID:         "spaces/DM123",
		AuthorizedUsers: []string{"users/user1"},
		Client:          fakeClient,
		Subscriber:      fakeSub,
	}
	u := New(cfg)

	// Simulate streaming events coming from a terminal turn (teed via hub)
	events := []ui.StreamEvent{
		{Type: ui.StreamTextDelta, Text: "Terminal says: "},
		{Type: ui.StreamTextDelta, Text: "Hello from TUI!"},
		{Type: ui.StreamDone},
	}

	for _, ev := range events {
		if err := u.RenderStream(ev); err != nil {
			t.Fatalf("RenderStream failed: %v", err)
		}
	}

	created := fakeClient.CreatedMessages()
	foundAnswer := false
	for _, m := range created {
		if strings.Contains(m.Text, "Terminal says: Hello from TUI!") {
			foundAnswer = true
			break
		}
	}
	if !foundAnswer {
		t.Fatalf("expected mirrored terminal turn answer in chat, got created: %+v", created)
	}
}

func TestGoogleChatUI_RenderStreamToolConfirmNoPanic(t *testing.T) {
	fakeClient := gcapi.NewFakeClient()
	fakeSub := gcapi.NewFakeSubscriber()

	cfg := Config{
		SpaceID:         "spaces/DM123",
		AuthorizedUsers: []string{"users/user1"},
		Client:          fakeClient,
		Subscriber:      fakeSub,
		ConfirmTimeout:  50 * time.Millisecond,
	}
	u := New(cfg)

	replyCh := make(chan ui.ConfirmDecision, 1)
	confirmEvent := ui.StreamEvent{
		Type:         ui.StreamToolConfirm,
		ToolName:     "write_file",
		Text:         "file content preview",
		ConfirmReply: replyCh,
	}

	// Delivering confirm event from remote terminal must not panic and must post card
	if err := u.RenderStream(confirmEvent); err != nil {
		t.Fatalf("RenderStream failed: %v", err)
	}

	// Verify card was created
	time.Sleep(30 * time.Millisecond)
	created := fakeClient.CreatedMessages()
	foundCard := false
	for _, m := range created {
		if len(m.CardsV2) > 0 && strings.HasPrefix(m.CardsV2[0].CardID, "confirm-") {
			foundCard = true
			break
		}
	}
	if !foundCard {
		t.Fatalf("expected confirm card to be posted for remote turn, got: %+v", created)
	}

	// Wait for timeout deny
	select {
	case dec := <-replyCh:
		if dec != ui.ConfirmDeny {
			t.Errorf("got decision %v, want ConfirmDeny", dec)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("timed out waiting for ConfirmDeny")
	}
}
