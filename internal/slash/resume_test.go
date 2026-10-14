package slash_test

import (
	"context"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/session"
	"github.com/undeadindustries/sagittarius/internal/slash"
)

// TestChatResumeDispatch covers how /chat resume and /chat load interpret their
// argument: none opens the menu, a checkpoint tag restores it, and anything
// else is treated as a session ref. Checkpoints win a clash.
func TestChatResumeDispatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		input           string
		wantDialog      slash.DialogKind
		wantCheckpoint  bool
		wantSessionRef  string
		wantErrContains string
		sessionErr      error
	}{
		{name: "bare resume opens menu", input: "/chat resume", wantDialog: slash.DialogSessionPick},
		{name: "bare load opens menu", input: "/chat load", wantDialog: slash.DialogSessionPick},
		{name: "index switches session", input: "/chat resume 1", wantSessionRef: "1"},
		{name: "full id switches session", input: "/chat resume sagittarius-42", wantSessionRef: "sagittarius-42"},
		{name: "checkpoint tag wins", input: "/chat resume alpha", wantCheckpoint: true},
		{
			name:            "unknown ref explains both lookups",
			input:           "/chat resume 9",
			wantSessionRef:  "9",
			sessionErr:      &session.ErrInvalidSessionIdentifier{Identifier: "9"},
			wantErrContains: "no checkpoint or session matches \"9\"",
		},
		{
			name:            "no sessions at all",
			input:           "/chat resume 1",
			wantSessionRef:  "1",
			sessionErr:      session.ErrNoSessions,
			wantErrContains: "no checkpoint or session matches",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			deps, _, hooks := testDeps(t, nil)
			hooks.resumeSessionErr = tc.sessionErr
			p := slash.NewProcessor()

			res := p.Process(context.Background(), tc.input, deps)

			if tc.wantErrContains != "" {
				if res.Err == nil || !strings.Contains(res.Err.Error(), tc.wantErrContains) {
					t.Fatalf("err = %v, want containing %q", res.Err, tc.wantErrContains)
				}
				return
			}
			if res.Err != nil {
				t.Fatalf("unexpected error: %v", res.Err)
			}
			if res.OpenDialog != tc.wantDialog {
				t.Errorf("OpenDialog = %q, want %q", res.OpenDialog, tc.wantDialog)
			}
			if tc.wantDialog != "" {
				if hooks.resumedRef != "" {
					t.Errorf("menu must not switch sessions, but ResumeSession(%q) ran", hooks.resumedRef)
				}
				return
			}
			if tc.wantCheckpoint {
				if hooks.resumedRef != "" {
					t.Errorf("checkpoint tag must not fall through to ResumeSession (got %q)", hooks.resumedRef)
				}
				if !res.ClearScrollback || len(res.Scrollback) == 0 {
					t.Errorf("checkpoint restore must repaint scrollback")
				}
				return
			}
			if hooks.resumedRef != tc.wantSessionRef {
				t.Errorf("ResumeSession ref = %q, want %q", hooks.resumedRef, tc.wantSessionRef)
			}
			if !res.ClearScrollback || len(res.Scrollback) == 0 {
				t.Errorf("session switch must repaint scrollback")
			}
		})
	}
}

// TestChatSaveWarnsOnNumericTag asserts that a checkpoint tag which is also a
// session index is flagged, since checkpoint-first resolution would hide that
// session from /chat resume <n>. Named tags stay silent.
func TestChatSaveWarnsOnNumericTag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		wantNote bool
	}{
		{name: "numeric tag warns", input: "/chat save 5", wantNote: true},
		{name: "named tag is silent", input: "/chat save alpha", wantNote: false},
		{name: "digits mixed with letters are silent", input: "/chat save 5a", wantNote: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			deps, _, _ := testDeps(t, nil)
			res := slash.NewProcessor().Process(context.Background(), tc.input, deps)
			if res.Err != nil {
				t.Fatalf("unexpected error: %v", res.Err)
			}
			joined := strings.Join(res.Messages, "\n")
			hasNote := strings.Contains(joined, "shadows session #")
			if hasNote != tc.wantNote {
				t.Errorf("shadow note present = %v, want %v (messages: %q)", hasNote, tc.wantNote, joined)
			}
		})
	}
}

// TestChatResumeSuggestionSubmitsOnAccept asserts the resume row in the /chat
// dropdown does not append a space. A trailing space would leave the user on
// the line instead of running the menu on Enter.
func TestChatResumeSuggestionSubmitsOnAccept(t *testing.T) {
	t.Parallel()
	reg := slash.NewProcessor().Registry()

	got := reg.Complete("/chat ", slash.Deps{})
	var found bool
	for _, s := range got.Items {
		if s.Label != "resume" {
			continue
		}
		found = true
		if s.AppendSpace {
			t.Errorf("/chat resume suggestion must not append a space (it opens the menu on Enter)")
		}
	}
	if !found {
		t.Fatalf("/chat completion missing resume")
	}
}
