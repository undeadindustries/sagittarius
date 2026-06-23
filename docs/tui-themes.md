# TUI themes and presentation

Sagittarius renders its interactive terminal UI with a small semantic theme so
colors stay consistent across the scrollback, dialogs, launch banner, and exit
screen. This page covers the available themes, how to select one, and the
settings that control the launch presentation.

## Themes

Two themes ship today:

| Theme | When | Look |
|-------|------|------|
| `default` | Color terminals (the default) | Purple-leaning accents for the user prompt, focus borders, links, and the launch/exit titles. Diff previews use green additions / red deletions. Status colors (error/warning/success) stay conventional. |
| `greyscale` | `NO_COLOR` or `ui.theme: "greyscale"` | No color codes at all — only bold, faint, reverse, and underline attributes (diff additions show bold, deletions reversed). The layout is identical to `default`. |

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
| User | `You ›` | Your submitted input (de-emphasized grey, with a blank line separating turns) |
| Assistant | `✦` | Model response (rendered with basic markdown) |
| Info | `ℹ` | System/slash-command output |
| Error | `✕` | Non-fatal errors |
| Tool start | `⚙` | A tool invocation began, labeled with its target (e.g. `⚙ write_file hello.txt`) |
| Tool result | `↳` | A tool's result; `write_file` results show a colorized diff |
| Confirm | `?` | A tool is awaiting your approval |

The user's own prompts are rendered in a muted grey so the assistant's replies
stay the visual focus, and a blank line separates each turn.

### Tool confirmations

While a tool confirmation is pending, a focused (purple-bordered) band appears
above the input so it is never lost in scrollback. For `write_file` it shows a
colorized unified-diff preview (green additions, red deletions) of the pending
change, followed by a selectable list:

| Choice | Keys | Effect |
|--------|------|--------|
| Allow once | `1` or `y` | Approve this single invocation |
| Allow for this session | `2` | Approve this and all later calls of the same tool until you quit |
| No | `3`, `n`, or `Esc` | Reject the invocation |

Use the arrow keys plus `Enter` to pick, or press the number/letter directly.

### Diffs

`write_file` confirmations and results render a git-style unified diff with
additions in green, deletions in red, and hunk/file markers dimmed. Long diffs
are capped (with a `… N more diff lines` footer) so they never push the input
off-screen.

## Assistant markdown

Assistant responses are rendered with a lightweight markdown subset: headings,
bullet/numbered lists, fenced code blocks (shown with a left bar), and inline
**bold**, *italic*, and `code`. This is intentionally minimal — it is not a full
CommonMark renderer. User input is always shown verbatim.

## Launch banner and tips

On startup the TUI shows an ASCII Sagittarius banner, the version line, the
active provider/model, a short tips block, a line listing the `AGENTS.md`
memory files that were loaded into the system instruction (e.g. `Loaded 2
AGENTS.md files: ~/.sagittarius/AGENTS.md, ./AGENTS.md`), and any startup notice
(e.g. a missing-API-key warning). Two settings control this:

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

## Input box

The input is a wrapping, multi-line box that grows from one row up to six as you
type (longer content scrolls inside it). Its prompt reflects the current
interaction mode (`Agent>`, `Plan>`, `Ask>`, `Debug>`). `Enter` submits the
line; `Alt+Enter` (or `Shift+Enter` / `Ctrl+J`, terminal permitting) inserts a
newline for multi-line prompts.

## Working indicator and cancel

While the agent is working, a braille spinner appears directly above the input
showing the current activity, an elapsed timer, and a cancel hint — e.g.
`⠹ Thinking… (12s · esc to cancel)`. Press `Esc` to cancel the in-flight turn.
`Ctrl+C` also cancels a running turn rather than quitting outright; a second
`Ctrl+C` (when idle) exits.

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
`docs/PARITY_CHECKLIST.md`): a `/theme` command and interactive picker, a fully
configurable footer column registry, and extended screen-reader prefixes.
