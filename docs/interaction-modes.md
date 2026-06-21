# Interaction Modes

Sagittarius interaction modes control **model selection** and optional **system-prompt
suffixes**. They are separate from the fork’s **approval modes** (`default`,
`autoEdit`, `plan`, `yolo`), which only change **tool execution policy**.

| Fork concept | Sagittarius equivalent |
|--------------|------------------------|
| `--approval-mode plan` | Tool policy (read-only tools) — unchanged |
| Sagittarius `/mode plan` | Routes to `sagittarius.modes.plan.model` when set |

## Modes

| Mode | Purpose |
|------|---------|
| **agent** | Normal operation (default). Uses global default model, then provider default. |
| **plan** | Planning-oriented model override when configured. |
| **ask** | Q&A-oriented model override when configured. |
| **debug** | Extra verbose structured logging via `slog`; optional model override. Tool execution is **not** disabled in Phase 15. |

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

**Subagents** (via `agents.ResolveSubagentModel` — execution still stubbed):

1. `sagittarius.subagents.<name>.model`
2. `sagittarius.subagents.default.model`
3. `sagittarius.defaultModels[<activeProvider>]`
4. Provider default model
5. `sagittarius.defaultModel`

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

## Debug mode scope (Phase 15)

- Emits structured `slog` Info lines for mode/model selection when debug mode is active.
- Does **not** port fork `wireLogger.ts`.
- Does **not** disable tool execution (deferred post-parity if needed).

## Related docs

- Fork plan **approval** mode: see the frozen fork `docs/cli/plan-mode.md` (tool restrictions only).
- Project decisions: `AGENTS.md` AD-007, AD-022.
