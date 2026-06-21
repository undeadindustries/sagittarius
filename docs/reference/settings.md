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
