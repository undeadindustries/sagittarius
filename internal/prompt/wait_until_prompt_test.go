package prompt

import (
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/tools"
)

func TestWaitUntilGuidanceGatedByWaitUntilEnabled(t *testing.T) {
	t.Parallel()
	for _, p := range []Personality{PersonalityProgrammer, PersonalitySysadmin} {
		for _, variant := range []Variant{VariantFull, VariantLite} {
			on := Build(Options{Personality: p, Variant: variant, WaitUntilEnabled: true})
			if !strings.Contains(on, tools.WaitUntilToolName) {
				t.Errorf("%s/%s prompt should mention %s when WaitUntilEnabled", p, variant, tools.WaitUntilToolName)
			}
			off := Build(Options{Personality: p, Variant: variant, WaitUntilEnabled: false})
			if strings.Contains(off, tools.WaitUntilToolName) {
				t.Errorf("%s/%s prompt should not mention %s when WaitUntilEnabled is false", p, variant, tools.WaitUntilToolName)
			}
		}
	}
}
