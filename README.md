```
     · ✦ ·
  »»»»»»———➤
  S A G I T T A R I U S
```

# Sagittarius

Sagittarius is an agentic harness for coding, system administration, and personal assistance. It treats your machines and servers as live production environments where uptime, safety, and operational predictability matter.

### History and background

Sagittarius started as a 1:1 Go port of `gemini-cli`. When upstream development on Gemini-cli ended and alternative forks proved brittle, Sagittarius evolved into an independent, production-grade agent harness. It is built as a dependable, safe alternative to Gemini-cli, Agy, and OpenCode to construct large software projects, administer servers, and automate workflows.

It is an open-source terminal agent CLI that orchestrates requests across:

- **Google Gemini** (native wire format, API key)
- **OpenAI-compatible endpoints** (OpenAI, OpenRouter, local vLLM, custom/local AI providers)
- **OpenAI Responses API** (GPT-5 / reasoning models)

You can set specific models for different modes (agent, plan, ask, debug), choose different system prompts (programmer, system admin, personal assistant, creative assistant), and customize temperature, reasoning effort, and tool execution settings.

## Requirements

- Go **1.26.4** or later ([go.dev/dl](https://go.dev/dl/))

## Installation

### Via Homebrew (macOS & Linux)

```bash
brew trust undeadindustries/tap
brew install undeadindustries/tap/sagittarius
```

Or tap and install explicitly:

```bash
brew tap undeadindustries/tap
brew install sagittarius
```

### From GitHub Releases

Pre-compiled binaries for Linux and macOS (amd64 and arm64) are available on the [GitHub Releases](https://github.com/undeadindustries/sagittarius/releases) page.

This detects your platform, resolves the latest release, and installs into `/usr/local/bin`:

```bash
VERSION=$(curl -sS https://api.github.com/repos/undeadindustries/sagittarius/releases/latest \
  | grep -o '"tag_name": *"[^"]*"' | cut -d'"' -f4)
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m); case "$ARCH" in x86_64) ARCH=amd64 ;; aarch64|arm64) ARCH=arm64 ;; esac
ASSET="sagittarius_${VERSION#v}_${OS}_${ARCH}.tar.gz"

curl -sSLO "https://github.com/undeadindustries/sagittarius/releases/download/${VERSION}/${ASSET}"
tar -xzf "$ASSET"
sudo mv sagittarius /usr/local/bin/
sagittarius --version
```

To pin a specific version instead, set `VERSION=v0.2.0` by hand and skip the first command.

Optionally verify the download against the release's `checksums.txt`:

```bash
curl -sSLO "https://github.com/undeadindustries/sagittarius/releases/download/${VERSION}/checksums.txt"
sha256sum -c checksums.txt --ignore-missing
```

#### Note for macOS Users (Gatekeeper)

Since these binaries are not signed with an Apple Developer Account, macOS Gatekeeper will block execution when first run with a warning that "Apple could not verify sagittarius". 

You can bypass this easily by stripping the quarantine flag right after moving it to your path:

```bash
sudo xattr -d com.apple.quarantine /usr/local/bin/sagittarius
```

### From Source (Build)

```bash
make build
./bin/sagittarius --version
```

Or without Make:

```bash
go build -o bin/sagittarius ./cmd/sagittarius
```

### Keeping Sagittarius up to date

Sagittarius includes a built-in updater. Run `/update` in the TUI to check for a new version, and `/update install` to download and replace the running binary automatically. 

If Sagittarius is installed in a root-owned directory like `/usr/local/bin`, the in-app update may fail with a permission error. In that case, run the headless updater with `sudo`:

```bash
sudo sagittarius --self-update
```

## Development

```bash
make test    # unit tests
make vet     # go vet
make lint    # golangci-lint
make race    # race detector
```

## Terminal UI and rendering

Sagittarius is a single native Go binary with no Node runtime. The UI layer is swappable behind an internal interface, but today everything interactive goes through [Bubble Tea](https://github.com/charmbracelet/bubbletea).

## Configuration & Rules

Sagittarius reads its settings from `~/.sagittarius/settings.json`. Project overrides live in `<repo>/.sagittarius/settings.json` (project wins on merge). API keys belong in environment variables or OS keychain, not in settings files. See [docs/home-directory.md](docs/home-directory.md) for the dual-scope merge rules.

### Rules

You can define custom rules and instructions that the agent must follow. These are placed in `AGENTS.md` files:

- **Global rules:** Create `~/.sagittarius/AGENTS.md`. The agent will apply these rules across all projects.
- **Project rules:** Create an `AGENTS.md` file in the root of your project. The agent will read this file when run within the project directory.

**`AGENTS.md` is yours alone.** The memory commands below never read, write, or delete it, so your standards file stays hand-authored and reviewable. Asking the agent to "add this rule to AGENTS.md" still works — that goes through the normal file tools and shows you a diff first. (`.agents/` is for skills only; an `AGENTS.md` there is not read.)

### Memory

Durable facts the agent learns live in a separate `MEMORY.md`, injected after your rules and marked as reference material so a learned fact never outranks a written standard:

- **`/memory add [--project] <text>`** — appends one dated bullet to `~/.sagittarius/MEMORY.md` (or the repo's `.sagittarius/MEMORY.md` with `--project`).
- **`/memory list`** — numbers every entry with its date and per-file usage against the cap.
- **`/memory remove <n>`** — deletes entry `n` and echoes back what was removed.
- **`/memory compact`** — asks a model to merge duplicates and drop superseded facts, then shows a diff. Nothing is written until you accept.
- The model can call a `save_memory` tool to record a fact itself — it always asks for confirmation, and can only append. Deleting is a user-only action.

Because memory is sent on every request, each file is capped (`sagittarius.memory.maxRunes`, default 8192 runes, `0` = unlimited). Nothing is ever silently truncated or evicted: an add at the ceiling is refused and tells you how to free space.

## Quick reference

Interactive shortcuts, headless flags, and slash commands for the same features where they exist. Full slash-command tree: [docs/reference/commands.md](docs/reference/commands.md).

| Feature | Keyboard (TUI) | CLI flags / parameters |
|---------|----------------|------------------------|
| **Interaction mode** (agent / plan / ask / debug) | `Shift+Tab` (cycle), `Alt+1` … `Alt+4` (direct), `Ctrl+Shift+M` (cycle alias); or `/mode`, `/modes` | `--mode` (`agent`, `plan`, `ask`, `debug`) |
| **Model** (pick or cycle active set) | `Ctrl+/` (forward), `Ctrl+Shift+P` (back); or `/model`, `/models` | `-m`, `--model <id>` (pins model for this run; disables mode-based model routing) |
| **Tool approval** (confirm vs auto-run tools) | Tool cards: Allow once / session / deny; `Alt+Y` turns yolo on or off for this session (off restores the mode you started with); the footer shows the policy | `--approval-mode` (`default`, `autoEdit`, `yolo`); `-y`, `--yolo` (shorthand for yolo; not combinable with `--approval-mode`) |
| **Reasoning effort** | `Alt+R` steps through the levels the current model accepts, then back to its default (this session only; the footer shows `effort:`) | `/reasoning <level>`; `/reasoning save <level>` to keep it |
| **Thinking / reasoning box** | `Ctrl+T` (persists `ui.showThinking`) | — (use `/settings` or per-model `showThinking` in `/models`) |
| **Export last provider request** | `Ctrl+Shift+D`; or `/chat debug` (works while a turn is running) | — |
| **Color theme** | `Alt+T` (persists `ui.theme`) | `/theme` (no startup flag) |
| **Mouse (wheel scroll + drag-to-copy)** | On by default; `Alt+M` turns it off for the session (on again next launch) | `/mouse` (`on`, `off`, `toggle`, `show`) |
| **Background processes** | `Ctrl+B` | — |
| **Scroll conversation** | `PgUp` / `PgDn`, `Shift+Up` / `Shift+Down`; macOS: `Fn+Up` / `Fn+Down` on compact keyboards | — |
| **Prompt history** | `Up` / `Down`, `Ctrl+P` / `Ctrl+N` (at input line boundaries). A recalled slash command loads without its menu, so the arrows keep stepping through history; press `Tab` to open the menu for it | — |
| **New line in input** | `Alt+Enter`, `Shift+Enter`, `Ctrl+J` (`Enter` submits) | — |
| **Cancel in-flight turn** | `Esc` | — |
| **Quit** | `Ctrl+C` when idle; `/quit` | — |
| **Non-interactive turn** | — | `-p`, `--prompt <text>` (also accepts a single positional argument) |
| **Headless output shape** | — | `--output-format` (`text`, `json`, `stream-json`) |
| **Slash command (no TTY)** | — | `--slash "<command>"` (e.g. `--slash "/diff"`; mutually exclusive with `-p`) |
| **Resume session** | `/resume`, `/chat resume` | `--resume`, `-r` (id, index, or `latest`) |
| **List / delete sessions** | — | `--list-sessions`; `--delete-session` (id or index) |
| **Debug logging** | — | `--debug`, `-d` (writes to `~/.sagittarius/logs/sagittarius.log` in the TUI) |
| **Full chat transcript for bug reports** | — | `--log-verbose` (every request/response/tool result to `~/.sagittarius/logs/chat-verbose-<session>.log`; works with or without `--debug`) |
| **Google Chat bridge (TUI + DM)** | — | `--google-chat` (attaches 1:1 Google Chat DM alongside the TUI; see [docs/google-chat.md](docs/google-chat.md)) |
| **Google Chat daemon (no TTY)** | — | `--google-chat-only` (runs headless bridge under systemd or screen) |
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
| `Option+R` (`®`) | `Alt+R` | Step reasoning effort |
| `Option+Y` (`¥`) | `Alt+Y` | Toggle yolo for this session |
| `Option+M` (`µ`) | `Alt+M` | Toggle mouse mode |
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

You can. Sagittarius uses the terminal **alternate screen**, which hides the terminal's own scrollback, so the terminal's native selection can only reach what is on screen. Sagittarius handles selection itself instead:

1. **Drag to copy (default):** click and drag over the conversation. The selection highlights, scrolls when you drag to the top or bottom edge (or turn the mouse wheel mid-drag), and is copied to the clipboard when you release. Text that was wrapped to fit the window is rejoined, so a long command pastes as one line. The status row confirms the copy.
2. **Terminal-native selection:** hold **`Shift`** while dragging (in iTerm2, **`Option`**), or turn mouse mode off with **`Alt+M`** / **`/mouse off`** and drag normally. This only reaches the visible rows.
3. **Last assistant reply:** run **`/copy`** to copy the most recent assistant message (raw markdown, no TUI chrome). Run **`/copy code`** to copy only fenced command/code blocks from that reply.

**Over SSH:** copies go to your local machine's clipboard through the terminal (OSC 52), including inside tmux and GNU screen. Your terminal has to allow it: iTerm2 needs *Applications in terminal may access clipboard*, tmux needs `set -g set-clipboard on` (or `allow-passthrough on`), and most other modern terminals (kitty, WezTerm, Ghostty, Windows Terminal, Alacritty) allow it by default.

**To scroll:** use the mouse wheel, **`PgUp` / `PgDn`**, or **`Shift+Up` / `Shift+Down`**. Keyboard scrollback works whether mouse mode is on or off.

### I can't scroll back

Sagittarius runs in the terminal **alternate screen**, so your terminal's normal scrollback (mouse wheel, scrollbar, or scrolling with two fingers on a trackpad) does **not** move the conversation. That is expected. Use Sagittarius's **in-app** scroll keys instead:

**Windows and Linux**

| Key | Action |
|-----|--------|
| `PgUp` / `PgDn` | Scroll up / down half a page |
| `Shift+Up` / `Shift+Down` | Scroll one line at a time |
| `Alt+M` | Toggle mouse mode (wheel scrolling, on by default) |

**macOS**

| Key | Action |
|-----|--------|
| `Fn+Up` / `Fn+Down` | Scroll up / down half a page (same as Page Up / Page Down on compact Mac keyboards) |
| `Shift+Up` / `Shift+Down` | Scroll one line at a time |
| `Option+M` (`µ`) or `Alt+M` | Toggle mouse mode (wheel scrolling, on by default) |

On a full-size keyboard with dedicated Page Up / Page Down keys, those work too. The composer status row above the input shows both (`Pg↑ Pg↓ or Fn↑ Fn↓`), because a Mac keyboard over SSH is still a Mac keyboard.

The mouse wheel scrolls the conversation by default. If you turned mouse mode off with **`Alt+M`** (or **`Option+M`** on macOS), press it again or run **`/mouse on`**. It is on again at the next launch.

### Why doesn't my terminal scrollback show the conversation after I quit?

The alt-screen is cleared when Sagittarius exits. The exit summary (session stats, resume hint) is printed to normal scrollback after teardown. For a durable transcript, use **`/chat save`**, session JSONL under `~/.sagittarius/tmp/`, or copy passages with the mouse (or `/copy` / `/copy code` for the last reply) before quitting.

### How do I change mode or model quickly?

- **`Shift+Tab`**: cycle modes in order (agent → plan → ask → debug → agent).
- **`Alt+1` … `Alt+4`** (or macOS Option equivalents): jump directly to agent, plan, ask, or debug.
- **`Ctrl+Shift+M`**: cycle modes in order (alias of `Shift+Tab`).
- **`Ctrl+/`** / **`Ctrl+Shift+P`**: cycle active models forward / backward.
- **`Alt+R`** (or **`Option+R`**): step the reasoning effort for the current model. It lasts for this session and resets when you switch models. **`/reasoning save`** keeps a level.
- **`/mode`**, **`/model`**, **`/modes`**: full pickers and override editors.

Every dialog that saves settings has a `Save to:` line under its title; press **Tab** to switch it between Global and Project. `/model`, `/modes`, and `/subagents` open on Project, while `/settings`, `/providers`, and `/models` open on Global. `/model --project provider/model` does the same without the picker. API keys, provider URLs, and provider definitions always stay **global**, so a project file can't send your requests somewhere else. See [docs/home-directory.md](docs/home-directory.md).

### Host Toolkit Checklist

On first launch (or by running **`/toolkit`**), Sagittarius scans your `PATH` for recommended tools (linters, language stacks, sysadmin utilities) and prints install hints for anything missing. You can permanently dismiss the startup scan with **`/toolkit dismiss`**.

### Skills

You can extend the agent's domain knowledge and capabilities by creating skills. Skills are Markdown files named `SKILL.md` (or ending in `.md` inside a skill directory).

**How to create a skill:**

1. Create a new directory for your skill.
2. Inside that directory, create a `SKILL.md` file.
3. Write your instructions, expert context, or playbook for the agent in that Markdown file.

**Where to put skills:**

- **Global skills:** `~/.sagittarius/skills/` (or `~/.agents/skills/`).
- **Project skills:** `<your-project>/.sagittarius/skills/` (or `<your-project>/.agents/skills/`).

The agent discovers these automatically and can activate them when relevant (the `activate_skill` tool declaration advertises the full catalog of available skill names and descriptions so the model and subagents can self-select skills). Use **`/skills`** in the CLI to list or reload them.

A ready-made `verify-after-edit` skill ships in [docs/skills/verify-after-edit/SKILL.md](docs/skills/verify-after-edit/SKILL.md). Copy it into your skills directory to reinforce running lint, format, type-check, and tests after edits.

## Web tools and search

Sagittarius includes built-in web search (`google_web_search`) and web fetch (`web_fetch`) tools, available to all chat models and interaction modes:

- **Web search cascade:**
  1. **Google Search grounding:** Used when a Gemini API key is configured. Provides cited prose and source links.
  2. **Brave Search API:** Used when a Brave key is configured, either via `BRAVE_API_KEY` or stored in the keychain from `/settings` → Secrets. Returns structured organic search results.
  3. **DuckDuckGo HTML search:** Key-free fallback that returns organic results without requiring any API keys.
- **Web fetch:** Downloads and extracts clean text from URLs with SSRF protection (blocking private networks and localhost) and sliding-window rate limiting.

See [docs/web-tools.md](docs/web-tools.md) for full configuration details.

## Working memory

Long conversations get compressed, and compression is lossy: an exact path or decision from two hundred turns ago survives only as a summary. Two tools close that gap. `update_scratchpad` lets the model keep a short note in the system prompt, where compression cannot reach it, and `search_session` scans this session's full transcript for an exact detail that is no longer in context. `/scratchpad` shows or clears the note. See [docs/working-memory.md](docs/working-memory.md) for the token cost and the `sagittarius.scratchpadEnabled` toggle.

## Code quality

Sagittarius keeps code IDE-clean by running each project's own lint, format, type-check, and test tooling. It does not bundle linters. The built-in `run_project_checks` tool auto-detects the stack and runs its checks, and Go projects can opt into `gopls` code intelligence over MCP. See [docs/code-quality.md](docs/code-quality.md).

## License

Apache License 2.0. See [LICENSE](LICENSE).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). By participating, you agree to the [Code of Conduct](CODE_OF_CONDUCT.md).

## Security

Report vulnerabilities per [SECURITY.md](SECURITY.md).
