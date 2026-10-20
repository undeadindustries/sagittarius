package prompt

import (
	"strings"
	"testing"
)

func TestScopeAndDependencyRules(t *testing.T) {
	t.Parallel()
	tests := []struct {
		personality Personality
		variant     Variant
		want        []string
	}{
		{PersonalityProgrammer, VariantFull, []string{"## Scope", "**Dependencies:** " + dependencyRule,
			"Three similar lines are better", "Validate input where it enters the system", mentionOtherFixesRule}},
		{PersonalityProgrammer, VariantLite, []string{"## Scope", dependencyRule, mentionOtherFixesRule}},
		{PersonalitySysadmin, VariantFull, []string{sysadminDependencyRule, mentionOtherFixesRule}},
		{PersonalitySysadmin, VariantLite, []string{sysadminDependencyRule, "Change only what the request needs. " + mentionOtherFixesRule}},
	}
	for _, tt := range tests {
		t.Run(string(tt.personality)+"/"+string(tt.variant), func(t *testing.T) {
			t.Parallel()
			out := Build(Options{Personality: tt.personality, Variant: tt.variant})
			for _, w := range tt.want {
				if !strings.Contains(out, w) {
					t.Errorf("missing %q", w)
				}
			}
			if strings.Contains(out, "just-in-case") {
				t.Error("the speculative just-in-case clause must be gone")
			}
		})
	}
}

// TestProgrammerLiteScopeFollowsWorkflow pins placement: scope rules sit
// between the workflow and the editing rules they qualify.
func TestProgrammerLiteScopeFollowsWorkflow(t *testing.T) {
	t.Parallel()
	out := Build(Options{Personality: PersonalityProgrammer, Variant: VariantLite})
	workflow := strings.Index(out, "## Workflow")
	scope := strings.Index(out, "## Scope")
	edit := strings.Index(out, "## Editing Rules")
	if workflow < 0 || scope < 0 || edit < 0 || workflow >= scope || scope >= edit {
		t.Fatalf("want Workflow < Scope < Editing Rules, got %d, %d, %d", workflow, scope, edit)
	}
}
