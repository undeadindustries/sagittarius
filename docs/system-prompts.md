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
| `programmer` | Implemented (default) | Software-engineering assistant. |
| `sysadmin`   | Stub | Recognized; currently falls back to the programmer prompt. |
| `assistant`  | Stub | Recognized; currently falls back to the programmer prompt. |

The stubs exist so settings validation, listings, and resolution all recognize
them today; their dedicated prompt content is deferred.

## Variants

A **variant** controls prompt size.

| Variant | When to use |
|---------|-------------|
| `full` (default) | Standard models. Rich mandates, workflow, and operational guidelines. |
| `lite` | Low-context / small models. A condensed prompt (identity, tool usage, workflow, edit rules, shell safety, git, sandbox). |

`lite` reuses the existing per-provider `promptMode` setting (`lite`/`full`).

## Resolution order

Both personality and variant resolve by **first non-empty wins**:

1. **Per-model override** — `providers.<id>.models.<model>.{personality,promptMode}`
2. **Provider override** — `providers.<id>.{personality,promptMode}`
3. **Global default** — `sagittarius.systemPrompt.{personality,variant}`
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
      "promptMode": "full",
      "models": {
        "qwen3-small": { "promptMode": "lite" }
      }
    }
  }
}
```

The provider's `personality` is also editable from the providers wizard / via
`provider.ApplyProviderSetting` (openai-chat and openai-responses wire formats;
gemini exposes no editable keys).
