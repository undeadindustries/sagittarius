```
     · ✦ ·
  »»»»»»———➤
  S A G I T T A R I U S
```

# Sagittarius

Sagittarius is a terminal AI agent for software engineering, system administration, and IT operations. It is one native Go binary. It treats your repositories and servers as live production systems, where uptime, safety, and predictable behavior matter more than speed.

Sagittarius started as a 1:1 Go port of `gemini-cli`. Upstream development on `gemini-cli` ended, and the alternative forks were brittle. Sagittarius grew into an independent agent harness for large software projects, server administration, and workflow automation.

We test Sagittarius against OpenCode, Hermes, and DeepSeek. The goal of these comparisons is safer code changes, tighter scope, and predictable tool use during server maintenance.

## Why Sagittarius

**It does not let a repository take over the agent.** Settings have two scopes: global (`~/.sagittarius/settings.json`) and project (`<repo>/.sagittarius/settings.json`). API keys, provider URLs, wire formats, and provider definitions come only from the global file. A cloned repository cannot send your requests to a different endpoint. Keys live in environment variables, the OS keychain, or an encrypted file, never in settings.

**It can undo what it wrote.** Sagittarius snapshots each file before the agent changes it. `/diff` shows what changed in the session, and `/undo` reverts it.

**It has personas for code and for servers.** The `programmer` and `sysadmin` system prompts each have a full and a lite variant. The sysadmin prompt detects the OS, release, init system, and package manager before it acts. It detects them again on the target host when it works over SSH or inside a container.

**It stays in scope.** The system prompts tell the model to change only what the request needs. They also tell it to prefer the standard library, then dependencies the project already uses. After each write, Sagittarius runs the project's own checks. Linter warnings on lines the session did not change go to you, not to the model, so a one-line fix does not turn into a refactor.

**It has a real shell.** The shell tool runs in a PTY. Press `Tab` on a running command to type into it, for example a `sudo` password or an interactive prompt. Long-running processes can move to the background, and `Ctrl+B` lists them.

**It talks to the providers you already use.** Sagittarius has native adapters for Google Gemini, OpenAI Chat Completions (OpenRouter, DeepSeek, local vLLM, llama-server, and other compatible endpoints), and the OpenAI Responses API. A connect timeout and a first-token timeout catch a server that accepts a request and then stops responding. No Node.js or Python runtime is necessary.

**Each mode can use a different model.** The modes are `agent`, `plan` (read-only, except for `docs/plans/`), `ask` (read-only), and `debug`. You can route each mode to a different provider and model, and switch with `Shift+Tab`.

**It works over SSH and without a terminal.** The Bubble Tea TUI has in-app drag selection and rejoins wrapped lines when you copy. Over SSH and tmux, it copies to your local clipboard with OSC 52. Headless mode (`-p`, `--slash`, `--output-format stream-json`) runs in scripts and CI. Sagittarius can also export Harbor ATIF v1.7 trajectories and attach to a Google Chat DM.

## Installation

### Homebrew (macOS and Linux)

```bash
brew trust undeadindustries/tap
brew install undeadindustries/tap/sagittarius
```

### GitHub Releases

[GitHub Releases](https://github.com/undeadindustries/sagittarius/releases) has binaries for Linux and macOS (amd64 and arm64). This script finds your platform and the latest release, then installs into `/usr/local/bin`:

```bash
VERSION=$(curl -sS https://api.github.com/repos/undeadindustries/sagittarius/releases/latest \
  | grep -o '"tag_name": *"[^"]*"' | cut -d'"' -f4)
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m); case "$ARCH" in x86_64) ARCH=amd64 ;; aarch64|arm64) ARCH=arm64 ;; esac
ASSET="sagittarius_${VERSION#v}_${OS}_${ARCH}.tar.gz"

curl -sSLO "https://github.com/undeadindustries/sagittarius/releases/download/${VERSION}/${ASSET}"
curl -sSLO "https://github.com/undeadindustries/sagittarius/releases/download/${VERSION}/checksums.txt"
sha256sum -c checksums.txt --ignore-missing
tar -xzf "$ASSET"
sudo mv sagittarius /usr/local/bin/
sagittarius --version
```

To pin a version, set `VERSION` by hand (for example `VERSION=v0.25.0`) and skip the first command.

The macOS binaries are not signed with an Apple Developer account, so Gatekeeper blocks the first run. Remove the quarantine flag after you install:

```bash
sudo xattr -d com.apple.quarantine /usr/local/bin/sagittarius
```

### From source

Building from source needs Go 1.26.9 or later ([go.dev/dl](https://go.dev/dl/)).

```bash
make build
./bin/sagittarius --version
```

### Updates

Run `/update` in the TUI to check for a new version, and `/update install` to replace the binary. If the binary is in a root-owned directory such as `/usr/local/bin`, run `sudo sagittarius --self-update` instead.

## Quick start

Run `sagittarius` in a project directory. On the first launch, a setup screen asks for a provider (Gemini, OpenRouter, or a custom OpenAI-compatible endpoint), an API key, and a starting model. Later, use `/providers` and `/models` to change them.

Write your standards in `AGENTS.md`. Sagittarius reads `~/.sagittarius/AGENTS.md` for all projects and `AGENTS.md` in the project root for that project. The agent never edits these files through its memory tools. Facts it learns go to a separate `MEMORY.md`, which you manage with `/memory`.

The most common controls:

| Action | How |
|--------|-----|
| Switch mode (agent, plan, ask, debug) | `Shift+Tab`, or `Alt+1` … `Alt+4` |
| Cycle models | `Ctrl+/` and `Ctrl+Shift+P` |
| Run tools without asking for this session | `Alt+Y` (yolo on or off) |
| Step the reasoning effort | `Alt+R` |
| Show or hide the thinking box | `Ctrl+T` |
| Copy the last reply, or only its code | `/copy`, `/copy code` |
| Run one prompt without the TUI | `sagittarius -p "your prompt"` |
| Resume a session | `sagittarius --resume latest`, or `/resume` |

The complete keyboard reference, macOS key aliases, scrolling, and copy over SSH are in [docs/terminal-and-keyboard.md](docs/terminal-and-keyboard.md). All slash commands and flags are in [docs/reference/commands.md](docs/reference/commands.md).

## Documentation

**Usage and reference**

- [Terminal, keyboard, and mouse](docs/terminal-and-keyboard.md): shortcuts, screen layout, scrolling, copy and paste, FAQ
- [Commands and flags](docs/reference/commands.md): every slash command and headless flag
- [Settings](docs/reference/settings.md): every `settings.json` key
- [Home directory](docs/home-directory.md): `~/.sagittarius` layout and the global and project merge rules
- [Themes](docs/tui-themes.md)

**Agent behavior and tools**

- [Interaction modes](docs/interaction-modes.md): agent, plan, ask, debug, and per-mode model routing
- [System prompts](docs/system-prompts.md): personas, variants, and scope rules
- [Shell execution](docs/shell-execution.md): the PTY shell and background processes
- [Snapshots and undo](docs/snapshots-and-undo.md): `/diff` and `/undo`
- [Code quality](docs/code-quality.md): project checks and LSP diagnostics
- [Subagents](docs/subagents.md): research and coding children
- [Working memory](docs/working-memory.md): the scratchpad and session search
- [Web tools](docs/web-tools.md): search and fetch
- [MCP servers](docs/tools/mcp-server.md)
- [Hooks](docs/hooks.md)
- [Google Chat bridge](docs/google-chat.md)
- [Example skill: verify-after-edit](docs/skills/verify-after-edit/SKILL.md)

**Testing and benchmarks**

- [Agent testing](docs/agent-testing.md): headless end-to-end recipes
- [Benchmarks](docs/benchmarks.md): Harbor trials
- [Trajectory analysis](docs/trajectory-analysis.md): ATIF export and diagnostics

## Development

```bash
make test    # unit tests
make vet     # go vet
make lint    # golangci-lint
make race    # race detector
```

## License

Apache License 2.0. See [LICENSE](LICENSE).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). By participating, you agree to the [Code of Conduct](CODE_OF_CONDUCT.md).

## Security

Report vulnerabilities as described in [SECURITY.md](SECURITY.md).
