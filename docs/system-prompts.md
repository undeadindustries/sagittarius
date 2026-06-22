# System prompts

Sagittarius composes the model's system instruction from three parts, in order:

1. **Personality base prompt** — the agent's "brain" (ported and adapted from the
   gemini-cli programmer prompt).
2. **Project memory** — the contents of discovered `AGENTS.md` files.
3. **Mode suffix** — the optional `sagittarius.modes.<mode>.systemPromptSuffix`
   for the active interaction mode.

The personality base prompt is the part this document covers. Memory discovery
is described in [home-directory.md](home-directory.md); interaction modes in
[interaction-modes.md](interaction-modes.md).

## Personalities

A **personality** is what the agent is specialized for.

| Personality | Status | Notes |
|-------------|--------|-------|
| `programmer` | Implemented (default) | Software-engineering assistant. Full prompt is an adaptation of the fork programmer prompt; lite is a faithful port of the fork local prompt. |
| `sysadmin`   | Stub | Distinct short role preamble (system/infra focus) over the shared lite core. |
| `personal-assistant` | Stub | Distinct short role preamble (productivity/organization focus). |
| `creative-assistant` | Stub | Distinct short role preamble (writing/ideation focus). |
| `assistant` | Legacy alias | Reads as `personal-assistant`. |

The stub personalities each produce a **distinct** prompt (a role-specific
identity line plus a few role bullets over the shared tool/workflow/shell-safety
core), so picking one is meaningful today; their full dedicated content is still
deferred. The legacy `assistant` id is canonicalized to `personal-assistant` on
read.

## System prompt presets

The providers wizard collapses **personality** and **variant** into a single
**System prompt** picker. Each preset writes both `personality` and `promptMode`:

| Preset id | Label | personality / variant |
|-----------|-------|------------------------|
| `programmer` | Programmer | programmer / full |
| `programmer-lite` | Programmer (low context) | programmer / lite |
| `sysadmin` | System administrator | sysadmin / full |
| `sysadmin-lite` | System administrator (low context) | sysadmin / lite |
| `personal-assistant` | Personal assistant | personal-assistant / full |
| `personal-assistant-lite` | Personal assistant (low context) | personal-assistant / lite |
| `creative-assistant` | Creative assistant | creative-assistant / full |
| `creative-assistant-lite` | Creative assistant (low context) | creative-assistant / lite |

### Preset-linked sampling defaults

Selecting a preset does **not** persist `temperature` or `compressionThreshold`.
Those resolve dynamically so model-family rules always win and an unset value is
distinguishable from a user-pinned one:

- **temperature** — user pin → model-family rule → personality default → omit.
  Personality defaults (generic openai-chat models): programmer 0.35, sysadmin
  0.25, personal assistant 0.55, creative assistant 0.85. Model-family rules
  **omit** temperature for `gemini-3*`/`gemini-2.5*`, `gpt-5*`/`o3*`/`o4*`, and
  Anthropic Opus 4.7+, and force `1.0` for Qwen3-Coder families.
- **compressionThreshold** — user pin → variant default (`full` 0.45, `lite`
  0.38).

When the user applies a preset the wizard shows an info line describing the
suggested temperature and compression threshold, noting which were kept because
the user had pinned them.

### Context limit auto-detection

On model activation and model switch, Sagittarius reads the model's context
window from the provider when available (`context_length` on OpenRouter,
`max_model_len` on vLLM/OpenAI-compatible endpoints, `inputTokenLimit` on Gemini)
and falls back to a static table for OpenAI-direct ids. The discovered limit is
written to `providers.<id>.contextLimit` **only when the user has not pinned it**
(tracked by `contextLimitUserSet`).

## Variants

A **variant** controls prompt size.

| Variant | When to use |
|---------|-------------|
| `full` (default) | Standard models. Rich mandates, workflow, and operational guidelines. |
| `lite` | Low-context / small models. A condensed prompt (identity, tool usage, workflow, edit rules, shell safety, git, sandbox). |

`lite` reuses the existing per-provider `promptMode` setting (`lite`/`full`).

## Resolution order

Both personality and variant resolve by **first non-empty wins**:

1. **Provider override** — `providers.<id>.{personality,promptMode}`
2. **Project default** — `sagittarius.systemPrompt` in `<repo>/.sagittarius/settings.json`
   (set via `/system-prompt`; merged over the global `~/.sagittarius` default)
3. **Global default** — `sagittarius.systemPrompt` in `~/.sagittarius/settings.json`
4. **Built-in default** — `programmer` / `full`

The runner re-resolves on every model, provider, mode, settings, or memory
change, so switching providers or models updates the prompt (and identity) live.

## Identity

The prompt opens with an honest self-identification line derived from the active
model and provider:

- **Known model** (a real model id, not the `local-model` placeholder): the
  prompt names the model and provider and instructs the agent to identify itself
  accurately as that model.
- **Unknown model**: the prompt instructs the agent to answer honestly and
  **forbids** falsely claiming to be Google Gemini (or any specific model it is
  not). This prevents a non-Gemini model served through an OpenAI-compatible
  endpoint from claiming to be Gemini.

## Adapted, not copied

The full prompt is an **adaptation** of the fork's programmer prompt: it
references only Sagittarius's real tools (`read_file`, `write_file`,
`list_directory`, `run_shell_command`, `grep_search`) and drops sections for
features Sagittarius has not ported (todo/plan-mode tools, task tracker,
sub-agents, hierarchical memory directives).

## Example settings

```json
{
  "sagittarius": {
    "systemPrompt": { "personality": "programmer", "variant": "full" }
  },
  "providers": {
    "active": "openai",
    "openai": {
      "model": "gpt-4o",
      "promptMode": "full"
    }
  }
}
```

Set the project default interactively with `/system-prompt` (writes
`<repo>/.sagittarius/settings.json`). The provider's `personality` is also editable
from the providers wizard / via `provider.ApplyProviderSetting` (openai-chat and
openai-responses wire formats; gemini exposes no editable keys).

## Resetting overrides

The providers edit sheet shows each advanced row's **effective** value (the
resolved default in parentheses when unset, `= value` when overridden) and
supports two resets:

- **Per field** — press `r` on a highlighted row to clear that single override
  (`Deps.ClearSetting` → `provider.ClearProviderSetting`). On the System prompt
  row this clears both `personality` and `promptMode`.
- **Whole provider** — the "Reset all settings to defaults" row clears every
  behavioral override (`Deps.ResetSettings` →
  `provider.ResetProviderInstanceOverrides`) while preserving the model, base
  URL, wire format, API key, and curated `activeModels`.
