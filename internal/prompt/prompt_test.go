package prompt

import (
	"strings"
	"testing"
)

// unportedFeatures are fork prompt tokens for tools/features Sagittarius does
// not have. None must leak into the adapted prompts.
var unportedFeatures = []string{
	"write_todos",
	"enter_plan_mode",
	"Gemini CLI",
	"ask_user",
	"sub-agent",
	"subagent",
}

func TestProgrammerFullAnchors(t *testing.T) {
	t.Parallel()

	out := Build(Options{
		Personality: PersonalityProgrammer,
		Variant:     VariantFull,
		Identity:    Identity{Model: "gpt-4o", ProviderName: "OpenRouter"},
		ToolNames:   []string{"read_file", "run_shell_command"},
		Interactive: true,
		IsGitRepo:   true,
	})

	wantAnchors := []string{
		"# Core Mandates",
		"# Primary Workflow",
		"# Operational Guidelines",
		"## Git",
		"read_file",
		"write_file",
		"run_shell_command",
		"grep_search",
		"No Post-Change Summaries",
		"git rebase -i",
	}
	for _, a := range wantAnchors {
		if !strings.Contains(out, a) {
			t.Errorf("full prompt missing anchor %q", a)
		}
	}
	assertNoUnported(t, out)
}

func TestProgrammerLiteAnchors(t *testing.T) {
	t.Parallel()

	out := Build(Options{
		Personality: PersonalityProgrammer,
		Variant:     VariantLite,
		Identity:    Identity{Model: "qwen3", ProviderName: "vLLM"},
		Interactive: true,
		IsGitRepo:   false,
	})

	for _, a := range []string{"## Tool Usage", "## Workflow", "## Editing Rules", "grep_search", "git rebase -i", "npm init -y"} {
		if !strings.Contains(out, a) {
			t.Errorf("lite prompt missing anchor %q", a)
		}
	}
	// Lite is the low-context variant: it must be materially shorter than full.
	full := Build(Options{Personality: PersonalityProgrammer, Variant: VariantFull})
	if len(out) >= len(full) {
		t.Errorf("lite prompt (%d) should be shorter than full (%d)", len(out), len(full))
	}
	// Git section omitted when not a repo.
	if strings.Contains(out, "## Git") {
		t.Error("lite prompt should omit Git section when not a git repo")
	}
	assertNoUnported(t, out)
}

func TestIdentityKnownModelNamesProviderAndModel(t *testing.T) {
	t.Parallel()

	out := Build(Options{Identity: Identity{Model: "gpt-4o", ProviderName: "OpenRouter"}})
	if !strings.Contains(out, "gpt-4o") {
		t.Error("known identity should name the model")
	}
	if !strings.Contains(out, "OpenRouter") {
		t.Error("known identity should name the provider")
	}
	if !strings.Contains(out, "identify yourself accurately as gpt-4o") {
		t.Errorf("known identity should pin self-identification, got:\n%s", out)
	}
}

func TestIdentityUnknownForbidsFalseGeminiClaim(t *testing.T) {
	t.Parallel()

	for _, model := range []string{"", "local-model"} {
		out := Build(Options{Identity: Identity{Model: model}})
		if !strings.Contains(out, "Do not claim to be Google Gemini") {
			t.Errorf("model %q: unknown identity must forbid false Gemini claim, got:\n%s", model, out)
		}
	}
}

func TestStubPersonalitiesAreDistinct(t *testing.T) {
	t.Parallel()

	opts := Options{Variant: VariantFull, Identity: Identity{Model: "m"}}
	programmer := Build(withPersonality(opts, PersonalityProgrammer))

	// Each stub personality produces distinct, role-specific output.
	stubAnchors := map[Personality]string{
		PersonalitySysadmin:          "system administration assistant",
		PersonalityPersonalAssistant: "personal assistant",
		PersonalityCreativeAssistant: "creative assistant",
	}
	for p, anchor := range stubAnchors {
		out := Build(withPersonality(opts, p))
		if out == programmer {
			t.Errorf("personality %q should not reuse programmer output verbatim", p)
		}
		if !strings.Contains(out, anchor) {
			t.Errorf("personality %q missing role anchor %q", p, anchor)
		}
		assertNoUnported(t, out)
	}

	// Legacy "assistant" aliases to personal-assistant.
	if Build(withPersonality(opts, PersonalityAssistant)) != Build(withPersonality(opts, PersonalityPersonalAssistant)) {
		t.Error("legacy assistant should alias to personal-assistant output")
	}

	// Truly unknown personalities fall back to programmer.
	if got := Build(withPersonality(opts, Personality("unknown"))); got != programmer {
		t.Error("unknown personality should fall back to programmer output")
	}
}

func TestStubLiteShorterThanFull(t *testing.T) {
	t.Parallel()
	opts := Options{Identity: Identity{Model: "m"}, IsGitRepo: true}
	for _, p := range []Personality{PersonalitySysadmin, PersonalityPersonalAssistant, PersonalityCreativeAssistant} {
		full := Build(withPersonality(withVariant(opts, VariantFull), p))
		lite := Build(withPersonality(withVariant(opts, VariantLite), p))
		if len(lite) >= len(full) {
			t.Errorf("personality %q: lite (%d) should be shorter than full (%d)", p, len(lite), len(full))
		}
	}
}

func TestKnownPersonalityAndVariant(t *testing.T) {
	t.Parallel()

	for _, p := range []Personality{PersonalityProgrammer, PersonalitySysadmin, PersonalityAssistant, "PROGRAMMER", " sysadmin "} {
		if !KnownPersonality(p) {
			t.Errorf("KnownPersonality(%q) = false, want true", p)
		}
	}
	if KnownPersonality("nope") {
		t.Error("KnownPersonality(nope) = true, want false")
	}
	if !KnownVariant(VariantFull) || !KnownVariant(VariantLite) || !KnownVariant("FULL") {
		t.Error("full/lite should be known variants")
	}
	if KnownVariant("medium") {
		t.Error("medium should not be a known variant")
	}
}

func withPersonality(o Options, p Personality) Options {
	o.Personality = p
	return o
}

func withVariant(o Options, v Variant) Options {
	o.Variant = v
	return o
}

func assertNoUnported(t *testing.T, out string) {
	t.Helper()
	for _, tok := range unportedFeatures {
		if strings.Contains(out, tok) {
			t.Errorf("prompt leaked unported feature token %q", tok)
		}
	}
}
