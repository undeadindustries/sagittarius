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

### Switching models mid-conversation

Every one of these knobs is resolved against the model that is active right
now, including `contextLimit`. Switching model (`/model`, `Ctrl+/`) or entering
a mode whose override names a different model re-resolves the window
immediately, whether or not the provider changed.

The conversation is fitted lazily: nothing is compressed at the moment you
switch. The next turn applies the usual defenses — masking, compression, then
truncation — against the new window. When the history is already larger than
the destination model's usable window, the switch prints one line saying so, so
a long conversation moving to a smaller model is not a surprise on the next
prompt.

Compression that fails is also reported in the conversation rather than only in
`~/.sagittarius/logs/sagittarius.log`. After a failure the session falls back to
truncation for the rest of its life; the failure is stated once.

## Thinking budget (`providers.<id>.models.<model>.*`)

Caps how long a model may reason before it has to act. Both keys are per-model
only — there is no provider-wide fallback, because a budget that suits a local
reasoning model is wrong for every other model behind the same endpoint. Edit
them in `/models`.

| Key | Type | Default | Purpose |
|-----|------|---------|---------|
| `thinkingBudgetTokens` | int (tokens) | unset | Reasoning tokens allowed per round. When set, it is advertised on every request as `reasoning_budget_tokens` (llama-server, with a wrap-up message) and as `ThinkingConfig.ThinkingBudget` (Gemini 2.5). `0` or absent means no budget. Gemini 3 takes a level, not a number — use `/reasoning` there. |
| `hardThinkingBudget` | bool | `false` | Enforce the budget client-side for servers that ignore the advertised one (vLLM, SGLang). The over-budget round is abandoned and re-issued with the model's partial reasoning quoted back and thinking switched off. Costs one extra round per cut, so leave it off unless a model actually loops. |

A cut only happens while the model is still purely thinking; once answer text or
a tool call has arrived the round runs to completion. The retry round runs with
the budget disabled, so two cuts can never happen back to back.

### Example

```json
{
  "providers": {
    "local-vllm": {
      "models": {
        "qwen3-30b": {
          "thinkingBudgetTokens": 4096,
          "hardThinkingBudget": true
        }
      }
    }
  }
}
```

## Sagittarius settings (`sagittarius.*`)

These live under the top-level `sagittarius` key. Leaf names are typed and
validated; unknown keys pass through untouched.

### Script collapsing (`sagittarius.scriptToolEnabled`)

| Key | Type | Default | Purpose |
|-----|------|---------|---------|
| `scriptToolEnabled` | bool | `false` | Register the `run_script` tool so the model can batch read-only operations (grep, read, list, find_symbol) in one turn instead of one LLM hop per tool. Mutating tools, shell, nested `run_script`, and confirmation-gated tools are rejected. Live-toggled from `/settings`. |

### Working memory (`sagittarius.scratchpadEnabled`)

| Key | Type | Default | Purpose |
|-----|------|---------|---------|
| `scratchpadEnabled` | bool | `true` | Register the `update_scratchpad` tool, letting the model keep a short note in the system prompt where context compression cannot reach it. Capped at 4,096 characters, which costs about 1,000 tokens on every request once populated — that cost is the only reason this toggle exists. Live-toggled from `/settings`. |

The companion `search_session` tool, which scans this session's transcript for an
exact detail compression has summarized away, has no setting: it is always
registered because it costs one tool declaration and reads a file the session is
already writing. See `docs/working-memory.md`.

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

### Google Chat Bridge (`sagittarius.chat.googleChat.*`)

| Key | Type | Default | Purpose |
|-----|------|---------|---------|
| `enabled` | bool | `false` | Enable the Google Chat bridge for remote access via 1:1 direct messages. |
| `spaceId` | string | empty | Target 1:1 DM space resource name (e.g. `spaces/AAAA...`). |
| `authorizedUsers` | string[] | `[]` | List of authorized user resource names (`users/<id>`) and/or emails. Messages and button clicks from unlisted users are dropped silently. |
| `projectId` | string | empty | GCP project ID hosting the Cloud Pub/Sub topic and subscription. |
| `subscriptionId` | string | empty | Pub/Sub pull subscription ID. |
| `credentialsFile` | string | empty | Path to GCP service account JSON key file (falls back to ADC if empty). |
| `maxResultRunes` | int | `2000` | Character limit for tool execution output before posting to chat. |
| `confirmTimeout` | int (seconds) | `300` | Timeout before an interactive tool approval card automatically fails closed and sends a deny. |

See [google-chat.md](../google-chat.md) for Google Cloud Console setup and architecture details.

### Example

```json
{
  "sagittarius": {
    "sessions": {
      "autoTitle": "auto"
    },
    "chat": {
      "googleChat": {
        "enabled": false,
        "spaceId": "spaces/DM_SPACE_ID",
        "authorizedUsers": ["users/123456789", "rob@example.com"],
        "projectId": "my-gcp-project",
        "subscriptionId": "sagittarius-sub",
        "credentialsFile": "~/.sagittarius/google-chat-key.json",
        "maxResultRunes": 2000,
        "confirmTimeout": 300
      }
    }
  }
}
```
