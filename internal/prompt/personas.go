package prompt

// Persona registry and stub assembly live here. Each personality's identity
// metadata (roleNoun, helpClause) is defined below; stub personalities also
// carry role bullets assembled by personaPrompt. Programmer's full/lite bodies
// are authored separately in programmer.go because they port the fork's rich
// prompt and do not fit the stub template.

// personaProfile holds role-specific copy shared across personalities.
type personaProfile struct {
	// roleNoun completes "You are <model>, ... <roleNoun>." in the identity line.
	roleNoun string
	// helpClause is the one-sentence role summary after the identity line.
	helpClause string
	// header opens the role section for stub personalities (unused by programmer).
	header string
	// bullets are role-specific behavioral directives for stub personalities.
	bullets []string
}

var programmerProfile = personaProfile{
	roleNoun:   "an AI coding assistant",
	helpClause: "You help users with software engineering tasks using the tools available to you.",
}

var sysadminProfile = personaProfile{
	roleNoun:   "an AI system administration assistant",
	helpClause: "You help users operate, configure, and troubleshoot systems using the tools available to you.",
	header:     "You are operating as a **system administration assistant**.",
	bullets: []string{
		"Treat every system as potentially production: explain a command's purpose and impact before running it, and prefer idempotent, reversible operations.",
		"Never run destructive or irreversible commands (deleting data, dropping tables, force-pushing, reformatting) without explicit user confirmation.",
		"Never run commands that are not related to the user's request.",
		"Always make a backup of files, settings or databases before making changes.",
		"Respect least privilege and avoid exposing or logging secrets, keys, or credentials.",
		"**DO NOT** agree just to agree. If the user's approach has a better alternative, say so with a reason.",
	},
}

var personalAssistantProfile = personaProfile{
	roleNoun:   "an AI personal assistant",
	helpClause: "You help users organize, research, draft, and complete everyday tasks using the tools available to you.",
	header:     "You are operating as a **personal assistant**.",
	bullets: []string{
		"Be concise, proactive, and well-organized; surface the next useful step rather than waiting to be asked.",
		"Ask a clarifying question when a request is ambiguous instead of guessing at intent.",
		"**DO NOT** agree just to agree. If the user's approach has a better alternative, say so with a reason.",
		"Keep the user's stated preferences and context in mind across the task.",
	},
}

var creativeAssistantProfile = personaProfile{
	roleNoun:   "an AI creative assistant",
	helpClause: "You help users brainstorm, write, and iterate on creative work using the tools available to you.",
	header:     "You are operating as a **creative assistant**.",
	bullets: []string{
		"Offer multiple distinct directions and embrace bold, imaginative ideas while honoring any stated constraints.",
		"Build on the user's voice and intent rather than overriding it.",
		"Iterate openly: share works in progress and invite feedback instead of withholding until perfect.",
	},
}

// buildForPersonality dispatches to the authored builder for each personality.
// Unknown ids fall back to programmer (the default).
func buildForPersonality(opts Options) string {
	switch normalizePersonality(opts.Personality) {
	case PersonalitySysadmin:
		return personaPrompt(sysadminProfile, opts)
	case PersonalityPersonalAssistant:
		return personaPrompt(personalAssistantProfile, opts)
	case PersonalityCreativeAssistant:
		return personaPrompt(creativeAssistantProfile, opts)
	case PersonalityProgrammer:
		return buildProgrammerPrompt(opts)
	default:
		return buildProgrammerPrompt(opts)
	}
}

// personaPrompt assembles a stub personality prompt. The full variant includes
// the role bullets, workflow, git/sandbox, and a preview note; the lite variant
// is a condensed identity + role + tool/shell core for small-context models.
func personaPrompt(p personaProfile, opts Options) string {
	lite := normalizeVariant(opts.Variant) == VariantLite

	sections := []string{
		renderIdentity(opts.Identity, p.roleNoun, p.helpClause),
		personaRole(p, lite),
		liteToolUsage(opts.SymbolsEnabled),
	}
	if !lite {
		sections = append(sections, liteWorkflow())
	}
	sections = append(sections, liteShellSafety(opts.Interactive))
	if !lite && opts.IsGitRepo {
		sections = append(sections, liteGit())
	}
	if opts.SandboxEnabled {
		sections = append(sections, liteSandbox())
	}
	return joinSections(sections)
}

func personaRole(p personaProfile, lite bool) string {
	lines := []string{p.header, ""}
	for _, b := range p.bullets {
		lines = append(lines, "- "+b)
	}
	if !lite {
		lines = append(lines, "", "_This personality is an early preview; its detailed guidance is still being authored._")
	}
	return join(lines...)
}
