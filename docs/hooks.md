# Sagittarius Lifecycle Hooks (AD-100)

Sagittarius supports a wire-compatible lifecycle hooks system matching `gemini-cli`. External scripts can observe agent lifecycle events, inject additional prompt context, modify tool inputs, or block execution.

## Overview & Protocol

- **Wire Format:** Input is written as JSON to `stdin`. Output is read as JSON from `stdout`. `stderr` is passed to logging and debug output.
- **Top-Level Key:** Configured under the top-level `"hooks"` object in `~/.sagittarius/settings.json` (global) or `<repo>/.sagittarius/settings.json` (project).
- **Environment Variables:** Each hook process receives `SAGITTARIUS_PROJECT_DIR`, `SAGITTARIUS_CWD`, `SAGITTARIUS_SESSION_ID`, `GEMINI_PROJECT_DIR`, `GEMINI_CWD`, `GEMINI_SESSION_ID`, `CLAUDE_PROJECT_DIR`, and `GEMINI_PLANS_DIR`.
- **Exit Codes:**
  - `0`: Success. `stdout` parsed as JSON (or degraded to plain-text system message if non-JSON).
  - `1`: Non-blocking warning. `stderr` logged as a warning message.
  - `2`: Hard system block. `stderr` becomes the denial reason and cancels execution. This outranks
    `stdout`: a hook that exits `2` while printing `{"decision":"allow"}` still blocks.
- **Timeout:** the per-hook `timeout` is in **seconds**, matching `gemini-cli` and Claude Code, so
  their hook config blocks can be pasted in unchanged. The default is 60s.
- **Sequential groups:** a group marked `"sequential": true` threads each hook's output into the
  next one's input, and stops at the first hook that blocks.

## Supported Lifecycle Events

| Event | Firing Point | Input Payload | Actions / Capabilities |
|---|---|---|---|
| `SessionStart` | Startup, resume, or session clear | `{source: "startup"|"resume"|"clear"}` | Initialize session, wing/room setup |
| `SessionEnd` | Runner shutdown (`Runner.Close`) | `{reason: "exit"}` | Finalize or flush session state (5s timeout) |
| `FirstTurn` | Terminal `StreamDone` on turn 1 | `{prompt, prompt_response}` | Fires **once** per session after turn 1 |
| `BeforeAgent` | Start of `RunTurn` (pre-history) | `{prompt}` | Block turn (`deny`/`block`) or append `additionalContext` |
| `AfterAgent` | Terminal `StreamDone` of turn | `{prompt, prompt_response}` | Observe complete turn response |
| `PreCompress` | Pre-compression trigger | `{trigger: "auto"|"manual"}` | Fire before history compression |
| `BeforeTool` | Pre-tool execution | `{tool_name, tool_input}` | Block tool (`deny`/`block`) or modify `tool_input` |
| `AfterTool` | Post-tool execution | `{tool_name, tool_input, tool_response}` | Observe tool result |

*Note: Each event payload also includes `session_id`, `transcript_path`, `cwd`, `hook_event_name`, `timestamp`, and `turn_index`.*

`prompt` on `AfterAgent` and `FirstTurn` is the prompt for **that** turn, not the first one in the
session. `turn_index` counts user turns in the conversation and continues across `--resume` and
`/chat resume`, so a hook can gate on "every Nth turn" without keeping its own state or parsing the
transcript. Tool results are recorded with the user role internally but are not counted as turns.

A `BeforeTool` hook returning `tool_input` **merges** those keys into the call rather than replacing
it, so a hook can rewrite one argument without having to restate the rest. Rewritten arguments are
re-checked against the project boundary, the interaction-mode gate, and `write_file` validation,
since those ran against the model's original arguments.

## Security & Trust Model

- **Global Hooks:** Configured in `~/.sagittarius/settings.json` are implicitly trusted.
- **Project Hooks:** Configured in `<repo>/.sagittarius/settings.json` are fingerprinted by `project_root + key + command` (SHA-256). Fingerprints are stored in `~/.sagittarius/trusted_hooks.json`. If a project hook is added or modified (e.g. via `git pull`), it is marked untrusted until approved.
- **Headless Mode:** Untrusted project hooks are automatically skipped in headless execution mode.

## Slash Commands

| Command | Description |
|---|---|
| `/hooks` / `/hooks list` | List all loaded lifecycle hooks and their status |
| `/hooks enable <name>` | Enable a specific hook |
| `/hooks disable <name>` | Disable a specific hook |
| `/hooks enable-all` | Globally enable all hooks |
| `/hooks disable-all` | Globally disable all hooks |
| `/hooks reload` | Reload hook configurations from settings files |
| `/hooks test <name>` | Test-run a hook with sample event input |

## Example: automatic MemPalace capture

[MemPalace](https://github.com/undeadindustries/mempalace) ships hook scripts for Claude Code, and
two of them work here unchanged. Its `PreCompact` script maps to `PreCompress` and its `SessionEnd`
script maps to `SessionEnd`; both mine the transcript without needing to count turns.

Its `Stop` auto-save script does **not** port cleanly. That script decides when to save by counting
user messages in the transcript with `mempalace.hook_shell count-human-messages`, which expects
Claude Code's `{"message": {"role": "user"}}` shape. Sagittarius session JSONL uses a top-level
`{"type": "user"}`, so the count is always `0` and the save never fires. Since Sagittarius supplies
`turn_index` directly, the fix is to gate on that instead of parsing the transcript at all:

```bash
#!/bin/bash
# AfterAgent: mine into MemPalace every Nth turn. Never blocks a turn.
set -u
INTERVAL="${MEMPAL_SAVE_INTERVAL:-15}"
STATE_DIR="$HOME/.mempalace/hook_state"; mkdir -p "$STATE_DIR"
PY="${MEMPAL_PYTHON:-$(command -v python3)}"

payload="$(cat)"
turn=$(printf '%s' "$payload" | "$PY" -c 'import json,sys; print(json.load(sys.stdin).get("turn_index",0))' 2>/dev/null || echo 0)
transcript=$(printf '%s' "$payload" | "$PY" -c 'import json,sys; print(json.load(sys.stdin).get("transcript_path",""))' 2>/dev/null)

if [ "$turn" -le 0 ] || [ $((turn % INTERVAL)) -ne 0 ] || [ ! -f "$transcript" ]; then
  echo '{}'; exit 0
fi

if ! "$PY" -c 'import mempalace' >/dev/null 2>&1; then
  echo "$transcript" >> "$STATE_DIR/unmined.spool"
  printf '{"systemMessage":"MemPalace unavailable - capture paused, transcript spooled."}'
  exit 0
fi

( "$PY" -m mempalace mine "$(dirname "$transcript")" --mode convos >>"$STATE_DIR/hook.log" 2>&1 \
    || echo "$transcript" >> "$STATE_DIR/unmined.spool" ) >/dev/null 2>&1 </dev/null &
disown 2>/dev/null || true
echo '{}'
```

Wire it up in `~/.sagittarius/settings.json`, where hooks are implicitly trusted:

```json
{
  "hooks": {
    "AfterAgent": [
      { "hooks": [ {
        "type": "command",
        "name": "mempalace-autosave",
        "command": "$HOME/.sagittarius/hooks/mempal-autosave.sh",
        "timeout": 5,
        "env": { "MEMPAL_SAVE_INTERVAL": "15" }
      } ] }
    ],
    "PreCompress": [
      { "hooks": [ {
        "type": "command",
        "name": "mempalace-precompact",
        "command": "$HOME/src/mempalace/hooks/mempal_precompact_hook.sh",
        "timeout": 60
      } ] }
    ],
    "SessionEnd": [
      { "hooks": [ {
        "type": "command",
        "name": "mempalace-session-end",
        "command": "$HOME/src/mempalace/hooks/mempal_session_end_hook.sh",
        "timeout": 10
      } ] }
    ]
  }
}
```

`$HOME` works because commands run through `sh -c`.

### Failure behavior

Some conventions worth copying for any hook that talks to an external service:

- **Never block.** These all exit `0` unconditionally. A memory or telemetry integration that can
  stop the user from working is worse than one that is switched off.
- **Report rather than repair.** A hook fires mid-turn with nobody watching, under a timeout, and
  possibly alongside another Sagittarius process. That is the wrong place to attempt automatic
  repair of a database.
- **Be silent on success, visible on failure.** The real hazard is not a failed save, it is a week
  of failed saves nobody noticed. `systemMessage` surfaces one line in the TUI when capture stops.
- **Spool instead of retrying in place.** Session JSONL is append-only and never discarded, so a
  missed mine loses nothing permanently. Recording the path and re-running it from a `SessionStart`
  hook turns an outage into a catch-up on next launch.

If MemPalace is installed in a virtualenv or via pipx, it will not be importable from the system
`python3`. Point `MEMPAL_PYTHON` at the right interpreter; MemPalace's own scripts honor the same
variable.
