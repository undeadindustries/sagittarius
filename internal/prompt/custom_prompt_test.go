package prompt

import (
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/tools"
)

func TestBuildCustomBody(t *testing.T) {
	t.Parallel()
	const body = "You are Atlas, a database reviewer. Flag every unindexed query."
	got := Build(Options{
		Personality:       PersonalitySysadmin,
		Identity:          Identity{Model: "gpt-4o", ProviderName: "OpenRouter"},
		CustomBody:        "\n" + body + "\n",
		EditEnabled:       true,
		ScratchpadEnabled: true,
		SkillsAvailable:   true,
		Environment:       &Environment{WorkDir: "/work"},
	})

	if !strings.HasPrefix(got, body) {
		t.Fatalf("custom body is not first:\n%s", got[:min(len(got), 200)])
	}
	if strings.Contains(got, "You are Sagittarius,") {
		t.Error("custom prompt must not include the built-in identity line")
	}
	if strings.Contains(got, sysadminProfile.helpClause) {
		t.Error("custom prompt must ignore the configured personality")
	}

	ordered := []string{
		body,
		"identify yourself accurately as gpt-4o (via OpenRouter)",
		"## Skills",
		"## Shell Commands",
		"## Tool Usage",
		"`" + tools.UpdateScratchpadToolName + "`",
		"**Questions are not tasks.**",
		"## Environment",
		"## Presenting Commands to the User",
	}
	last := -1
	for _, want := range ordered {
		i := strings.Index(got, want)
		if i < 0 {
			t.Fatalf("missing %q in custom prompt", want)
		}
		if i <= last {
			t.Fatalf("%q is out of order", want)
		}
		last = i
	}

	questions := strings.Index(got, "**Questions are not tasks.**")
	env := strings.Index(got, "## Environment")
	if strings.Contains(got[questions:env], "## ") {
		t.Error("the questions rule must be the last section before Environment")
	}
}

func TestBuildCustomBodyBlankFallsBackToPersonality(t *testing.T) {
	t.Parallel()
	got := Build(Options{Personality: PersonalitySysadmin, CustomBody: "  \n "})
	if !strings.Contains(got, sysadminProfile.helpClause) {
		t.Fatal("blank CustomBody should build the personality prompt")
	}
}

func TestBuildCustomBodyOmitsSkillsWhenUnavailable(t *testing.T) {
	t.Parallel()
	got := Build(Options{CustomBody: "Custom."})
	if strings.Contains(got, "## Skills") {
		t.Fatal("skills guidance should be gated on SkillsAvailable")
	}
	if !strings.Contains(got, "answer honestly based on your own knowledge") {
		t.Fatal("unknown model should get the generic honesty sentence")
	}
}
