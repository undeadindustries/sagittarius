package prompt

import (
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/tools"
)

func TestSkillsGuidanceGatedBySkillsAvailable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		personality Personality
		trigger     string
	}{
		{PersonalityProgrammer, "write or change code"},
		{PersonalitySysadmin, "run commands or change the system"},
	}
	for _, tc := range cases {
		for _, variant := range []Variant{VariantFull, VariantLite} {
			t.Run(string(tc.personality)+"/"+string(variant), func(t *testing.T) {
				on := Build(Options{Personality: tc.personality, Variant: variant, SkillsAvailable: true})
				if !strings.Contains(on, "## Skills") {
					t.Errorf("prompt should contain ## Skills when SkillsAvailable is true:\n%s", on)
				}
				if !strings.Contains(on, "Before you "+tc.trigger) {
					t.Errorf("prompt missing trigger phrase %q when SkillsAvailable is true:\n%s", tc.trigger, on)
				}
				if !strings.Contains(on, tools.ActivateSkillToolName) {
					t.Errorf("prompt should mention %s when SkillsAvailable is true", tools.ActivateSkillToolName)
				}

				off := Build(Options{Personality: tc.personality, Variant: variant, SkillsAvailable: false})
				if strings.Contains(off, "## Skills") {
					t.Errorf("prompt must not contain ## Skills when SkillsAvailable is false:\n%s", off)
				}
				if strings.Contains(off, "activate it first") {
					t.Errorf("prompt must not contain skill guidance when SkillsAvailable is false:\n%s", off)
				}
				if strings.Contains(off, tools.ActivateSkillToolName) {
					t.Errorf("prompt must not mention %s when SkillsAvailable is false:\n%s", tools.ActivateSkillToolName, off)
				}
			})
		}
	}
}
