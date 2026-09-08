package tools

import (
	"testing"

	"github.com/undeadindustries/sagittarius/internal/modes"
)

// TestWorkingMemoryToolsAllowedInReadOnlyModes pins the admission decision. The
// scratchpad and session search touch nothing outside the session, so unlike
// save_memory they are usable while inspecting, planning, or being interrogated
// — which is exactly when a model most needs to hold state it cannot write down
// anywhere else.
func TestWorkingMemoryToolsAllowedInReadOnlyModes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		tool string
		args map[string]any
	}{
		{tool: UpdateScratchpadToolName, args: map[string]any{ScratchpadParamContent: "note"}},
		{tool: SearchSessionToolName, args: map[string]any{SearchSessionParamQuery: "port"}},
	}

	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			t.Parallel()

			for _, mode := range []modes.Mode{modes.ModeAgent, modes.ModeAsk, modes.ModePlan, modes.ModeDebug} {
				if allowed, reason := InteractionModeAllow(mode, tc.tool, tc.args, nil); !allowed {
					t.Errorf("%s mode denied %s: %s", mode, tc.tool, reason)
				}
			}
			if allowed, reason := inspectModeAllow(tc.tool, tc.args, nil); !allowed {
				t.Errorf("inspect posture denied %s: %s", tc.tool, reason)
			}
			if allowed, reason := grillModeAllow(tc.tool, tc.args, nil); !allowed {
				t.Errorf("grill mode denied %s: %s", tc.tool, reason)
			}
		})
	}
}

// TestWorkingMemoryToolsVisibleInReadOnlyModes guards the second half of AD-133:
// admitting a call the model was never told about leaves the capability
// unreachable, so the declaration filter must agree with the gate.
func TestWorkingMemoryToolsVisibleInReadOnlyModes(t *testing.T) {
	t.Parallel()

	for _, name := range []string{UpdateScratchpadToolName, SearchSessionToolName} {
		for _, mode := range []modes.Mode{modes.ModeAsk, modes.ModePlan} {
			if !ToolVisibleInMode(mode, name) {
				t.Errorf("%s is not declared in %s mode", name, mode)
			}
		}
	}
}
