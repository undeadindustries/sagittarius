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

	for _, a := range []string{"## Tool Usage", "## Workflow", "## Editing Rules", "grep_search"} {
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

func TestStubPersonalitiesFallBackToProgrammer(t *testing.T) {
	t.Parallel()

	opts := Options{Variant: VariantFull, Identity: Identity{Model: "m"}}
	programmer := Build(withPersonality(opts, PersonalityProgrammer))
	for _, p := range []Personality{PersonalitySysadmin, PersonalityAssistant, Personality("unknown")} {
		if got := Build(withPersonality(opts, p)); got != programmer {
			t.Errorf("personality %q should fall back to programmer output", p)
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

func assertNoUnported(t *testing.T, out string) {
	t.Helper()
	for _, tok := range unportedFeatures {
		if strings.Contains(out, tok) {
			t.Errorf("prompt leaked unported feature token %q", tok)
		}
	}
}
