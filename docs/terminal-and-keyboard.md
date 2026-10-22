# Terminal, keyboard, and mouse

This page covers the Sagittarius terminal UI: keyboard shortcuts, headless flags, screen layout, scrolling, copy and paste, and the reasoning box. The full slash-command tree is in [reference/commands.md](reference/commands.md).

## Quick reference

The table lists interactive shortcuts, headless flags, and the slash commands for the same features where they exist.

| Feature | Keyboard (TUI) | CLI flags / parameters |
|---------|----------------|------------------------|
| **Interaction mode** (agent / plan / ask / debug) | `Shift+Tab` (cycle), `Alt+1` … `Alt+4` (direct), `Ctrl+Shift+M` (cycle alias); or `/mode`, `/modes` | `--mode` (`agent`, `plan`, `ask`, `debug`) |
| **Model** (pick or cycle active set) | `Ctrl+/` (forward), `Ctrl+Shift+P` (back); or `/model`, `/models` | `-m`, `--model <id>` (pins the model for this run and disables mode-based model routing) |
| **Tool approval** (confirm or auto-run tools) | Tool cards: Allow once / session / deny. `Alt+Y` turns yolo on or off for this session (off restores the mode you started with). The footer shows the policy. | `--approval-mode` (`default`, `autoEdit`, `yolo`); `-y`, `--yolo` (shorthand for yolo, not combinable with `--approval-mode`) |
| **Reasoning effort** | `Alt+R` steps through the levels the current model accepts, then back to its default (this session only, shown as `effort:` in the footer) | `/reasoning <level>`; `/reasoning save <level>` keeps it |
| **Thinking / reasoning box** | `Ctrl+T` (persists `ui.showThinking`) | None (use `/settings` or per-model `showThinking` in `/models`) |
| **Export last provider request** | `Ctrl+Shift+D`; or `/chat debug` (works while a turn is running) | None |
| **Color theme** | `Alt+T` (persists `ui.theme`) | `/theme` (no startup flag) |
| **Mouse (wheel scroll and drag-to-copy)** | On by default. `Alt+M` turns it off for the session (it is on again at the next launch). | `/mouse` (`on`, `off`, `toggle`, `show`) |
| **Background processes** | `Ctrl+B` | None |
| **Scroll conversation** | `PgUp` / `PgDn`, `Shift+Up` / `Shift+Down`; macOS: `Fn+Up` / `Fn+Down` on compact keyboards | None |
| **Prompt history** | `Up` / `Down`, `Ctrl+P` / `Ctrl+N` (at input line boundaries). A recalled slash command loads without its menu, so the arrows keep stepping through history. Press `Tab` to open the menu for it. | None |
| **New line in input** | `Alt+Enter`, `Shift+Enter`, `Ctrl+J` (`Enter` submits) | None |
| **Cancel in-flight turn** | `Esc` | None |
| **Quit** | `Ctrl+C` when idle; `/quit` | None |
| **Non-interactive turn** | None | `-p`, `--prompt <text>` (also accepts a single positional argument) |
| **Headless output shape** | None | `--output-format` (`text`, `json`, `stream-json`) |
| **Slash command (no TTY)** | None | `--slash "<command>"` (for example `--slash "/diff"`, mutually exclusive with `-p`) |
| **Resume session** | `/resume`, `/chat resume` | `--resume`, `-r` (id, index, or `latest`) |
| **List / delete sessions** | None | `--list-sessions`; `--delete-session` (id or index) |
| **Debug logging** | None | `--debug`, `-d` (writes to `~/.sagittarius/logs/sagittarius.log` in the TUI) |
| **Full chat transcript for bug reports** | None | `--log-verbose` (every request, response, and tool result goes to `~/.sagittarius/logs/chat-verbose-<session>.log`, with or without `--debug`) |
| **Google Chat bridge (TUI and DM)** | None | `--google-chat` (attaches a 1:1 Google Chat DM next to the TUI, see [google-chat.md](google-chat.md)) |
| **Google Chat daemon (no TTY)** | None | `--google-chat-only` (runs the headless bridge under systemd or screen) |
| **Screen layout** | `/screen` (this session only) | `--screen` (`fullscreen`, `inline`, `auto`) |
| **Screen-reader TUI** | None | `--screen-reader` |
| **Version** | None | `--version`, `-v` |
| **Git worktree** (stub) | None | `--worktree`, `-w` |

Direct mode selection uses `Alt+digit` because most terminals cannot tell `Ctrl+digit` from the plain digit.

`SAGITTARIUS_SESSION_ID` pins the session id across headless runs. This lets `--slash "/diff"` and `/undo` share snapshots with an earlier `-p` run. See [agent-testing.md](agent-testing.md).

## macOS keyboard aliases

Most shortcuts are the same as on Windows and Linux. macOS terminals often send **Option** chords as special characters. Sagittarius accepts these characters, so you do not need to change the terminal configuration.

| macOS key | Same as | Action |
|-----------|---------|--------|
| `Option+1` … `Option+4` | `Alt+1` … `Alt+4` | Switch mode (agent / plan / ask / debug) |
| `Option+T` (`†`) | `Alt+T` | Cycle theme |
| `Option+R` (`®`) | `Alt+R` | Step reasoning effort |
| `Option+Y` (`¥`) | `Alt+Y` | Toggle yolo for this session |
| `Option+M` (`µ`) | `Alt+M` | Toggle mouse mode |
| `Option+1` … `4` | | Also works through `¡`, `™`, `£`, `¢` when the terminal sends those characters |

All `Ctrl+…` shortcuts work the same as on Windows and Linux.

## Screen layout and scrolling

The default layout is `fullscreen`. It uses the terminal alternate screen, with in-app scrolling, drag selection, and dialogs. The `inline` layout prints finished blocks into the terminal's own scrollback instead. The `auto` layout picks one of the two from the terminal type. [reference/commands.md](reference/commands.md#screen-layout) explains each option.

In full-screen mode, the terminal's own scrollback (mouse wheel, scrollbar, or a two-finger trackpad scroll) does not move the conversation. This is expected. Use the in-app scroll keys:

| Key | Action |
|-----|--------|
| `PgUp` / `PgDn` | Scroll up or down half a page |
| `Shift+Up` / `Shift+Down` | Scroll one line at a time |
| `Fn+Up` / `Fn+Down` (macOS) | Same as `PgUp` / `PgDn` on compact Mac keyboards |
| `Alt+M` or `Option+M` (`µ`) | Toggle mouse mode (wheel scrolling, on by default) |

Dedicated Page Up and Page Down keys work on full-size keyboards. The composer status row shows both forms (`Pg↑ Pg↓ or Fn↑ Fn↓`), because a Mac keyboard over SSH still sends Mac keys.

The mouse wheel scrolls the conversation by default. If you turned mouse mode off with `Alt+M`, press it again or run `/mouse on`. Mouse mode is on again at the next launch.

### Conversation after exit

Sagittarius clears the alternate screen when it exits. It then prints the exit summary (session stats and the resume hint) to normal scrollback. To keep a full transcript, use `/chat save` or the session JSONL files under `~/.sagittarius/tmp/`. You can also copy text before you quit.

## Copy and paste

The alternate screen hides the terminal's own scrollback, so native terminal selection reaches only the visible rows. Sagittarius gives you three other ways to copy:

1. **Drag to copy (default).** Click and drag over the conversation. The selection scrolls when you drag to the top or bottom edge, or when you turn the mouse wheel during the drag. Sagittarius copies the text to the clipboard when you release the button. It rejoins text that it wrapped to fit the window, so a long command pastes as one line. The status row confirms the copy.
2. **Terminal-native selection.** Hold `Shift` while you drag (`Option` in iTerm2). You can also turn mouse mode off with `Alt+M` or `/mouse off` and drag normally. Both ways reach only the visible rows.
3. **Last assistant reply.** Run `/copy` to copy the most recent assistant message as raw markdown. Run `/copy code` to copy only the fenced code blocks from that reply.

### Copy over SSH (OSC 52)

Over SSH, Sagittarius sends copies to the clipboard of your local machine through the terminal (OSC 52). This also works inside tmux and GNU screen. The terminal must allow it:

- **iTerm2:** turn on *Applications in terminal may access clipboard*.
- **tmux:** set `set -g set-clipboard on` (or `allow-passthrough on`).
- **kitty, WezTerm, Ghostty, Windows Terminal, Alacritty:** most allow it by default.

## Thinking and reasoning box

The thinking box is off by default. These controls turn it on:

- `Ctrl+T` toggles it for the rest of the session and saves `ui.showThinking`.
- `/settings` has the same option under UI.
- `/models` sets `showThinking` for one model.

Not every provider sends readable reasoning text, even when the box is on:

- **OpenRouter** and **OpenAI Responses** (`openai-responses` wire format) can stream reasoning into the box.
- **Gemini native** does not send readable reasoning. It sends only opaque `thoughtSignature` bytes, which tool loops need.
- **OpenAI Chat** and local vLLM endpoints usually send answer text only, unless the model has a separate reasoning field.

When the box is on and the model supports reasoning, you can see "Listening for reasoning from the model." before the first tokens arrive.

## Changing mode or model

- `Shift+Tab` or `Ctrl+Shift+M` cycles the modes in order: agent, plan, ask, debug.
- `Alt+1` … `Alt+4` (or the macOS Option keys) selects agent, plan, ask, or debug directly.
- `Ctrl+/` and `Ctrl+Shift+P` cycle the active models forward and back.
- `Alt+R` (or `Option+R`) steps the reasoning effort for the current model. The level lasts for this session and resets when you switch models. `/reasoning save` keeps a level.
- `/mode`, `/model`, and `/modes` open the full pickers and override editors.

Every dialog that saves settings shows a `Save to:` line under its title. Press `Tab` to switch it between Global and Project. `/model`, `/modes`, and `/subagents` open on Project. `/settings`, `/providers`, and `/models` open on Global. `/model --project provider/model` saves for one project without the picker. API keys, provider URLs, and provider definitions are always global, so a project file cannot send your requests to a different endpoint. See [home-directory.md](home-directory.md).
