package prompt

import (
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/tools"
)

// allPersonalities is every persona the working-memory guidance has to reach.
// Steering that lands in only one of them is the AD-074 shape: a tool exists,
// the model is never told when to use it, and the capability is unreachable.
var allPersonalities = []Personality{
	PersonalityProgrammer,
	PersonalitySysadmin,
	PersonalityPersonalAssistant,
	PersonalityCreativeAssistant,
}

func TestScratchpadGuidanceGatedByScratchpadEnabled(t *testing.T) {
	t.Parallel()

	for _, p := range allPersonalities {
		for _, variant := range []Variant{VariantFull, VariantLite} {
			on := Build(Options{Personality: p, Variant: variant, ScratchpadEnabled: true})
			if !strings.Contains(on, tools.UpdateScratchpadToolName) {
				t.Errorf("%s/%s prompt should mention %s when ScratchpadEnabled",
					p, variant, tools.UpdateScratchpadToolName)
			}

			off := Build(Options{Personality: p, Variant: variant, ScratchpadEnabled: false})
			if strings.Contains(off, tools.UpdateScratchpadToolName) {
				t.Errorf("%s/%s prompt names %s while it is not registered; "+
					"the model would call a tool that does not exist",
					p, variant, tools.UpdateScratchpadToolName)
			}
		}
	}
}

// TestSessionSearchGuidanceIsUnconditional mirrors the registration decision:
// search_session has no toggle, so its guidance has none either.
func TestSessionSearchGuidanceIsUnconditional(t *testing.T) {
	t.Parallel()

	for _, p := range allPersonalities {
		for _, variant := range []Variant{VariantFull, VariantLite} {
			for _, scratchpad := range []bool{true, false} {
				out := Build(Options{Personality: p, Variant: variant, ScratchpadEnabled: scratchpad})
				if !strings.Contains(out, tools.SearchSessionToolName) {
					t.Errorf("%s/%s prompt (scratchpad=%v) should always mention %s",
						p, variant, scratchpad, tools.SearchSessionToolName)
				}
			}
		}
	}
}

// TestWorkingMemoryGuidanceNotDuplicated guards the AD-092 lesson: these rules
// are worded once and rendered per style, so a persona that composes both a
// shared section and its own must not state them twice.
func TestWorkingMemoryGuidanceNotDuplicated(t *testing.T) {
	t.Parallel()

	const phrase = "no longer in context"
	for _, p := range allPersonalities {
		for _, variant := range []Variant{VariantFull, VariantLite} {
			out := Build(Options{Personality: p, Variant: variant, ScratchpadEnabled: true, IsGitRepo: true})
			if got := strings.Count(out, phrase); got != 1 {
				t.Errorf("%s/%s: phrase %q appears %d times, want exactly 1", p, variant, phrase, got)
			}
		}
	}
}
