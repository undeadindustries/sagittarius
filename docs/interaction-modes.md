# Interaction Modes

Sagittarius interaction modes control **model selection**, **tool restrictions**, and
optional **system-prompt suffixes**. They are separate from the fork’s **approval
modes** (`default`, `autoEdit`, `plan`, `yolo`), which only change **confirmation
policy** for destructive tools.

| Fork concept | Sagittarius equivalent |
|--------------|------------------------|
| `--approval-mode plan` | Fork tool policy (read-only + plan writes) — **not** wired to `/mode plan` |
| Sagittarius `/mode plan` | Read-only exploration + writes under `docs/plans/` only |
| Sagittarius `/mode ask` | Read-only Q&A (no writes, no shell) |

## Modes

| Mode | Purpose |
|------|---------|
| **agent** | Normal operation (default). Uses global default model, then provider default. |
| **plan** | Read-only exploration plus plan writes under `docs/plans/`; shell and other writes blocked. Optional model override. |
| **ask** | Strict read-only: `read_file`, `grep_search`, `list_directory`, `activate_skill` only; no writes or shell. Optional model override. |
| **debug** | Extra verbose structured logging via `slog`; optional model override. Full tool access (same as agent). |

When `sagittarius.defaultMode` is unset, new sessions start in **agent** mode.

## Settings (`settings.json`)

All keys live under the top-level `sagittarius` object so fork keys are never clobbered:

```json
{
  "sagittarius": {
    "defaultModel": "gpt-4o",
    "defaultModels": {
      "gemini-apikey": "gemini-2.5-flash",
      "openai": "gpt-4o-mini"
    },
    "defaultMode": "agent",
    "modes": {
      "plan": {
        "model": "o3-mini",
        "systemPromptSuffix": "Focus on architecture and trade-offs before implementation."
      },
      "ask": { "model": "gpt-4o-mini" },
      "debug": { "model": "gpt-4o" }
    },
    "subagents": {
      "default": { "model": "gpt-4o-mini" },
      "codebase_investigator": { "model": "o3-mini" }
    }
  }
}
```

`defaultModels` maps a provider id (canonical, e.g. `gemini-apikey`, or the short
`gemini` alias) to the default model used while that provider is active. Unknown
`sagittarius.*` sub-keys round-trip unchanged (forward compatibility).

### Model resolution

The per-mode model override **always wins**. The legacy single `defaultModel`
now sits at the very bottom of the chain so it can no longer clobber the active
provider's configured model when switching providers.

**Main agent loop** (each turn, unless `-m` CLI override pins the model):

1. `sagittarius.modes.<mode>.model` — the per-mode override always wins
2. `sagittarius.defaultModels[<activeProvider>]` — provider-scoped default
3. Active provider’s default model (`providers.<id>.model` or built-in default)
4. `sagittarius.defaultModel` — legacy single global default (last resort)

### Auxiliary model roles

Context compression, tool-utility calls, and subagents **default to the live
model** — whatever the main loop currently resolves (so they automatically
follow a `/mode` switch or provider change). Each role can be pinned to a fixed
model with an override, the same way mode models are overridden:

| Role | Override (first non-empty wins) | Default |
|------|----------------------------------|---------|
| Context compression / summarization | `sagittarius.compression.model` | live model |
| Tool-utility calls (reserved; no consumer yet) | `sagittarius.tools.model` | live model |
| Subagents (execution still stubbed) | `sagittarius.subagents.<name>.model` → `sagittarius.subagents.default.model` | live model |

Because the live model already encodes the full mode/provider chain above, an
auxiliary role without its own override needs no separate configuration.

Example:

```json
{
  "sagittarius": {
    "compression": { "model": "gemini-2.5-flash" },
    "subagents": { "default": { "model": "gpt-4o-mini" } }
  }
}
```

## Commands

```text
/mode show
/mode agent | plan | ask | debug
/mode set plan
```

`/help` lists `/mode` and subcommands.

### TUI shortcut

Press **Ctrl+Shift+M** to cycle modes: agent → plan → ask → debug → agent.

## CLI overrides

`-m` / `--model` pins the model for the process and **disables** mode-based routing.
Fork-compatible provider defaults apply when no `sagittarius` keys are present.

## Tool restrictions (plan / ask)

Enforcement happens in the tool scheduler before execution and when building the
tool list sent to the model:

| Tool | agent / debug | plan | ask |
|------|---------------|------|-----|
| `read_file`, `grep_search`, `list_directory`, `activate_skill` | yes | yes | yes |
| `write_file` | yes | `docs/plans/` only | no |
| `run_shell_command` | yes | no | no |
| MCP tools | yes | no | no |

Blocked calls return an error function response so the model can retry or explain
the restriction. Built-in system-prompt suffixes reinforce these rules; optional
`sagittarius.modes.<mode>.systemPromptSuffix` values append after the built-in text.

## Debug mode scope

- Emits structured `slog` Info lines for mode/model selection when debug mode is active.
- Does **not** port fork `wireLogger.ts`.
- Does **not** restrict tools (same access as agent mode).

## Related docs

- Fork plan **approval** mode: see the frozen fork `docs/cli/plan-mode.md` (tool restrictions only).
- Project decisions: `AGENTS.md` AD-007, AD-022.
