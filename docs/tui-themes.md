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

The user's own prompts are rendered in a muted grey so the assistant's replies
stay the visual focus, and a blank line separates each turn.

## Tool cards

Each tool invocation renders as a single rounded **card** in the scrollback
rather than separate start/output/result lines. The card updates in place across
the call's lifecycle, so you see one grouped box per tool call. Its top border
carries a status icon, the tool's display name, and a dimmed truncated summary of
the arguments (the command, the path, etc.).

On the `default` theme the border color follows the card's state, not the tool:
dark purple once it's finished, a slightly brighter purple while it runs, muted
red when it fails or is denied, and the bright accent purple when it is waiting
for your answer. That last one is always the brightest, so a pending approval
stands out from a scroll full of finished cards. The thinking box and the
sub-agent frame use the dark purple. `greyscale` keeps plain borders.

```
╭─ ✓ Shell  go build ./... ───────────────────────╮
│ ok                                              │
│ exit 0                                          │
╰─────────────────────────────────────────────────╯
```

| Phase | Icon | Body |
|-------|------|------|
| Running | spinner | Live output (shell) or a `Running…` placeholder |
| Confirming | `?` (warning) | A nested command/diff preview plus the numbered allow menu |
| Success | `✓` (green) | The result snippet, a colorized diff (`write_file`), or formatted text; shell adds an `exit N` footer |
| Error | `✗` (red) | The error text (workspace-boundary block, denial, tool failure, or a non-zero exit) |

Built-in tools get friendly names (`run_shell_command` → **Shell**,
`write_file` → **Write file**, `grep_search` → **Search**, …). MCP tools render
as their bare tool name with a dim `(server)` badge so they are visually distinct
from built-ins. Long output is capped to the most recent lines with a `… N more
lines` note, so a noisy command never floods the viewport.

### Tool confirmations

When a tool needs approval, its card switches to the confirming phase (a
focus-bordered box) and shows the request inline in the scrollback — a nested
preview (the colorized diff for `write_file`, or the command for shell) followed
by a numbered allow menu:

| Choice | Keys | Effect |
|--------|------|--------|
| Allow once | `1` or `y` | Approve this single invocation |
| Allow for this session | `2` | Approve this and all later calls of the same tool until you quit |
| No | `3`, `n`, or `Esc` | Reject the invocation |

Use the arrow keys plus `Enter` to pick, or press the number/letter directly.
The viewport pins to the confirming card so the prompt is never lost.

### Diffs

`write_file` confirmations and results render a git-style unified diff with
additions in green, deletions in red, and hunk/file markers dimmed. Long diffs
are capped (with a `… N more diff lines` footer) so they never push the input
off-screen.

## Assistant markdown

Assistant responses are rendered with a lightweight markdown subset: headings,
bullet/numbered lists, fenced code blocks, and inline **bold**, *italic*, and
`code`. Fenced code uses `Theme.Code` with no gutter (a copyable `│ ` bar made
mouse-select paste unusable). Long code, diff, and command-output lines wrap
instead of truncating; a dim `wrapped to fit · /copy code N for the exact text`
line appears under a fenced block only after it closes, and only when a line
in that block wrapped. This is intentionally minimal — it is not a full
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

While the agent is working, a braille spinner appears just above the key-hint
row (the `/screen display mode · …` line over the input) showing the current
activity, an elapsed timer, and a cancel hint — e.g.
`⠹ Working… (12s · esc to cancel)`. The thinking box uses the same spot. Both
sit above the key hints so that row stays put next to the input when a turn
starts or the box opens; the conversation gives up the space instead. Press `Esc` to cancel the in-flight turn.
`Ctrl+C` also cancels a running turn rather than quitting outright; a second
`Ctrl+C` (when idle) exits.

The label is phase-accurate, so the spinner reflects actual activity:

- **`Working…`** — the turn is busy with local context preparation, the network
  round-trip, or provider queueing (time-to-first-token), and during the gaps
  between tool rounds while waiting on the next model response. This is the
  default state immediately after you submit.
- **`Thinking…`** — shown on the standalone spinner line when the model is
  actively streaming reasoning ("thinking") tokens while the dedicated thinking
  box is hidden (`showThinking` is off).
- **`Running {tool}`** — shown in the tool card while a tool executes (the card
  header carries the spinner; the standalone line is suppressed).
- **Thinking box** — a separate rounded box (its border carries the spinner and
  a `Thinking` label) appears when the model streams reasoning tokens *and*
  `showThinking` is enabled (`Ctrl+T`, or a per-provider/model/global setting).
  Providers that send no reasoning never trigger it, so you see `Working…` instead.

`showThinking` controls only whether the box is displayed. Reasoning text is
requested whenever reasoning is enabled for the model, so `Thinking…` stays
accurate with the box collapsed and `Ctrl+T` shows the thoughts so far rather
than starting to collect them. This adds no cost: providers bill the full
reasoning trace as output tokens whether or not they return a readable summary.
Turn reasoning off with `/reasoning none` if you want to stop paying for it.
- **No spinner** — once the assistant's reply text is visibly streaming into the
  scrollback, the spinner line is hidden: the words are the feedback.

## Footer

The footer has two lines:

**Line 1 (right side):** Per-turn token counts for the most recently completed
response — `↑{in} ↓{out}` — and, when the request was routed through
OpenRouter, a cost figure (e.g. `$0.0021`). When a context limit is known
(OpenAI-compatible providers with a configured `contextLimit`), the context
gauge `{pct}% ctx` is also appended. On terminals at least 80 columns wide, the
active turn's decode rate is placed just before that gauge (`42 tok/s`, or
`7.5 tok/s` when the rate is under 10). The gauge uses `ContextTokens / ContextLimit`
(capped at 100%). `ContextTokens` comes from the provider's last main-turn
`prompt_tokens` when reported; after `/compress` or automatic defenses in
`prepareContext`, it is refreshed from `CompressionInfo.NewTokenCount` or a local
history estimate so the figure drops immediately without waiting for the next
API turn (AD-070; gemini-cli parity).

The rate is generated tokens divided by the time from the first streamed
content chunk to the last. The wait before the first token is not included.
No provider sends this number. Sagittarius computes it. When a provider omits
token usage, the count is a character estimate of the answer, reasoning, and
tool calls. A new turn clears the figure until that turn has at least 16
generated tokens and 500ms of decode time. A reply that arrives in one chunk
has no decode interval, so the rate stays hidden. Providers that send large
chunks (Gemini) read slightly high, because the first chunk's tokens were
generated before the clock started. The figure updates when the round
finishes, at the same time as `% ctx`. It does not tick while the reply is
still streaming.

**Line 2 (detail):** The current settings, then the system-prompt preset label
(when set), then (on terminals ≥ 80 cols) the running session totals `Σ
{in}/{out}` and the cumulative session cost when OpenRouter cost is known. The
settings read live state: the tool policy (`Tools: auto-approve all (yolo)`,
`Tools: confirm before run`, `Tools: read-only`), `screen: full` or `inline`,
`thinking: shown` or `hidden`, `effort: <level>` when a reasoning effort is set
for this session (`Alt+R`, `/reasoning`) or pinned in settings, and `mouse: on`,
`off`, or `terminal` (inline mode leaves the mouse to the terminal). For
example: `Tools: auto-approve all (yolo) · screen: full · thinking: hidden ·
effort: high · mouse: on  ·  System Prompt: System administrator`.

The row above the composer is the opposite half: it only says which keys to
press (`/screen display mode · Ctrl+T show thinking · Alt+R or ⌥R effort ·
Ctrl+/ model · Alt+Y or ⌥Y yolo · Alt+M or ⌥M mouse · Pg↑ Pg↓ or Fn↑ Fn↓`). The
Alt hints name both keyboards, because the process OS is the server's and a Mac
over SSH would otherwise see only the Alt and Page keys. While the read-only
posture is on, `/readonly off to lift` leads that row. Inline mode stops after
the yolo hint, since page keys and mouse capture do nothing there.

On a narrow terminal both rows truncate instead of wrapping. They drop whole
entries from the end first (the scroll keys, then the mouse hint, then yolo,
model, and effort), and
cut text only when the first entry itself does not fit.

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
