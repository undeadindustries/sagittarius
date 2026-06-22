# TUI themes and presentation

Sagittarius renders its interactive terminal UI with a small semantic theme so
colors stay consistent across the scrollback, dialogs, launch banner, and exit
screen. This page covers the available themes, how to select one, and the
settings that control the launch presentation.

## Themes

Two themes ship today:

| Theme | When | Look |
|-------|------|------|
| `default` | Color terminals (the default) | Purple-leaning accents for the user prompt, focus borders, links, and the launch/exit titles. Status colors (error/warning/success) stay conventional. |
| `greyscale` | `NO_COLOR` or `ui.theme: "greyscale"` | No color codes at all — only bold, faint, reverse, and underline attributes. The layout is identical to `default`. |

The theme lives entirely in the `internal/ui/theme` package; the agent layer
never depends on it, preserving the UI-library boundary (AD-004).

## Selecting a theme

Resolution order (first match wins):

1. The `NO_COLOR` environment variable (any non-empty value) forces `greyscale`.
   See [no-color.org](https://no-color.org).
2. `ui.theme` in `settings.json` — set it to `"greyscale"` (aliases:
   `grayscale`, `mono`, `monochrome`, `none`) for monochrome, or `"default"`.
3. Otherwise the purple `default` theme.

```json
{
  "ui": {
    "theme": "greyscale"
  }
}
```

```bash
# One-off monochrome session:
NO_COLOR=1 sagittarius
```

## Message roles

The scrollback prefixes each line by role so user input, assistant output, and
tool activity are easy to scan:

| Role | Prefix | Meaning |
|------|--------|---------|
| User | `>` | Your submitted input |
| Assistant | `✦` | Model response (rendered with basic markdown) |
| Info | `ℹ` | System/slash-command output |
| Error | `✕` | Non-fatal errors |
| Tool start | `⚙` | A tool invocation began |
| Tool result | `↳` | A tool's result |
| Confirm | `?` | A tool is awaiting your `y/n` approval |

While a tool confirmation is pending, a focused (purple-bordered) band appears
above the input with the `(y/n)` prompt so it is never lost in scrollback.

## Assistant markdown

Assistant responses are rendered with a lightweight markdown subset: headings,
bullet/numbered lists, fenced code blocks (shown with a left bar), and inline
**bold**, *italic*, and `code`. This is intentionally minimal — it is not a full
CommonMark renderer. User input is always shown verbatim.

## Launch banner and tips

On startup the TUI shows an ASCII Sagittarius banner, the version line, the
active provider/model, a short tips block, and any startup notice (e.g. a
missing-API-key warning). Two settings control this:

```json
{
  "ui": {
    "hideBanner": false,
    "hideTips": false
  }
}
```

- `ui.hideBanner` — when `true`, the ASCII logo is replaced by a one-line title.
- `ui.hideTips` — when `true`, the tips block is omitted.

## Footer

The footer has two lines:

**Line 1 (right side):** Per-turn token counts for the most recently completed
response — `↑{in} ↓{out}` — and, when the request was routed through
OpenRouter, a cost figure (e.g. `$0.0021`). When a context limit is known
(OpenAI-compatible providers with a configured `contextLimit`), the context
gauge `{pct}% ctx` is also appended.

**Line 2 (detail, wide terminals ≥ 80 cols):** Running session totals — `Σ
{in}/{out}` — followed by the cumulative session cost when OpenRouter cost is
known. The system-prompt preset label (when set) appears on the same line before
the token figures.

Token counts come from the provider's reported usage when available (all three
supported providers return them), falling back to the stdlib heuristic estimator
only when the provider returns no usage data.

## Exit screen

On `/quit` or `Ctrl+C`, Sagittarius prints a goodbye summary after the
alt-screen tears down: session id, provider, model, turn and tool-call counts,
session duration, optional session cost, a per-model/per-mode token breakdown,
and a resume hint:

```
To resume this session: sagittarius --resume <sessionId>
```

The **Model Usage** section groups rows by model, with child rows per
interaction mode (agent / plan / ask / debug). When any request was routed
through OpenRouter (the only provider that returns per-request cost), a **Cost**
column is appended to the breakdown.

Non-OpenRouter providers (Gemini, direct OpenAI) report token counts but not
cost; they appear in the table without a cost value.

## Deferred

The following fork-adjacent features are deferred (tracked in
`docs/PARITY_CHECKLIST.md`): a `/theme` command and interactive picker, rich
tool-confirmation dialogs (radio/diff preview), a fully configurable footer
column registry, and extended screen-reader prefixes.
