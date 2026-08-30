# Settings reference

Sagittarius reads `~/.gemini/settings.json` (shared with the fork where
practical). Provider-scoped options live under `providers.<id>.*`, where `<id>`
is a built-in provider (`openai`, `gemini-apikey`, `openai-responses`) or a key
under `providers.custom.<id>`.

## Context-management settings (openai-chat only)

These tune the Phase 11 local-context defenses. They take effect **only when the
active provider uses the `openai-chat` wire format** (the fork's local mode).
Gemini-native and `openai-responses` providers ignore them — those paths are
never masked or compressed client-side. Leaf names match the fork for
`settings.json` compatibility; omitting a key uses the built-in default.

| Key | Type | Default | Purpose |
|-----|------|---------|---------|
| `contextLimit` | int (tokens) | `32768` | Assumed model context window. Scales the masking thresholds and drives pre-turn budget math. Custom providers may instead set `defaultContextLimit`. |
| `compressionThreshold` | float (0–1) | `0.4` | Fraction of `contextLimit` at which chat history is summarized. Setting it explicitly **pins** the value and disables adaptive tightening. |
| `preserveFraction` | float (0–1) | `0.2` | Fraction of the most recent history kept verbatim after a compression. |
| `toolOutputMaskingEnabled` | bool | `true` | Offload bulky tool outputs to disk and replace them with a compact marker. |
| `toolOutputMaskingProtectionFraction` | float (0.05–0.5) | `0.15` | Fraction of `contextLimit` of newest tool output kept unmasked (floored at 2000 tokens). |
| `toolOutputMaskingPrunableFraction` | float (0.05–0.5) | `0.10` | Prunable buffer that must accumulate before masking fires (floored at 1000 tokens). |
| `toolOutputMaskingProtectLatestTurn` | bool | `true` | Never mask tool outputs in the most recent turn. |

Write-file ejection, the pre-turn token budget, and adaptive-threshold
tightening also run in `openai-chat` mode but currently use built-in defaults
and are not yet user-configurable.

### Example

```json
{
  "providers": {
    "active": "local-vllm",
    "custom": {
      "local-vllm": {
        "displayName": "Local vLLM",
        "baseUrl": "http://localhost:8000/v1",
        "wireFormat": "openai-chat",
        "defaultContextLimit": 32768
      }
    },
    "local-vllm": {
      "compressionThreshold": 0.5,
      "preserveFraction": 0.25,
      "toolOutputMaskingProtectionFraction": 0.2
    }
  }
}
```

Compression and summarization always use the **active provider model**; there is
no separate summarizer/compressor model setting.

## Sagittarius settings (`sagittarius.*`)

These live under the top-level `sagittarius` key. Leaf names are typed and
validated; unknown keys pass through untouched.

### Script collapsing (`sagittarius.scriptToolEnabled`)

| Key | Type | Default | Purpose |
|-----|------|---------|---------|
| `scriptToolEnabled` | bool | `false` | Register the `run_script` tool so the model can batch read-only operations (grep, read, list, find_symbol) in one turn instead of one LLM hop per tool. Mutating tools, shell, nested `run_script`, and confirmation-gated tools are rejected. Live-toggled from `/settings`. |

### Subagents (`sagittarius.subagents.*`)

| Key | Type | Default | Purpose |
|-----|------|---------|---------|
| `research.enabled` | bool | `false` | Register the `task` tool: read-only children that explore and summarize, isolating a large search from your context window. |
| `coding.enabled` | bool | `false` | Register the `code_task` tool: write-capable children that each declare the paths they may modify. Siblings run in parallel and overlapping leases are rejected before any child starts. |
| `research.model` / `coding.model` | string | empty | Model for that class. Empty falls back to `default.model`, then the live model. |
| `default.model` | string | empty | Model for any subagent class without its own pin. |
| `enabled` | bool | `false` | Deprecated. Stands in for `research.enabled` when that key is unset; never enables coding subagents. |

Both switches are live-toggled from `/settings`. See [subagents.md](../subagents.md)
for the lease format, what a coding subagent cannot do, and why approval is a
single up-front prompt.

### Goal evaluator (`sagittarius.goal.*`)

| Key | Type | Default | Purpose |
|-----|------|---------|---------|
| `maxTurns` | int | `25` | Cap on autonomous `/goal` loop iterations. |
| `evaluatorProvider` | string | empty | Provider used by the `/goal` judge. Prefer a different family from the worker — same-family models share self-approval. Empty uses the worker's provider. |
| `evaluatorModel` | string | empty | Model that judges goal completion. Prefer a different family from the worker. Stronger helps; empty means the worker grades its own work. |
| `evaluatorTimeout` | int (seconds) | `120` | Cap on the judge's read-only tool loop. |
| `defaultBudget` | int | unset | Token budget applied when `/goal start` does not specify one. |

The judge is a read-only sub-runner (ask-mode tools, 6 tool-round cap) with its own system prompt. It verifies claims against the current working tree and does not depend on git history. Live-toggled from `/settings` (Goal section).

### Sessions (`sagittarius.sessions.*`)

| Key | Type | Default | Purpose |
|-----|------|---------|---------|
| `autoTitle` | string | `prompt` | Automatic session titling after your first full exchange. `prompt` applies the proposed title and shows a one-time `Named "…" — Ctrl+E rename` hint on the status row (Ctrl+E opens a rename editor, Enter on an empty input dismisses, everything else stays passive). `auto` applies the title silently. `off` disables titling entirely and leaves the first-message fallback. `/chat rename` always overrides manually. |

### Example

```json
{
  "sagittarius": {
    "sessions": {
      "autoTitle": "auto"
    }
  }
}
```
