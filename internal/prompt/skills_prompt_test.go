package prompt

import (
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/tools"
)

func TestSkillsGuidanceGatedBySkillsAvailable(t *testing.T) {
	t.Parallel()
	personalities := []Personality{
		PersonalityProgrammer,
		PersonalitySysadmin,
		PersonalityPersonalAssistant,
		PersonalityCreativeAssistant,
	}
	for _, p := range personalities {
		for _, variant := range []Variant{VariantFull, VariantLite} {
			t.Run(string(p)+"/"+string(variant), func(t *testing.T) {
				on := Build(Options{Personality: p, Variant: variant, SkillsAvailable: true})
				if !strings.Contains(on, "## Skills") {
					t.Errorf("prompt should contain ## Skills when SkillsAvailable is true:\n%s", on)
				}
				if !strings.Contains(on, "Before your first tool call") {
					t.Errorf("prompt should ask for the skill check as the first step:\n%s", on)
				}

				off := Build(Options{Personality: p, Variant: variant, SkillsAvailable: false})
				if strings.Contains(off, "## Skills") {
					t.Errorf("prompt must not contain ## Skills when SkillsAvailable is false:\n%s", off)
				}
				if strings.Contains(off, tools.ActivateSkillToolName) {
					t.Errorf("prompt must not mention %s when SkillsAvailable is false:\n%s", tools.ActivateSkillToolName, off)
				}
			})
		}
	}
}

// TestSkillsGuidanceComesBeforeWorkflow pins the placement: at high
// temperature a model that reads files first tends to edit straight away, so
// the skill check has to be read before the workflow's research step.
func TestSkillsGuidanceComesBeforeWorkflow(t *testing.T) {
	t.Parallel()
	cases := []struct {
		personality Personality
		variant     Variant
		workflow    string
		toolUsage   string
	}{
		{PersonalityProgrammer, VariantFull, "# Primary Workflow", ""},
		{PersonalityProgrammer, VariantLite, "## Workflow", liteToolUsageMarker},
		{PersonalitySysadmin, VariantFull, "# Primary Workflow", ""},
		{PersonalitySysadmin, VariantLite, "## Workflow", "## Tool Usage"},
		{PersonalityPersonalAssistant, VariantFull, "## Workflow", ""},
	}
	for _, tc := range cases {
		t.Run(string(tc.personality)+"/"+string(tc.variant), func(t *testing.T) {
			out := Build(Options{Personality: tc.personality, Variant: tc.variant, SkillsAvailable: true})
			skills := strings.Index(out, "## Skills")
			workflow := strings.Index(out, tc.workflow)
			if skills < 0 || workflow < 0 {
				t.Fatalf("missing section: skills=%d workflow=%d\n%s", skills, workflow, out)
			}
			if skills > workflow {
				t.Errorf("## Skills (at %d) must come before %q (at %d)", skills, tc.workflow, workflow)
			}
			if tc.toolUsage != "" {
				toolUsage := strings.Index(out, tc.toolUsage)
				if toolUsage < 0 {
					t.Fatalf("missing tool usage %q\n%s", tc.toolUsage, out)
				}
				if skills > toolUsage {
					t.Errorf("## Skills (at %d) must come before %q (at %d)", skills, tc.toolUsage, toolUsage)
				}
			}
		})
	}
}

// liteToolUsageMarker is the opening phrase of liteToolUsage, used to locate
// that section in a built prompt.
const liteToolUsageMarker = "**Search before reading.**"
