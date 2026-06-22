# Agent-driven testing

Sagittarius can be exercised end-to-end without a terminal, so a Cursor agent
(or CI) can create apps, route models per mode, and verify tool execution and
local snapshots directly from the command line. This guide covers the headless
flags, the `stream-json` contract, the paired interactive workflow, and the
end-to-end harness.

## Headless agent recipes

Run a single non-interactive turn with `-p`. By default destructive tools are
denied headlessly; pass `--yolo` to let the model write files and run shell
commands.

```bash
# Create a small app (writes allowed).
sagittarius --yolo -p "Create a Go hello-world program in main.go and a go.mod."

# Read-only inspection in ask mode (writes and shell are blocked).
sagittarius --mode ask -p "Summarize what main.go does."

# Compare per-mode model routing by repeating with different modes.
sagittarius --mode plan -p "Outline a refactor of the config loader."
sagittarius --mode agent -p "Implement the first step of that refactor."
```

Approval policy and interaction mode are independent axes:

| Axis | Flag | Values |
|------|------|--------|
| Tool approval | `--approval-mode`, `--yolo`/`-y` | `default`, `autoEdit`, `yolo` |
| Interaction mode | `--mode` | `agent`, `plan`, `ask`, `debug` |

`ask` and `plan` enforce read-only tool policy in the scheduler regardless of the
approval policy, so `--mode ask --yolo` still blocks writes (see
[interaction-modes.md](interaction-modes.md)).

## Slash commands without a TTY

`--slash` runs one slash command headlessly and exits. This is how an agent
inspects snapshots and modes after a headless write:

```bash
sagittarius --slash "/mode show"
sagittarius --slash "/diff"
sagittarius --slash "/undo"
sagittarius --slash "/help"
```

Commands that open an interactive dialog (bare `/providers`, `/models`) print a
message and exit 2 rather than crashing.

Because each invocation is a separate process, pin a shared session id so a write
and a later `/diff` or `/undo` see the same snapshot history:

```bash
export SAGITTARIUS_SESSION_ID="agent-run-1"
cd /tmp/sag-test
sagittarius --yolo --output-format stream-json -p "create hello.txt with content hi"
sagittarius --slash "/diff"   # shows the diff for hello.txt
sagittarius --slash "/undo"   # removes hello.txt
```

## Observability: `--output-format stream-json`

`stream-json` emits one JSON object per line, so an agent can verify tool
execution from stdout:

| Type | Shape |
|------|-------|
| `text` | `{"type":"text","text":"<delta>"}` |
| `tool_start` | `{"type":"tool_start","tool":"<name>"}` |
| `tool_result` | `{"type":"tool_result","tool":"<name>","text":"<summary>"}` |
| `info` | `{"type":"info","text":"<message>"}` |
| `error` | `{"type":"error","error":"<message>"}` |

```bash
sagittarius --yolo --output-format stream-json -p "write notes.md with a TODO list"
# {"type":"tool_start","tool":"write_file"}
# {"type":"tool_result","tool":"write_file","text":"wrote notes.md"}
# {"type":"text","text":"Done."}
```

## Paired interactive workflow

For the full TUI (themes, dialogs, confirm prompts) the agent does not automate
Bubble Tea. Instead, run a paired workflow:

1. You run interactive `sagittarius` in a Cursor terminal.
2. The agent supplies prompts for you to paste.
3. The agent verifies the result out of band: workspace files, the snapshot
   index under `~/.sagittarius/tmp/<slug>/snapshots/<sessionId>.jsonl`, and any
   `settings.json` changes.

No code automation is required — the headless flags above cover everything an
agent needs to drive and verify on its own.

## Running the E2E harness

The end-to-end suite lives in [`tests/e2e/`](../tests/e2e) and drives the
compiled binary as a subprocess.

```bash
# Live mode (default): real providers, cheap models. Needs at least one key.
make e2e

# Key-free, deterministic mock mode.
make e2e-mock

# Or via the script (adds an up-front key check in live mode).
scripts/smoke-e2e.sh         # live
scripts/smoke-e2e.sh --mock  # mock
```

Live mode discovers configured providers (Gemini, OpenAI, OpenAI Responses) via
the same credential resolution the binary uses, and **skips** providers without
a usable key — it fails only when the script finds zero keys. Control cost by
overriding the per-provider model:

```bash
SAGITTARIUS_E2E_MODEL_GEMINI=gemini-2.0-flash \
SAGITTARIUS_E2E_MODEL_OPENAI=gpt-4o-mini \
make e2e
```

A plain `go test ./...` neither runs live scenarios (they require
`SAGITTARIUS_E2E_LIVE=1`, set by `make e2e`) nor the mock scenarios (they require
`SAGITTARIUS_E2E_MOCK=1`), so the default test run never makes network calls.
