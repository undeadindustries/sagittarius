package slash_test

import (
	"context"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/slash"
)

// TestModeSwitchLiftsReadOnlyPosture guards the AD-107 escape hatch: an
// explicit switch into a mutating mode clears the durable posture and says so.
// Before AD-107 neither /agent nor /readonly off could clear an active gate,
// so a user could be denied every write with no reachable way out.
func TestModeSwitchLiftsReadOnlyPosture(t *testing.T) {
	t.Parallel()

	cases := []struct {
		cmd       string
		wantLift  bool
		wantAfter bool
	}{
		{cmd: "/agent", wantLift: true, wantAfter: false},
		{cmd: "/debug", wantLift: true, wantAfter: false},
		// Plan and ask are read-only anyway, so the posture must survive a
		// round trip through them rather than being silently dropped.
		{cmd: "/plan", wantLift: false, wantAfter: true},
		{cmd: "/ask", wantLift: false, wantAfter: true},
	}

	const liftedMsg = "Read-only posture lifted."

	for _, tc := range cases {
		t.Run(tc.cmd, func(t *testing.T) {
			deps, _, hooks := testDeps(t, nil)
			hooks.readOnly = true
			p := slash.NewProcessor()

			result := p.Process(context.Background(), tc.cmd, deps)
			if !result.Handled {
				t.Fatalf("%s: expected handled", tc.cmd)
			}
			if hooks.readOnly != tc.wantAfter {
				t.Fatalf("%s: read-only posture = %v, want %v", tc.cmd, hooks.readOnly, tc.wantAfter)
			}

			joined := strings.Join(result.Messages, " ")
			if got := strings.Contains(joined, liftedMsg); got != tc.wantLift {
				t.Fatalf("%s: message %q contains %q = %v, want %v", tc.cmd, joined, liftedMsg, got, tc.wantLift)
			}
		})
	}
}

// TestModeSwitchWithoutPostureStaysQuiet verifies the lift notice is not
// appended when there was no posture to lift, so the ordinary mode-switch
// message is unchanged for the common case.
func TestModeSwitchWithoutPostureStaysQuiet(t *testing.T) {
	t.Parallel()

	deps, _, hooks := testDeps(t, nil)
	p := slash.NewProcessor()

	result := p.Process(context.Background(), "/agent", deps)
	if !result.Handled {
		t.Fatal("/agent: expected handled")
	}
	if hooks.readOnly {
		t.Fatal("/agent: read-only posture set when it started clear")
	}
	if joined := strings.Join(result.Messages, " "); strings.Contains(joined, "Read-only posture") {
		t.Fatalf("/agent: message %q mentions the posture when none was active", joined)
	}
}
