// Package prompt builds Sagittarius system prompts. It ports the gemini-cli
// programmer prompt (full + lite variants) adapted to Sagittarius's real tool
// set, behind a personality abstraction so additional personalities
// (sysadmin, assistant, ...) can be added later. The package is a leaf used only
// for prompt *construction*: it imports internal/config (personality constants +
// canonicalization) and internal/tools (wire-name constants), and nothing imports
// it back. Personality/variant *resolution* lives in internal/config
// (config.ResolvePersonality / config.ResolveVariant) so internal/provider and the
// runner can share it without importing prompt; callers convert the resolved
// config strings into prompt.Personality/prompt.Variant when building Options.
//
// Layout: prompt.go (types + shared core sections), personas.go (registry +
// stub assembly), programmer.go (programmer full/lite bodies).
package prompt

import (
	"strings"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/tools"
)

// Personality is what the agent is specialized for.
type Personality string

const (
	PersonalityProgrammer        Personality = config.PersonalityProgrammer
	PersonalitySysadmin          Personality = config.PersonalitySysadmin
	PersonalityPersonalAssistant Personality = config.PersonalityPersonalAssistant
	PersonalityCreativeAssistant Personality = config.PersonalityCreativeAssistant
	// PersonalityAssistant is the legacy generic id, canonicalized to
	// personal-assistant during resolution.
	PersonalityAssistant Personality = config.PersonalityAssistant
)

// Variant selects prompt size. Full is the default rich prompt; Lite is the
// low-context port of the fork's local prompt for small-context models.
type Variant string

const (
	VariantFull Variant = "full"
	VariantLite Variant = "lite"
)

// DefaultPersonality and DefaultVariant are the built-in fallbacks at the
// bottom of the resolution chain.
const (
	DefaultPersonality = PersonalityProgrammer
	DefaultVariant     = VariantFull
)

// KnownPersonality reports whether p is a recognized personality id.
func KnownPersonality(p Personality) bool {
	return config.KnownPersonality(string(p))
}

// KnownVariant reports whether v is a recognized variant id (full or lite,
// case- and space-insensitive).
func KnownVariant(v Variant) bool {
	switch Variant(strings.ToLower(strings.TrimSpace(string(v)))) {
	case VariantFull, VariantLite:
		return true
	default:
		return false
	}
}

func normalizePersonality(p Personality) Personality {
	canon, _ := config.CanonicalPersonality(string(p))
	return Personality(canon)
}

func normalizeVariant(v Variant) Variant {
	if Variant(strings.ToLower(strings.TrimSpace(string(v)))) == VariantLite {
		return VariantLite
	}
	return VariantFull
}

// Identity describes the active model and provider so the prompt can give an
// honest self-identification line (ported from the fork's renderIdentity).
type Identity struct {
	// Model is the resolved model id (e.g. "gpt-4o", "qwen3"). The
	// "local-model" placeholder means "the server picks" and is treated as
	// unknown.
	Model string
	// ProviderName is the human-readable provider label (e.g. "OpenRouter").
	ProviderName string
}

// Options drives Build.
type Options struct {
	Personality Personality
	Variant     Variant
	Identity    Identity
	ToolNames   []string
	Interactive bool
	IsGitRepo   bool
	// OS is the host GOOS; empty means unknown, which suppresses the platform sentence.
	OS string
	// SymbolsEnabled reports whether the find_symbol tool is registered, so the
	// prompt can steer the model toward it for symbol lookups.
	// EditEnabled reports whether the edit tool is registered.
	EditEnabled    bool
	SymbolsEnabled bool
	MemoryEnabled  bool
}

// Build returns the system prompt base (without user memory or mode suffix,
// which the runner appends). Unknown personalities fall back to programmer.
func Build(opts Options) string {
	return buildForPersonality(opts)
}

// renderIdentity builds the self-identification block. roleNoun completes the
// "You are Sagittarius, <roleNoun>." sentence and helpClause is the role summary
// that follows; the model-honesty sentence is identical across personalities.
func renderIdentity(id Identity, roleNoun, helpClause string) string {
	model := strings.TrimSpace(id.Model)
	known := model != "" && model != "local-model"
	providerName := strings.TrimSpace(id.ProviderName)

	who := "You are Sagittarius, " + roleNoun + "."
	var selfID string
	if known {
		via := ""
		if providerName != "" {
			via = " (via " + providerName + ")"
		}
		selfID = "If asked which AI model or LLM you are, identify yourself accurately as " + model + via + ". Do not claim to be a different model."
	} else {
		selfID = "If asked which AI model or LLM you are, answer honestly based on your own knowledge. Do not claim to be Google Gemini or any specific model unless you genuinely are that model."
	}
	return who + " " + helpClause + "\n\n" + selfID
}

// Shared condensed sections used by programmer lite and stub personalities.

func liteToolUsage(symbolsEnabled, editEnabled bool) string {
	search := "**Search before reading.** Use `" + tools.GrepToolName + "` to find specific strings or patterns and `" + tools.ListDirectoryToolName + "` to explore directories. Do not read entire files unless necessary — target specific line ranges with `" + tools.ReadFileToolName + "`."
	if symbolsEnabled {
		search += " When you know a symbol name (function, type, class) and want its definition or call sites, prefer `" + tools.FindSymbolToolName + "` over `" + tools.GrepToolName + "`."
	}
	var write string
	if editEnabled {
		write = "**Always read before modifying.** If you are changing an existing file, prefer `edit` over `write_file` because it is faster and safer against truncation. Use `write_file` only to create new files or when intentionally rewriting the whole file. Read first to get the exact string."
	} else {
		write = "**Always read before modifying.** You cannot use patch/diff operations. Use `write_file` with the ENTIRE file contents."
	}

	return join(
		"## Tool Usage",
		"",
		"You have tools to read, search, and create or update files, and to run shell commands.",
		"",
		search,
		"",
		write,
		"",
		"**No Placeholders:** NEVER use placeholders or elision (like `// ... existing code ...`) in file writes. Tools overwrite files entirely, so you must always provide the COMPLETE file content.",
		"",
		"**No Diff Format:** NEVER send unified-diff lines (`+` / `-` prefixes) or UI diff previews to `write_file`. It is not a patch tool — send the entire file body.",
		"",
		"**Prefer editing over creating.** Do not create new files when you can update existing ones. Do not create documentation files unless explicitly asked.",
		"",
		toolInvocationMandate(editEnabled),
	)
}

func liteWorkflow() string {
	return join(
		"## Workflow",
		"",
		"For each request:",
		"1. **Understand**: Clarify ambiguous requirements before acting. Ask the user if unsure.",
		"2. **Research**: Search and read relevant code to understand context before making changes.",
		"3. **Implement**: Make targeted changes. Prefer small, incremental edits over large rewrites. Invoke `"+tools.WriteFileToolName+"` or `"+tools.ShellToolName+"` in the same turn once you know the fix — do not stop after narrating intent.",
		"4. **Verify**: CRITICAL: After EVERY write — including edits to files you already wrote earlier in this session — you MUST re-run the project's checks (lint, format check, type check, build, and tests) on the final version of every changed file. Use `"+tools.ProjectChecksToolName+"` when available, or `"+tools.ShellToolName+"` to run the project's own scripts (`make lint`, `npm test`). Discover the right checker from the project (scripts and config files) before falling back to language defaults. If the expected checker is not installed, tell the user once how to install it (e.g. \"run `pip install ruff` and I can lint\") rather than skipping the check. A passing check from an earlier turn does NOT cover later edits. Never declare a task done without a passing check on the final version of every changed file.",
	)
}

func liteShellSafety(interactive bool) string {
	lines := []string{
		"## Shell Commands",
		"",
		"- Use `" + tools.ShellToolName + "` to run terminal commands.",
		"- Never run destructive or irreversible commands (rm -rf, DROP TABLE, force push) without explicit user confirmation.",
		"- Quote file paths that contain spaces.",
		"- Avoid interactive commands (e.g. `git rebase -i`); use non-interactive flags when available (`npm init -y`).",
		"- For a process that only needs to outlive the current turn — a dev server, a watcher, a tail — use `run_shell_command`'s `is_background` parameter instead of detaching. Sagittarius tracks those, captures their output, and can kill them by process group.",
	}
	if interactive {
		lines = append(lines, "- Ask the user before running commands with significant side effects.")
	}
	return join(lines...)
}

func liteGit() string {
	return join(
		"## Git",
		"",
		"- Never push to a remote repository unless the user explicitly asks.",
		"- Never force push to main/master.",
		"- Before committing, review changes with `git status` and `git diff HEAD`.",
		`- Write clear, concise commit messages focused on "why" not "what".`,
		"- After committing, confirm success with `git status`.",
	)
}

// toolInvocationMandate discourages premature stops where the model narrates an
// intended tool call but ends the turn without invoking it. The final bullet is
// deliberately last (highest recency) because it is a precedence rule over the
// bullets above it, not an independent behavior of equal weight.
func toolInvocationMandate(editEnabled bool) string {
	mutatingTools := "`" + tools.WriteFileToolName + "`"
	if editEnabled {
		mutatingTools += ", `" + tools.EditToolName + "`,"
	}
	mutatingTools += " and mutating `" + tools.ShellToolName + "`"
	return join(
		"**Execute, don't narrate.** When a task requires tools, invoke them in the same response. Never end a turn with only text that promises a future action (e.g. \"Let me write...\", \"I will fix...\", \"I'll update that file\") without actually calling the tool.",
		"",
		"**Incomplete turns.** When the user has asked for a change and you know what to change but emit no tool calls, the turn failed: invoke the needed tools now, or ask one specific blocking question. Do not yield while actionable work remains undone.",
		"",
		"**Directives require tools.** For fix/implement/update/create requests, research with tools when needed, then mutate with `"+tools.WriteFileToolName+"` or `"+tools.ShellToolName+"` in the same turn when the fix is clear. Do not split \"I'll do it next\" across turns for small, obvious fixes.",
		"",
		"**Scope limits outrank this mandate.** When the user restricts the turn — \"just discuss this\", "+
			"\"don't change anything yet\", \"tell me what you would do\", or a named exclusion like \"do not edit X\" — "+
			"a text-only answer with no mutating tool calls is the correct and complete turn, not a failed one. "+
			"Read-only tools stay available for research; "+mutatingTools+" do not. "+
			"The restriction holds until the user lifts it. If you cannot tell whether a message is a directive or "+
			"a discussion, ask one short question rather than acting.",
	)
}

func join(lines ...string) string {
	return strings.Join(lines, "\n")
}

func joinSections(sections []string) string {
	parts := make([]string, 0, len(sections))
	for _, s := range sections {
		s = strings.TrimSpace(s)
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n\n")
}
