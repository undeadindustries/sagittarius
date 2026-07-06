# Sagittarius

Sagittarius started as a 1:1 Go port of gemini-cli. Gemini-cli was discontinued and Antigravity is...not ideal. This project has evolved into a bug-free, safe alternative to Gemini-cli, Agy, and Opencode to build large projects, admin your system, or be your assistant.

It is an open-source terminal agent CLI that orchestrates requests across:

- **Google Gemini** (native wire format, API key)
- **OpenAI-compatible endpoints** (OpenAI, OpenRouter, local vLLM, custom/local AI providers)
- **OpenAI Responses API** (GPT-5 / reasoning models)

You can set specific models for different modes (agent, plan, ask), choose different system prompts (programmer, system admin, personal assistant, creative assistant), and customize temperature and other settings.

## Requirements

- Go **1.26.4** or later ([go.dev/dl](https://go.dev/dl/))

## Build

```bash
make build
./bin/sagittarius --version
```

Or without Make:

```bash
go build -o bin/sagittarius ./cmd/sagittarius
```

## Development

```bash
make test    # unit tests
make vet     # go vet
make lint    # golangci-lint
make race    # race detector
```

## Terminal UI and rendering

The interactive session is built with **[Bubble Tea](https://github.com/charmbracelet/bubbletea)** (Charm Bracelet), styled with **lipgloss**, and composed from **bubbles** widgets (textarea input, viewport scrollback, spinner). Sagittarius runs in the terminal's **alternate screen** (`tea.WithAltScreen()`), keeps conversation history in an in-app viewport, and redraws only what the model changes each frame through Bubble Tea's Elm-style update loop. That keeps streaming output smooth and avoids the full-screen erase/redraw flicker common in naive terminal UIs.

Structured log output goes to `~/.sagittarius/logs/sagittarius.log` during interactive sessions, not stderr, so late cancel messages cannot corrupt the bottom row of the alt-screen.

**How this differs from gemini-cli and OpenCode:**

| | **Sagittarius** | **gemini-cli** | **OpenCode** |
|---|---|---|---|
| Language | Go | TypeScript (Node.js) | TypeScript |
| UI stack | Bubble Tea + lipgloss | React + [Ink](https://github.com/vadimdemedes/ink) | React/Solid on [OpenTUI](https://opentui.com) (Zig core) |
| Rendering model | Alt-screen + viewport; frame updates via Bubble Tea messages | Ink reconciler on alt-screen; later added TerminalBuffer / incremental modes to reduce flicker | Native Zig diff renderer with flexbox (Yoga) layout |
| Scrollback | In-app viewport (`PgUp`/`PgDn`); native terminal scrollback hidden while running | Similar alt-screen tradeoffs; mouse/scroll modes evolved over time | OpenTUI-managed buffers |
| Mouse default | Off (native text selection works) | Configurable; flicker fixes added mouse toggles | Framework-dependent |

Sagittarius is a single native Go binary with no Node runtime. The UI layer is swappable behind an internal interface, but today everything interactive goes through Bubble Tea.

## Configuration & Rules

Sagittarius reads its settings from `~/.sagittarius/settings.json`. Project overrides live in `<repo>/.sagittarius/settings.json` (project wins on merge). API keys belong in environment variables or OS keychain, not in settings files. See [docs/home-directory.md](docs/home-directory.md) for the dual-scope merge rules.

### Rules (`AGENTS.md`)

You can define custom rules and instructions that the agent must follow. These are placed in `AGENTS.md` files:

- **Global rules:** Create `~/.sagittarius/AGENTS.md`. The agent will apply these rules across all projects.
- **Project rules:** Create an `AGENTS.md` file in the root of your project. The agent will read this file when run within the project directory.

## Quick reference

Interactive shortcuts, headless flags, and slash commands for the same features where they exist. Full slash-command tree: [docs/reference/commands.md](docs/reference/commands.md).

| Feature | Keyboard (TUI) | CLI flags / parameters |
|---------|----------------|------------------------|
| **Interaction mode** (agent / plan / ask / debug) | `Alt+1` … `Alt+4`, `Ctrl+Shift+M`; or `/mode`, `/modes` | `--mode` (`agent`, `plan`, `ask`, `debug`) |
| **Model** (pick or cycle active set) | `Ctrl+/` (forward), `Ctrl+Shift+P` (back); or `/model`, `/models` | `-m`, `--model <id>` (pins model for this run; disables mode-based model routing) |
| **Tool approval** (confirm vs auto-run tools) | Tool cards: Allow once / session / deny; status row shows policy | `--approval-mode` (`default`, `autoEdit`, `yolo`); `-y`, `--yolo` (shorthand for yolo; not combinable with `--approval-mode`) |
| **Thinking / reasoning box** | `Ctrl+T` (persists `ui.showThinking`) | — (use `/settings` or per-model `showThinking` in `/models`) |
| **Color theme** | `Alt+T` (persists `ui.theme`) | `/theme` (no startup flag) |
| **Mouse-wheel scroll** | `Alt+M` (per session; off again on next launch) | `/mouse` (`on`, `off`, `toggle`, `show`) |
| **Background processes** | `Ctrl+B` | — |
| **Scroll conversation** | `PgUp` / `PgDn`, `Shift+Up` / `Shift+Down`; macOS: `Fn+Up` / `Fn+Down` on compact keyboards | — |
| **Prompt history** | `Up` / `Down`, `Ctrl+P` / `Ctrl+N` (at input line boundaries) | — |
| **New line in input** | `Alt+Enter`, `Shift+Enter`, `Ctrl+J` (`Enter` submits) | — |
| **Cancel in-flight turn** | `Esc` | — |
| **Quit** | `Ctrl+C` when idle; `/quit` | — |
| **Non-interactive turn** | — | `-p`, `--prompt <text>` (also accepts a single positional argument) |
| **Headless output shape** | — | `--output-format` (`text`, `json`, `stream-json`) |
| **Slash command (no TTY)** | — | `--slash "<command>"` (e.g. `--slash "/diff"`; mutually exclusive with `-p`) |
| **Resume session** | `/resume`, `/chat resume` | `--resume`, `-r` (id, index, or `latest`) |
| **List / delete sessions** | — | `--list-sessions`; `--delete-session` (id or index) |
| **Debug logging** | — | `--debug`, `-d` (writes to `~/.sagittarius/logs/sagittarius.log` in the TUI) |
| **Screen-reader TUI** | — | `--screen-reader` |
| **Version** | — | `--version`, `-v` |
| **Git worktree** (stub) | — | `--worktree`, `-w` |

`Alt+digit` is used for direct mode selection because most terminals cannot distinguish `Ctrl+digit` from the plain digit.

**Environment:** `SAGITTARIUS_SESSION_ID` pins the session id across headless invocations (shared snapshots for `--slash "/diff"` / `/undo`). See [docs/agent-testing.md](docs/agent-testing.md).

### macOS keyboard aliases

Most shortcuts match Windows/Linux. macOS terminals often send **Option** chords as special characters; Sagittarius accepts these without terminal reconfiguration:

| macOS key | Same as | Action |
|-----------|---------|--------|
| `Option+1` … `Option+4` | `Alt+1` … `Alt+4` | Switch mode (agent / plan / ask / debug) |
| `Option+T` (`†`) | `Alt+T` | Cycle theme |
| `Option+M` (`µ`) | `Alt+M` | Toggle mouse-wheel scrolling |
| `Option+1` … `4` | | Also works via `¡`, `™`, `£`, `¢` when the terminal emits those instead |

All `Ctrl+…` shortcuts behave the same as on Windows and Linux.

## FAQ

### I don't see "thinking" / reasoning

The thinking box is **off by default**. Turn it on with **`Ctrl+T`** (toggles for the rest of the session and persists to `ui.showThinking`), or enable it in **`/settings`** under UI, or set **`showThinking`** for a specific model in **`/models`**.

Even with the box enabled, **not every provider sends visible reasoning text**:

- **OpenRouter** and **OpenAI Responses** (`openai-responses` wire) can stream reasoning into the box.
- **Gemini native** does not expose readable reasoning; it only carries opaque `thoughtSignature` bytes required for tool loops.
- **OpenAI Chat** / local vLLM endpoints generally send answer text only unless the model exposes a separate reasoning field.

If the box is on and the model supports reasoning, you may briefly see "Listening for reasoning from the model." before tokens arrive.

### Why can't I copy and paste with my mouse?

Sagittarius uses the terminal **alternate screen**, which takes over the full window while the session runs. Native terminal scrollback is hidden during that time, and **mouse tracking is off by default** so your terminal's normal **click-and-drag text selection** still works on the visible transcript.

**To copy text:**

1. **Default (recommended):** Leave mouse scrolling off. Click and drag in the terminal to select text, then copy with your terminal's usual shortcut (`Ctrl+Shift+C` in many Linux terminals, `Cmd+C` on macOS after selection, or right-click → Copy).
2. **If you enabled mouse scrolling** (`Alt+M` or `/mouse on`): hold **`Shift`** while dragging to select text (standard xterm-style behavior when the app captures mouse events).
3. **Last assistant reply:** run **`/copy`** to copy the most recent assistant message to the clipboard.

**To scroll:** use **`PgUp` / `PgDn`** or **`Shift+Up` / `Shift+Down`**, or enable wheel scrolling with **`Alt+M`** or **`/mouse on`**. Wheel scrolling is per-session and resets to off on the next launch.

We chose this default because many users live in tmux, SSH, and IDE-integrated terminals where mouse capture breaks selection and copy/paste. Keyboard scrollback always works regardless of the mouse setting.

### I can't scroll back

Sagittarius runs in the terminal **alternate screen**, so your terminal's normal scrollback (mouse wheel, scrollbar, or scrolling with two fingers on a trackpad) does **not** move the conversation. That is expected. Use Sagittarius's **in-app** scroll keys instead:

**Windows and Linux**

| Key | Action |
|-----|--------|
| `PgUp` / `PgDn` | Scroll up / down half a page |
| `Shift+Up` / `Shift+Down` | Scroll one line at a time |
| `Alt+M` | Toggle mouse-wheel scrolling (optional) |

**macOS**

| Key | Action |
|-----|--------|
| `Fn+Up` / `Fn+Down` | Scroll up / down half a page (same as Page Up / Page Down on compact Mac keyboards) |
| `Shift+Up` / `Shift+Down` | Scroll one line at a time |
| `Option+M` (`µ`) or `Alt+M` | Toggle mouse-wheel scrolling (optional) |

On a full-size keyboard with dedicated Page Up / Page Down keys, those work too. The composer status row above the input shows a reminder (`Fn↑ Fn↓` on macOS, `Pg↑ Pg↓` elsewhere).

If you prefer the mouse wheel, press **`Alt+M`** (or **`Option+M`** on macOS) or run **`/mouse on`**. Wheel scrolling is off again on the next launch unless you turn it on each session.

### Why doesn't my terminal scrollback show the conversation after I quit?

The alt-screen is cleared when Sagittarius exits. The exit summary (session stats, resume hint) is printed to normal scrollback after teardown. For a durable transcript, use **`/chat save`**, session JSONL under `~/.sagittarius/tmp/`, or copy passages with the mouse (or `/copy` for the last reply) before quitting.

### How do I change mode or model quickly?

- **`Alt+1` … `Alt+4`** (or macOS Option equivalents): jump directly to agent, plan, ask, or debug.
- **`Ctrl+Shift+M`**: cycle modes in order.
- **`Ctrl+/`** / **`Ctrl+Shift+P`**: cycle active models forward / backward.
- **`/mode`**, **`/model`**, **`/modes`**: full pickers and override editors.

Per-repo routing defaults save to **project** scope (`.sagittarius/settings.json`); provider keys and definitions stay **global**. See [docs/home-directory.md](docs/home-directory.md).

### Skills (`SKILL.md`)

You can extend the agent's domain knowledge and capabilities by creating skills. Skills are Markdown files named `SKILL.md` (or ending in `.md` inside a skill directory).

**How to create a skill:**

1. Create a new directory for your skill.
2. Inside that directory, create a `SKILL.md` file.
3. Write your instructions, expert context, or playbook for the agent in that Markdown file.

**Where to put skills:**

- **Global skills:** `~/.sagittarius/skills/` (or `~/.agents/skills/`).
- **Project skills:** `<your-project>/.sagittarius/skills/` (or `<your-project>/.agents/skills/`).

The agent discovers these automatically and can activate them when relevant. Use **`/skills`** in the CLI to list or reload them.

A ready-made `verify-after-edit` skill ships in [docs/skills/verify-after-edit/SKILL.md](docs/skills/verify-after-edit/SKILL.md). Copy it into your skills directory to reinforce running lint, format, type-check, and tests after edits.

## Code quality

Sagittarius keeps code IDE-clean by running each project's own lint, format, type-check, and test tooling. It does not bundle linters. The built-in `run_project_checks` tool auto-detects the stack and runs its checks, and Go projects can opt into `gopls` code intelligence over MCP. See [docs/code-quality.md](docs/code-quality.md).

## License

Apache License 2.0. See [LICENSE](LICENSE).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). By participating, you agree to the [Code of Conduct](CODE_OF_CONDUCT.md).

## Security

Report vulnerabilities per [SECURITY.md](SECURITY.md).
