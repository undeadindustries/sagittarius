package googlechat

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	gcapi "github.com/undeadindustries/sagittarius/internal/googlechat"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

// DefaultDebounceInterval is the minimum interval between status edits to respect space quota.
const DefaultDebounceInterval = 1 * time.Second

// Config configures the Google Chat UI renderer.
type Config struct {
	SpaceID         string
	AuthorizedUsers []string
	MaxResultRunes  int
	ConfirmTimeout  time.Duration
	Debounce        time.Duration
	Client          gcapi.Client
	Subscriber      gcapi.Subscriber
	Deduplicator    *gcapi.Deduplicator
}

// UI is a ui.UI implementation that bridges Sagittarius to a Google Chat DM.
type UI struct {
	cfg Config

	mu             sync.Mutex
	app            ui.App
	liveTurns      map[*turnSession]struct{}
	remoteTurn     *turnSession
	confirmSeq     uint64
	activeConfirms map[string]chan ui.ConfirmDecision
	activeAsks     map[string]chan ui.AskAnswer

	runCancel context.CancelFunc
}

// New constructs a Google Chat UI adapter.
func New(cfg Config) *UI {
	if cfg.MaxResultRunes <= 0 {
		cfg.MaxResultRunes = 2000
	}
	if cfg.ConfirmTimeout <= 0 {
		cfg.ConfirmTimeout = 300 * time.Second
	}
	if cfg.Debounce < DefaultDebounceInterval {
		cfg.Debounce = DefaultDebounceInterval
	}
	if cfg.Deduplicator == nil {
		cfg.Deduplicator = gcapi.NewDeduplicator(1000, 1*time.Hour)
	}

	return &UI{
		cfg:            cfg,
		liveTurns:      make(map[*turnSession]struct{}),
		activeConfirms: make(map[string]chan ui.ConfirmDecision),
		activeAsks:     make(map[string]chan ui.AskAnswer),
	}
}

// turnSession tracks the state and lifecycle of one active assistant turn in Google Chat.
type turnSession struct {
	space       string
	threadName  string
	threadKey   string
	ctx         context.Context
	cancel      context.CancelFunc
	done        chan struct{}
	answerBuf   strings.Builder
	statusName  string
	lastPatch   time.Time
	patchTimer  *time.Timer
	pendingText string
	mu          sync.Mutex
}

// Run begins listening for inbound Pub/Sub events from Google Chat and dispatches them to app.
func (u *UI) Run(ctx context.Context, app ui.App) error {
	u.mu.Lock()
	u.app = app
	runCtx, runCancel := context.WithCancel(ctx)
	u.runCancel = runCancel
	subscriber := u.cfg.Subscriber
	u.mu.Unlock()

	if subscriber == nil {
		return fmt.Errorf("google chat ui: subscriber is required")
	}

	return subscriber.Receive(runCtx, func(msgCtx context.Context, ev *gcapi.Event) error {
		return u.handleInboundEvent(runCtx, ev)
	})
}

func (u *UI) handleInboundEvent(ctx context.Context, ev *gcapi.Event) error {
	if ev == nil {
		return nil
	}

	// 1. Verify space is 1:1 DM with the bot
	if !gcapi.IsSingleUserBotDm(ev.Space) {
		return nil // Drop group space events
	}
	if u.cfg.SpaceID != "" {
		cfgSpace := strings.TrimPrefix(u.cfg.SpaceID, "spaces/")
		evSpace := ""
		if ev.Space != nil {
			evSpace = strings.TrimPrefix(ev.Space.Name, "spaces/")
		}
		if evSpace != cfgSpace {
			return nil
		}
	}

	// 2. Check authorized sender on ALL events (messages and button clicks)
	if !gcapi.IsAuthorizedSender(ev.User, u.cfg.AuthorizedUsers) {
		return nil // Drop unauthorized sender events silently
	}

	switch ev.Type {
	case gcapi.EventMessage:
		if ev.Message == nil {
			return nil
		}

		// Deduplicate on message name
		if ev.Message.Name != "" && u.cfg.Deduplicator.SeenOrAdd(ev.Message.Name) {
			return nil
		}

		text := strings.TrimSpace(ev.Message.ArgumentText)
		if text == "" {
			text = strings.TrimSpace(ev.Message.Text)
		}

		// Handle /stop command directly
		if strings.EqualFold(text, "/stop") {
			stopped := u.stopActiveTurn()
			spaceName := ""
			if ev.Space != nil {
				spaceName = ev.Space.Name
			}
			msg := "Turn stopped."
			if stopped == 0 {
				msg = "No turn is currently running."
			} else if stopped > 1 {
				msg = fmt.Sprintf("Stopped %d turns.", stopped)
			}
			_, _ = u.cfg.Client.CreateMessage(ctx, spaceName, &gcapi.Message{
				Text: msg,
			})
			return nil
		}

		// Process user prompt asynchronously so subscriber loop stays responsive
		go u.dispatchInboundTurn(ctx, ev.Space, ev.Message.Thread, text)
		return nil

	case gcapi.EventCardClicked:
		if ev.Action == nil {
			return nil
		}
		u.handleCardClicked(ctx, ev)
		return nil
	}

	return nil
}

func (u *UI) stopActiveTurn() int {
	u.mu.Lock()
	turns := make([]*turnSession, 0, len(u.liveTurns))
	for t := range u.liveTurns {
		turns = append(turns, t)
	}
	u.mu.Unlock()

	for _, t := range turns {
		t.cancel()
	}
	return len(turns)
}

func (u *UI) dispatchInboundTurn(ctx context.Context, space *gcapi.Space, thread *gcapi.Thread, input string) {
	u.mu.Lock()
	app := u.app
	u.mu.Unlock()

	if app == nil {
		return
	}

	spaceName := ""
	if space != nil {
		spaceName = space.Name
	}
	if spaceName == "" {
		spaceName = u.cfg.SpaceID
	}
	if !strings.HasPrefix(spaceName, "spaces/") {
		spaceName = "spaces/" + spaceName
	}

	turnCtx, turnCancel := context.WithCancel(ctx)
	defer turnCancel()

	turn := &turnSession{
		space:  spaceName,
		ctx:    turnCtx,
		cancel: turnCancel,
		done:   make(chan struct{}),
	}
	if thread != nil {
		turn.threadName = thread.Name
		turn.threadKey = thread.ThreadKey
	}

	u.mu.Lock()
	u.liveTurns[turn] = struct{}{}
	u.mu.Unlock()

	defer func() {
		u.mu.Lock()
		delete(u.liveTurns, turn)
		u.mu.Unlock()
		close(turn.done)
	}()

	// Defer-owned status cleanup: ensure status placeholder is NEVER leaked on cancelled/silent turns
	defer u.cleanupTurnStatus(turn)

	// Post initial status placeholder message
	u.createTurnStatus(turn, "Thinking…")

	// Submit input to app
	streamCh, err := app.HandleInput(turnCtx, input)
	if err != nil {
		// If input queue is full, reply clearly
		_, _ = u.cfg.Client.CreateMessage(context.Background(), turn.space, &gcapi.Message{
			Text:   fmt.Sprintf("Cannot process prompt: %v", err),
			Thread: turn.makeThread(),
		})
		return
	}

	// Consume streaming events for this turn
	for ev := range streamCh {
		u.processTurnEvent(turn, ev)
	}

	// Post final answer on completion
	u.finalizeTurnAnswer(turn)
}

func (u *UI) cleanupTurnStatus(t *turnSession) {
	t.mu.Lock()
	if t.patchTimer != nil {
		t.patchTimer.Stop()
	}
	statusName := t.statusName
	t.statusName = ""
	t.mu.Unlock()

	if statusName != "" {
		_ = u.cfg.Client.DeleteMessage(context.Background(), statusName)
	}
}

func (u *UI) finalizeTurnAnswer(t *turnSession) {
	t.mu.Lock()
	answer := strings.TrimSpace(t.answerBuf.String())
	wasCancelled := t.ctx != nil && t.ctx.Err() != nil
	t.mu.Unlock()

	if answer != "" {
		// Split answer into chunks of <= 4000 runes if very large
		chunks := splitMessage(answer, 4000)
		for _, chunk := range chunks {
			_, _ = u.cfg.Client.CreateMessage(context.Background(), t.space, &gcapi.Message{
				Text:   chunk,
				Thread: t.makeThread(),
			})
		}
	} else if wasCancelled {
		_, _ = u.cfg.Client.CreateMessage(context.Background(), t.space, &gcapi.Message{
			Text:   "Turn canceled.",
			Thread: t.makeThread(),
		})
	}
}

func (t *turnSession) makeThread() *gcapi.Thread {
	if t.threadName != "" {
		return &gcapi.Thread{Name: t.threadName}
	}
	if t.threadKey != "" {
		return &gcapi.Thread{ThreadKey: t.threadKey}
	}
	return nil
}

func (u *UI) createTurnStatus(t *turnSession, initialText string) {
	card := &gcapi.CardV2{
		CardID: "status-card",
		Card: &gcapi.Card{
			Header: &gcapi.CardHeader{
				Title:    "Sagittarius",
				Subtitle: "Working…",
			},
			Sections: []gcapi.Section{
				{
					Widgets: []gcapi.Widget{
						{TextParagraph: &gcapi.TextParagraph{Text: initialText}},
						{
							ButtonList: &gcapi.ButtonList{
								Buttons: []gcapi.Button{
									{
										Text: "Stop",
										OnClick: &gcapi.OnClick{
											Action: &gcapi.Action{
												Function: "stop_turn",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	msg, err := u.cfg.Client.CreateMessage(t.ctx, t.space, &gcapi.Message{
		CardsV2: []gcapi.CardV2{*card},
		Thread:  t.makeThread(),
	})
	if err == nil && msg != nil {
		t.mu.Lock()
		t.statusName = msg.Name
		t.lastPatch = time.Now()
		t.mu.Unlock()
	}
}

func (u *UI) updateTurnStatus(t *turnSession, text string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.statusName == "" {
		return
	}

	now := time.Now()
	elapsed := now.Sub(t.lastPatch)

	if elapsed >= u.cfg.Debounce {
		if t.patchTimer != nil {
			t.patchTimer.Stop()
			t.patchTimer = nil
		}
		t.lastPatch = now
		go u.patchStatus(t.statusName, text)
		return
	}

	// Debounce: schedule trailing patch
	t.pendingText = text
	if t.patchTimer == nil {
		remaining := u.cfg.Debounce - elapsed
		t.patchTimer = time.AfterFunc(remaining, func() {
			t.mu.Lock()
			pending := t.pendingText
			statusName := t.statusName
			t.lastPatch = time.Now()
			t.patchTimer = nil
			t.mu.Unlock()

			if statusName != "" && pending != "" {
				u.patchStatus(statusName, pending)
			}
		})
	}
}

func (u *UI) patchStatus(statusName, text string) {
	card := &gcapi.CardV2{
		CardID: "status-card",
		Card: &gcapi.Card{
			Header: &gcapi.CardHeader{
				Title:    "Sagittarius",
				Subtitle: "Working…",
			},
			Sections: []gcapi.Section{
				{
					Widgets: []gcapi.Widget{
						{TextParagraph: &gcapi.TextParagraph{Text: text}},
						{
							ButtonList: &gcapi.ButtonList{
								Buttons: []gcapi.Button{
									{
										Text: "Stop",
										OnClick: &gcapi.OnClick{
											Action: &gcapi.Action{
												Function: "stop_turn",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	_, _ = u.cfg.Client.PatchMessage(context.Background(), statusName, &gcapi.Message{
		CardsV2: []gcapi.CardV2{*card},
	}, "cardsV2")
}

func (u *UI) processTurnEvent(t *turnSession, ev ui.StreamEvent) {
	switch ev.Type {
	case ui.StreamTextDelta:
		t.mu.Lock()
		t.answerBuf.WriteString(ev.Text)
		t.mu.Unlock()

	case ui.StreamReasoningDelta:
		u.updateTurnStatus(t, "Thinking…")

	case ui.StreamToolStart:
		label := fmt.Sprintf("Running %s…", ev.ToolName)
		u.updateTurnStatus(t, label)

	case ui.StreamToolResult:
		// Post tool result sanitized and capped
		sanitized := SanitizeToolResult(ev.Text, u.cfg.MaxResultRunes)
		if sanitized != "" {
			_, _ = u.cfg.Client.CreateMessage(context.Background(), t.space, &gcapi.Message{
				Text:   fmt.Sprintf("*Tool `%s` result:*\n```\n%s\n```", ev.ToolName, sanitized),
				Thread: t.makeThread(),
			})
		}

	case ui.StreamToolConfirm:
		u.handleToolConfirm(t, ev)

	case ui.StreamAskUser:
		u.handleAskUser(t, ev)

	case ui.StreamOpenDialog:
		// Explicitly notify that interactive dialogs are unavailable over chat
		msg := "Dialog overlays are not available over Google Chat. Use `/modes override <mode> <model>` or edit settings directly in `.sagittarius/settings.json`."
		_, _ = u.cfg.Client.CreateMessage(context.Background(), t.space, &gcapi.Message{
			Text:   msg,
			Thread: t.makeThread(),
		})

	case ui.StreamInfo:
		if ev.Text != "" {
			_, _ = u.cfg.Client.CreateMessage(context.Background(), t.space, &gcapi.Message{
				Text:   ev.Text,
				Thread: t.makeThread(),
			})
		}

	case ui.StreamError:
		if ev.Err != nil {
			_, _ = u.cfg.Client.CreateMessage(context.Background(), t.space, &gcapi.Message{
				Text:   fmt.Sprintf("⚠️ %v", ev.Err),
				Thread: t.makeThread(),
			})
		}
	}
}

func (u *UI) handleToolConfirm(t *turnSession, ev ui.StreamEvent) {
	confirmID := fmt.Sprintf("confirm-%d", atomic.AddUint64(&u.confirmSeq, 1))

	u.mu.Lock()
	u.activeConfirms[confirmID] = ev.ConfirmReply
	u.mu.Unlock()

	// Build confirmation card
	preview := ev.Text
	if ev.Diff != "" {
		preview = ev.Diff
	}
	preview = SanitizeToolResult(preview, 1000)

	card := &gcapi.CardV2{
		CardID: confirmID,
		Card: &gcapi.Card{
			Header: &gcapi.CardHeader{
				Title:    fmt.Sprintf("Approve %s", ev.ToolName),
				Subtitle: "Confirmation Required",
			},
			Sections: []gcapi.Section{
				{
					Widgets: []gcapi.Widget{
						{TextParagraph: &gcapi.TextParagraph{Text: fmt.Sprintf("```\n%s\n```", preview)}},
						{
							ButtonList: &gcapi.ButtonList{
								Buttons: []gcapi.Button{
									{
										Text: "Allow once",
										OnClick: &gcapi.OnClick{
											Action: &gcapi.Action{
												Function: "confirm_tool",
												Parameters: []gcapi.ActionParameter{
													{Key: "id", Value: confirmID},
													{Key: "decision", Value: "once"},
												},
											},
										},
									},
									{
										Text: "Allow session",
										OnClick: &gcapi.OnClick{
											Action: &gcapi.Action{
												Function: "confirm_tool",
												Parameters: []gcapi.ActionParameter{
													{Key: "id", Value: confirmID},
													{Key: "decision", Value: "session"},
												},
											},
										},
									},
									{
										Text: "Deny",
										OnClick: &gcapi.OnClick{
											Action: &gcapi.Action{
												Function: "confirm_tool",
												Parameters: []gcapi.ActionParameter{
													{Key: "id", Value: confirmID},
													{Key: "decision", Value: "deny"},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	msg, err := u.cfg.Client.CreateMessage(context.Background(), t.space, &gcapi.Message{
		CardsV2: []gcapi.CardV2{*card},
		Thread:  t.makeThread(),
	})

	cardMsgName := ""
	if err == nil && msg != nil {
		cardMsgName = msg.Name
	}

	// Timeout timer to fail closed
	time.AfterFunc(u.cfg.ConfirmTimeout, func() {
		u.mu.Lock()
		replyCh, exists := u.activeConfirms[confirmID]
		if exists {
			delete(u.activeConfirms, confirmID)
		}
		u.mu.Unlock()

		if exists && replyCh != nil {
			replyCh <- ui.ConfirmDeny
			if cardMsgName != "" {
				_, _ = u.cfg.Client.PatchMessage(context.Background(), cardMsgName, &gcapi.Message{
					Text: fmt.Sprintf("⏱️ Confirmation for `%s` timed out. Denied.", ev.ToolName),
				}, "text")
			}
		}
	})
}

func (u *UI) handleAskUser(t *turnSession, ev ui.StreamEvent) {
	askID := fmt.Sprintf("ask-%d", atomic.AddUint64(&u.confirmSeq, 1))

	u.mu.Lock()
	u.activeAsks[askID] = ev.AskReply
	u.mu.Unlock()

	var buttons []gcapi.Button
	for i, opt := range ev.AskOptions {
		label := opt.Label
		if i == ev.AskRecommended {
			label += " (Recommended)"
		}
		buttons = append(buttons, gcapi.Button{
			Text: label,
			OnClick: &gcapi.OnClick{
				Action: &gcapi.Action{
					Function: "ask_user",
					Parameters: []gcapi.ActionParameter{
						{Key: "id", Value: askID},
						{Key: "index", Value: fmt.Sprintf("%d", i)},
						{Key: "text", Value: opt.Label},
					},
				},
			},
		})
	}

	card := &gcapi.CardV2{
		CardID: askID,
		Card: &gcapi.Card{
			Header: &gcapi.CardHeader{
				Title: "Question",
			},
			Sections: []gcapi.Section{
				{
					Header: ev.AskQuestion,
					Widgets: []gcapi.Widget{
						{ButtonList: &gcapi.ButtonList{Buttons: buttons}},
					},
				},
			},
		},
	}

	msg, err := u.cfg.Client.CreateMessage(context.Background(), t.space, &gcapi.Message{
		CardsV2: []gcapi.CardV2{*card},
		Thread:  t.makeThread(),
	})

	cardMsgName := ""
	if err == nil && msg != nil {
		cardMsgName = msg.Name
	}

	// Timeout to send default
	time.AfterFunc(u.cfg.ConfirmTimeout, func() {
		u.mu.Lock()
		replyCh, exists := u.activeAsks[askID]
		if exists {
			delete(u.activeAsks, askID)
		}
		u.mu.Unlock()

		if exists && replyCh != nil {
			defaultText := ""
			if ev.AskRecommended >= 0 && ev.AskRecommended < len(ev.AskOptions) {
				defaultText = ev.AskOptions[ev.AskRecommended].Label
			}
			replyCh <- ui.AskAnswer{Index: ev.AskRecommended, Text: defaultText}
			if cardMsgName != "" {
				_, _ = u.cfg.Client.PatchMessage(context.Background(), cardMsgName, &gcapi.Message{
					Text: fmt.Sprintf("⏱️ Question timed out. Selected: %s", defaultText),
				}, "text")
			}
		}
	})
}

func (u *UI) handleCardClicked(ctx context.Context, ev *gcapi.Event) {
	action := ev.Action
	params := action.ParameterMap()

	switch action.ActionMethodName {
	case "stop_turn":
		u.stopActiveTurn()
		if ev.Message != nil && ev.Message.Name != "" {
			_, _ = u.cfg.Client.PatchMessage(ctx, ev.Message.Name, &gcapi.Message{
				Text: "Turn stopped by user.",
			}, "text")
		}

	case "confirm_tool":
		id := params["id"]
		decisionStr := params["decision"]

		u.mu.Lock()
		replyCh, exists := u.activeConfirms[id]
		if exists {
			delete(u.activeConfirms, id)
		}
		u.mu.Unlock()

		if !exists || replyCh == nil {
			return
		}

		var decision ui.ConfirmDecision
		var label string
		switch decisionStr {
		case "once":
			decision = ui.ConfirmOnce
			label = "Approved once"
		case "session":
			decision = ui.ConfirmSession
			label = "Approved for session"
		default:
			decision = ui.ConfirmDeny
			label = "Denied"
		}

		replyCh <- decision

		if ev.Message != nil && ev.Message.Name != "" {
			_, _ = u.cfg.Client.PatchMessage(ctx, ev.Message.Name, &gcapi.Message{
				Text: fmt.Sprintf("Decision: **%s**", label),
			}, "text")
		}

	case "ask_user":
		id := params["id"]
		text := params["text"]

		u.mu.Lock()
		replyCh, exists := u.activeAsks[id]
		if exists {
			delete(u.activeAsks, id)
		}
		u.mu.Unlock()

		if !exists || replyCh == nil {
			return
		}

		replyCh <- ui.AskAnswer{Index: -1, Text: text}

		if ev.Message != nil && ev.Message.Name != "" {
			_, _ = u.cfg.Client.PatchMessage(ctx, ev.Message.Name, &gcapi.Message{
				Text: fmt.Sprintf("Selected: **%s**", text),
			}, "text")
		}
	}
}

// RenderStream handles streaming events when turns originate remotely (e.g. from TUI).
func (u *UI) RenderStream(delta ui.StreamEvent) error {
	spaceName := u.cfg.SpaceID
	if spaceName == "" {
		return nil
	}
	if !strings.HasPrefix(spaceName, "spaces/") {
		spaceName = "spaces/" + spaceName
	}

	u.mu.Lock()
	if u.remoteTurn == nil {
		u.remoteTurn = &turnSession{
			space: spaceName,
			done:  make(chan struct{}),
		}
	}
	turn := u.remoteTurn
	u.mu.Unlock()

	// Route through processTurnEvent for shared text accumulation, tool-result redaction/posting, and cards.
	u.processTurnEvent(turn, delta)

	if delta.Type == ui.StreamDone {
		u.finalizeTurnAnswer(turn)
		u.mu.Lock()
		if u.remoteTurn == turn {
			u.remoteTurn = nil
		}
		u.mu.Unlock()
	}

	return nil
}

// SetStatus updates status (no-op for chat UI).
func (u *UI) SetStatus(status ui.StatusBar) error {
	return nil
}

// ShowError displays a top-level error.
func (u *UI) ShowError(err error) error {
	if err == nil || u.cfg.SpaceID == "" {
		return nil
	}
	space := u.cfg.SpaceID
	if !strings.HasPrefix(space, "spaces/") {
		space = "spaces/" + space
	}
	_, _ = u.cfg.Client.CreateMessage(context.Background(), space, &gcapi.Message{
		Text: fmt.Sprintf("⚠️ %v", err),
	})
	return nil
}

// PromptInput returns ErrNotRunning as Google Chat is an event-driven subscriber.
func (u *UI) PromptInput() (string, error) {
	return "", ui.ErrNotRunning
}

// splitMessage splits text into chunks of at most maxChars runes.
func splitMessage(text string, maxChars int) []string {
	if maxChars <= 0 {
		maxChars = 4000
	}
	runes := []rune(text)
	if len(runes) <= maxChars {
		return []string{text}
	}

	var chunks []string
	for len(runes) > 0 {
		if len(runes) <= maxChars {
			chunks = append(chunks, string(runes))
			break
		}
		chunks = append(chunks, string(runes[:maxChars]))
		runes = runes[maxChars:]
	}
	return chunks
}
