package agent

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/session"
)

const (
	switchOriginID = "origin-session"
	switchTargetID = "target-session"
)

// switchFixture is a runner recording to an origin session, with a separate
// target session already on disk. The liveness registry is a private temp dir
// so tests never touch the real ~/.sagittarius.
type switchFixture struct {
	runner      *Runner
	chatsDir    string
	livenessDir string
	originRec   *session.Recorder
}

func newSwitchFixture(t *testing.T, livenessDir string) switchFixture {
	t.Helper()
	workDir := t.TempDir()
	chatsDir, err := session.ChatsDir(workDir)
	if err != nil {
		t.Fatalf("session.ChatsDir: %v", err)
	}

	target := session.NewRecorder(chatsDir, switchTargetID, "hash", "main")
	target.RecordUserMessage("hello from target")
	if err := target.SetScratchpad("target note"); err != nil {
		t.Fatalf("target SetScratchpad: %v", err)
	}
	if err := target.SetConstraints([]string{"do not touch the target"}); err != nil {
		t.Fatalf("target SetConstraints: %v", err)
	}
	if err := target.SetSummary("Target title"); err != nil {
		t.Fatalf("target SetSummary: %v", err)
	}

	origin := session.NewRecorder(chatsDir, switchOriginID, "hash", "main")
	origin.RecordUserMessage("message in origin")

	gen := &fakeGenerator{batches: [][]provider.StreamResponse{
		{{TextDelta: "ok"}, {Done: true}},
	}}
	runner, err := NewRunner(RunnerConfig{
		Generator:       gen,
		Model:           "test-model",
		WorkDir:         workDir,
		SessionRecorder: origin,
		Settings:        settingsForAutoTitle("off"),
		LivenessDir:     livenessDir,
		LivenessKey:     switchOriginID,
		LivenessRelease: session.Acquire(livenessDir, session.LivenessInfo{SessionID: switchOriginID}),
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	return switchFixture{runner: runner, chatsDir: chatsDir, livenessDir: livenessDir, originRec: origin}
}

// targetSelection resolves the target session the way the slash layer does.
func (f switchFixture) targetSelection(t *testing.T, ref string) *session.SelectionResult {
	t.Helper()
	res, err := session.NewSelector(f.chatsDir, f.runner.CurrentSessionID()).ResolveSession(ref)
	if err != nil {
		t.Fatalf("ResolveSession(%q): %v", ref, err)
	}
	return res
}

// TestSwitchSessionRestoresTargetAndAppendsToIt covers the core contract: the
// target's history and standing state replace the live ones, and the next turn
// lands in the target's file rather than the departing session's.
func TestSwitchSessionRestoresTargetAndAppendsToIt(t *testing.T) {
	t.Parallel()
	f := newSwitchFixture(t, t.TempDir())
	res := f.targetSelection(t, switchTargetID)

	if err := f.runner.SwitchSession(res); err != nil {
		t.Fatalf("SwitchSession: %v", err)
	}

	if got := f.runner.CurrentSessionID(); got != switchTargetID {
		t.Fatalf("CurrentSessionID = %q, want %q", got, switchTargetID)
	}
	history := f.runner.History()
	if len(history) != 1 || !historyContains(history, "hello from target") {
		t.Fatalf("history after switch = %+v, want the target's single message", history)
	}
	if got := f.runner.Scratchpad(); got != "target note" {
		t.Errorf("Scratchpad = %q, want the target's note", got)
	}
	if got := f.runner.Constraints(); len(got) != 1 || got[0] != "do not touch the target" {
		t.Errorf("Constraints = %v, want the target's constraint", got)
	}

	drainEvents(t, mustRunTurn(t, f.runner, "after switch"))

	targetRec, err := session.LoadSession(res.SessionPath)
	if err != nil {
		t.Fatalf("LoadSession(target): %v", err)
	}
	if !recordHasText(targetRec, "after switch") {
		t.Errorf("post-switch turn was not appended to the target file")
	}
	originPath := filepath.Join(f.chatsDir, originalSessionFile(t, f.chatsDir, switchOriginID))
	originRec, err := session.LoadSession(originPath)
	if err != nil {
		t.Fatalf("LoadSession(origin): %v", err)
	}
	if recordHasText(originRec, "after switch") {
		t.Errorf("post-switch turn leaked into the departing session's file")
	}
}

// TestSwitchSessionClearsDepartingStandingState: state the departing session set
// must not survive the switch when the target never set it.
func TestSwitchSessionClearsDepartingStandingState(t *testing.T) {
	t.Parallel()
	f := newSwitchFixture(t, t.TempDir())
	if err := f.runner.SetScratchpad("origin note"); err != nil {
		t.Fatalf("SetScratchpad: %v", err)
	}
	if err := f.runner.AddConstraint("origin-only constraint"); err != nil {
		t.Fatalf("AddConstraint: %v", err)
	}

	// Make a bare target with no standing state of its own.
	bare := session.NewRecorder(f.chatsDir, "bare-session", "hash", "main")
	bare.RecordUserMessage("bare prompt")
	res := f.targetSelection(t, "bare-session")

	if err := f.runner.SwitchSession(res); err != nil {
		t.Fatalf("SwitchSession: %v", err)
	}
	if got := f.runner.Scratchpad(); got != "" {
		t.Errorf("Scratchpad = %q after switch to a target with none, want empty", got)
	}
	if got := f.runner.Constraints(); len(got) != 0 {
		t.Errorf("Constraints = %v after switch to a target with none, want empty", got)
	}
}

// TestSwitchSessionMovesLivenessLock: the target becomes live and the departing
// session is released and marked clean, so a later launch sees it as abandoned
// rather than as a crash.
func TestSwitchSessionMovesLivenessLock(t *testing.T) {
	t.Parallel()
	livenessDir := t.TempDir()
	f := newSwitchFixture(t, livenessDir)

	if err := f.runner.SwitchSession(f.targetSelection(t, switchTargetID)); err != nil {
		t.Fatalf("SwitchSession: %v", err)
	}

	if state, _ := session.Probe(livenessDir, switchTargetID); state != session.ProbeLive {
		t.Errorf("target probe = %v, want ProbeLive after switch", state)
	}
	if state, _ := session.Probe(livenessDir, switchOriginID); state != session.ProbeFree {
		t.Errorf("origin probe = %v, want ProbeFree after switch", state)
	}

	originPath := filepath.Join(f.chatsDir, originalSessionFile(t, f.chatsDir, switchOriginID))
	originRec, err := session.LoadSession(originPath)
	if err != nil {
		t.Fatalf("LoadSession(origin): %v", err)
	}
	if !originRec.CleanExit {
		t.Errorf("departing session not marked clean; a deliberate switch must not look like a crash")
	}
}

// TestSwitchSessionRefusals: each refusal leaves the live session untouched and
// returns a sentinel the slash layer can map to a clear message.
func TestSwitchSessionRefusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(t *testing.T, f switchFixture) *session.SelectionResult
		wantErr error
	}{
		{
			name: "during a turn",
			setup: func(t *testing.T, f switchFixture) *session.SelectionResult {
				f.runner.turnActive.Store(true)
				return f.targetSelection(t, switchTargetID)
			},
			wantErr: ErrSwitchDuringTurn,
		},
		{
			name: "current session",
			setup: func(t *testing.T, f switchFixture) *session.SelectionResult {
				return f.targetSelection(t, switchOriginID)
			},
			wantErr: ErrSwitchToCurrent,
		},
		{
			name: "held by another process",
			setup: func(t *testing.T, f switchFixture) *session.SelectionResult {
				release := session.Acquire(f.livenessDir, session.LivenessInfo{SessionID: switchTargetID})
				if release == nil {
					t.Fatalf("test could not hold the target lock")
				}
				t.Cleanup(release)
				return f.targetSelection(t, switchTargetID)
			},
			wantErr: ErrSessionLiveElsewhere,
		},
		{
			name: "no liveness registry",
			setup: func(t *testing.T, f switchFixture) *session.SelectionResult {
				return f.targetSelection(t, switchTargetID)
			},
			wantErr: ErrSessionLockUnknown,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			livenessDir := t.TempDir()
			if tc.name == "no liveness registry" {
				livenessDir = ""
			}
			f := newSwitchFixture(t, livenessDir)
			res := tc.setup(t, f)

			err := f.runner.SwitchSession(res)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("SwitchSession err = %v, want %v", err, tc.wantErr)
			}
			if got := f.runner.CurrentSessionID(); got != switchOriginID {
				t.Errorf("refused switch moved the session to %q", got)
			}
		})
	}
}

// TestSwitchSessionRejectsNilSelection guards the input contract.
func TestSwitchSessionRejectsNilSelection(t *testing.T) {
	t.Parallel()
	f := newSwitchFixture(t, t.TempDir())
	if err := f.runner.SwitchSession(nil); err == nil {
		t.Fatal("SwitchSession(nil) = nil, want error")
	}
}

// historyContains reports whether any message carries text.
func historyContains(h []provider.Message, text string) bool {
	for _, m := range h {
		for _, p := range m.Parts {
			if strings.Contains(p.Text, text) {
				return true
			}
		}
	}
	return false
}

// recordHasText reports whether any message in a loaded session carries text.
func recordHasText(rec *session.ConversationRecord, text string) bool {
	for _, m := range rec.Messages {
		for _, p := range m.Content {
			if strings.Contains(p.Text, text) {
				return true
			}
		}
	}
	return false
}
