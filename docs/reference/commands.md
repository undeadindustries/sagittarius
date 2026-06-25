# CLI commands

Sagittarius supports built-in slash commands for session control and provider
configuration. Commands start with `/`. Provider credentials are managed inside
the `/providers` wizard rather than a separate `/auth` command.

This document mirrors the fork reference (`gemini-cli/docs/reference/commands.md`)
for the subset implemented in Sagittarius. Commands not listed here are deferred
to later phases — see [Deferred commands](#deferred-commands).

## Slash commands (`/`)

### `/help`

- **Description:** List slash commands and subcommands with short descriptions.
- **Usage:** `/help`

### `/quit`

- **Description:** Exit the interactive session.
- **Usage:** `/quit`
- **Note:** `Ctrl+C` exits when idle. While a turn is running, `Esc` (or `Ctrl+C`)
  cancels just that turn; a second `Ctrl+C` then exits.

### Tool confirmations

When a tool needs approval (e.g. `write_file`, `run_shell_command` in the
default policy), a band above the input shows the action — with a colorized diff
preview for `write_file` — and three choices: **Allow once** (`1`/`y`), **Allow
for this session** (`2`, skips later prompts for that tool), and **No**
(`3`/`n`/`Esc`). Pick with the arrow keys + `Enter` or press the key directly.

## Keyboard shortcuts

These work in the interactive TUI in addition to the slash commands above.

| Key | Action |
|-----|--------|
| `Alt+1` / `Alt+2` / `Alt+3` / `Alt+4` | Switch to agent / plan / ask / debug mode |
| `Ctrl+Shift+M` | Cycle interaction mode (agent → plan → ask → debug) |
| `Ctrl+/` | Cycle forward through active models |
| `Ctrl+Shift+P` | Cycle backward through active models |
| `Alt+T` | Cycle the color theme (default ↔ greyscale) |
| `Ctrl+T` | Toggle the thinking ("reasoning") box |
| `Alt+M` | Toggle mouse-wheel scrolling (see below) |
| `Ctrl+B` | Open the background process viewer |
| `PgUp` / `PgDn` / `Shift+Up` / `Shift+Down` | Scroll the conversation |
| `Up` / `Down` / `Ctrl+P` / `Ctrl+N` | Navigate prompt history (at the input boundaries) |
| `Esc` | Cancel the in-flight turn (second `Esc` force-stops) |
| `Ctrl+C` | Cancel the turn, or quit when idle |

`Alt+digit` is used for direct mode selection because terminals cannot
distinguish `Ctrl+digit` from the plain digit. On macOS, if your terminal sends
special characters for `Option+key` instead of Alt sequences, Sagittarius accepts
the characters `¡`, `™`, `£`, `¢` as aliases for `Alt+1..4`, `†` for `Alt+T`,
and `µ` for `Alt+M`, so the shortcuts work out of the box.

### Mouse scrolling vs. text selection

Mouse-wheel scrolling is **off by default** so the terminal's native click-drag
text selection works (for copy/paste). Enable wheel scrolling with `Alt+M` or
`/mouse on`; while it is on, hold `Shift` to select text. Keyboard scrollback
(`PgUp`/`PgDn`, `Shift+Up`/`Down`) works regardless. The setting is per-session
and resets to off on the next launch.

### `/mouse`

- **Description:** Toggle mouse-wheel scrolling of the conversation.
- **Usage:** `/mouse` (toggle), `/mouse on`, `/mouse off`, `/mouse show`.

### `/theme`

- **Description:** Show or switch the TUI color theme (persisted).
- **Usage:** `/theme` (show), `/theme default`, `/theme greyscale`. `Alt+T`
  cycles between the two live.

### `/providers`

- **Description:** Manage provider connections — edit definitions, API keys, and
  activate models per provider.
- **Menu-first:** `/providers` opens directly at a provider list (built-ins first,
  then custom providers alphabetically). Select a provider to open its edit sheet.
  Press `a` to add a custom provider; press `x` on a custom provider to open a
  delete confirmation screen (`y` or Enter confirms, Esc cancels). Built-ins
  (Gemini, OpenAI, OpenRouter) cannot be deleted.
- **Adding a custom provider (`a`):**
  1. **Provider name** — display name (required).
  2. **URL or host** — full URL (`http://127.0.0.1:8000`) or bare host (`127.0.0.1`).
  3. **Port** — shown only when the URL above has no port; defaults to `8000`.
  4. **Wire format** — toggle between `openai-chat` (default) and `openai-responses`.
  5. **API key env var** — optional environment variable name.
  6. **API key** — optional; stored in OS keychain.
  7. **Provider id** — auto-generated from the URL; edit to override before confirm.
  After submission the wizard discovers available models and prompts you to pick a default.
- **Edit sheet items (custom providers):**
  - **Provider name**, **URL / host**, **Port** — decomposed fields that compose back to `baseUrl` on save.
  - **Wire format** toggle.
  - **API key** and **API key env var**.
  - **Manage models…** — browse the provider's discovered models and toggle which
    are **active** (Space toggles one, `A` toggles all/none). Only active models
    appear in `/model` and `/models`. On a fresh provider only the configured
    default model is pre-checked; opt in to more before saving. The checked subset
    is saved to `providers.<id>.activeModels`. If you deactivate the model currently
    in use, the live model is automatically switched to the first still-active model.
  - **Provider-wide settings** (wire-format-gated): `temperature`, `contextLimit`,
    `toolCallParsing`, and for `openai-responses` also `reasoningEffort`, `useResponseChaining`.
  - **Reset all** — remove all provider-level instance overrides.
- **Delete (`x` on custom provider):** shows a confirmation screen; press `y` or Enter to confirm removal of the definition, instance overrides, and stored API key. Press Esc to cancel.

### `/model`

- **Description:** Pick the **current `{Provider}/{Model}`** from the global active list.
- **Menu-first:** `/model` (no argument) opens an interactive list spanning all
  providers' activated models. Each entry is displayed as `{Provider}/{Model}`.
  Selecting one atomically switches the active provider and its live model in a
  single step, then rebuilds the runner.
- **Direct argument:** `/model gemini/gemini-2.5-pro` switches directly.
- **Autocomplete:** Tab-completes `{Provider}/{Model}` pairs.
- **`Ctrl+/`:** Cycles globally across all active models (wraps around). The status
  bar shows the resolved model after each cycle.

### `/models`

- **Description:** Edit **per-model settings** — temperature, context limit, and
  reasoning effort — for any active `{Provider}/{Model}` pair.
- **Menu-first:** `/models` opens a global model list. Select a model to open its
  settings submenu. Changes are saved to `providers.<id>.models.<model>` in
  `settings.json` and take effect immediately for the active model.

### `/system-prompt`

- **Description:** Set the **project-wide** system-prompt personality (programmer,
  sysadmin, personal assistant, creative assistant × full/lite variants).
- **Menu-first:** `/system-prompt` opens a preset picker. Pass a preset id directly
  for headless use (e.g. `--slash "/system-prompt programmer-lite"`).
- **Persistence:** Saved to `<repo>/.sagittarius/settings.json` under
  `sagittarius.systemPrompt` and merged over the global default for the current
  workspace. Use `/providers` to set a per-provider override instead.

### `/modes`

- **Description:** Edit **mode overrides** — assign a `{Provider}/{Model}` to any
  interaction mode or clear an existing override to restore default routing.
- **Menu-first:** `/modes` (also reachable via `/mode settings`) shows each mode
  with its current `{Provider}/{Model}` override, or "default" when none is set.
  Selecting a mode opens a model picker; selecting "Clear override" resets it.
- **Scope:** The TUI shows an "Apply to" scope row (Tab to focus). Overrides
  default to **project** scope so each repo can have its own routing config.
- **Effect:** A provider-qualified override (`provider + model`) causes Sagittarius
  to rebuild the generator for the new provider when the mode activates, then revert
  when leaving that mode.

#### Headless mode-override subcommands

- `/modes override <agent|plan|ask|debug> <Provider/Model> [global|project]`
  — Persist a mode routing override to the specified scope (defaults to project).
  Example: `--slash "/modes override plan openrouter/qwen/qwen3-235b-a22b project"`
- `/modes clear <agent|plan|ask|debug> [global|project]`
  — Remove the override for that mode from the specified scope.

### `/settings`

- **Description:** Browse and edit **global and project settings** in a curated
  list grouped by category.
- **Scope radio:** Tab switches focus to the scope selector; arrow keys change
  between Global (`~/.sagittarius/settings.json`) and Project
  (`<repo>/.sagittarius/settings.json`). Values shown are from the selected scope
  file only (not merged), with a `*` on any key explicitly set in that scope.
- **Editing:**
  - **Bool** — Enter or Space toggles the value in-place.
  - **Enum** — Enter cycles through the allowed choices.
  - **String / Int** — Enter opens a text editor; Esc cancels; Enter again saves.
  - `Ctrl+L` — Clears the key from the selected scope only; the other scope or
    the built-in default takes over.
- **Categories:** General (`sagittarius.maxToolRounds`), UI (`ui.theme`,
  `ui.showThinking`, `ui.hideBanner`), Security (`security.projectBoundary.enforce`),
  Snapshots (`sagittarius.snapshots.*`), Verify (`sagittarius.verify.*`).
- **Persistence:** Changes are saved immediately to the target scope file and take
  effect in the current session. Provider API keys and definitions are always global
  (edit them in `/providers`).

### `/memory`

- **Description:** Manage project memory files (`AGENTS.md`).

#### Sub-commands

- **`reload`**
  - **Description:** Re-read memory files into the system prompt.
  - **Usage:** `/memory reload`

### `/skills`

- **Description:** Manage agent skills discovered from `SKILL.md` files.

#### Sub-commands

- **`list`**
  - **Description:** List discovered skills (user, workspace, extension paths).
  - **Usage:** `/skills list` or `/skills`
- **`reload`**
  - **Description:** Rescan skill directories and refresh the `activate_skill` tool schema.
  - **Usage:** `/skills reload`

### `/mcp`

- **Description:** Manage MCP servers configured in `settings.json` (`mcpServers`).
  Bare `/mcp` opens an interactive wizard to add, edit, enable/disable, remove,
  and reload servers. Extension-provided servers are shown read-only. Bearer
  tokens entered in the wizard are stored in the credentials layer, never in
  `settings.json`.
- **Scope:** When adding or editing a server, an "Apply to" scope row (Tab to
  focus) lets you save the server to the **project** file (default) or the
  **global** file. The server list shows merged results from both scopes; each
  row's scope is resolved automatically when editing or removing.

#### Sub-commands

- **`list`**
  - **Description:** Show MCP server connection status and discovered tool counts (text).
  - **Usage:** `/mcp list`
- **`reload`**
  - **Description:** Reconnect MCP servers and rediscover tools.
  - **Usage:** `/mcp reload`

See also: [MCP server configuration](../tools/mcp-server.md).

### `/tools`

- **Description:** Browse the effective tool inventory. Bare `/tools` opens an
  interactive view with two sections: built-in Sagittarius tools (read-only,
  labeled **not editable**) and MCP tools grouped by server. For MCP tools,
  Space toggles enable/disable, which persists each server's `includeTools` /
  `excludeTools` filter and reloads the registry. The footer links to the `/mcp`
  wizard for server management.

#### Sub-commands

- **`list`**
  - **Description:** List built-in and MCP tools as text, with MCP tools marked `[on]`/`[off]`.
  - **Usage:** `/tools list`
- **`desc`**
  - **Description:** List tools with descriptions.
  - **Usage:** `/tools desc`

### `/agents`

- **Description:** Manage discovered local agent definitions (stub registry — execution deferred).

#### Sub-commands

- **`list`**
  - **Description:** List agent definitions from user/project/extension paths.
  - **Usage:** `/agents list` or `/agents`
- **`reload`**
  - **Description:** Rescan agent markdown definitions.
  - **Usage:** `/agents reload`

### `/reasoning`

- **Description:** Show or override reasoning effort for OpenAI Responses API providers (`wireFormat: openai-responses`).

#### Sub-commands

- **`show`**
  - **Description:** Show the resolved reasoning effort and whether it comes from session override or provider settings.
  - **Usage:** `/reasoning` or `/reasoning show`
- **`clear`**
  - **Description:** Drop the session-only override (does not change `settings.json`).
  - **Usage:** `/reasoning clear`
- **`save <level>`**
  - **Description:** Persist `<level>` to `providers.<active>.reasoningEffort`.
  - **Usage:** `/reasoning save low`
- **`<minimal|low|medium|high>`**
  - **Description:** Set a session-only reasoning override (not persisted).
  - **Usage:** `/reasoning medium`

#### Notes

- Only applies when the active provider uses `openai-responses`. Other wire formats return an actionable “not applicable” message.

### `/diff`

- **Description:** Show the net unified diff of files Sagittarius changed this session (Sagittarius-specific; no fork equivalent).
- **Usage:** `/diff` for all changed files, or `/diff <path>` to filter by a path substring.
- **Notes:** Tracks `write_file` changes only. See [snapshots-and-undo.md](../snapshots-and-undo.md).

### `/undo`

- **Description:** Revert the most recent file change recorded this session (Sagittarius-specific; no fork equivalent).
- **Usage:** `/undo` reverts the last change; `/undo <n>` reverts the last `n` (most recent first).
- **Notes:** Restores prior file content (or removes newly created files). Disabled when `sagittarius.snapshots.enabled` is `false`.

## Headless CLI flags

These flags drive Sagittarius without a terminal, which is how agents (and CI)
exercise it. See [agent-testing.md](../agent-testing.md) for end-to-end recipes.

| Flag | Purpose |
|------|---------|
| `-p`, `--prompt <text>` | Run a single non-interactive turn and exit. |
| `--output-format <text\|json\|stream-json>` | Headless output shape. `stream-json` emits one JSON object per line. |
| `--approval-mode <default\|autoEdit\|yolo>` | Tool approval policy. `default` denies destructive tools headlessly; `yolo` runs all tools (path validation still applies). The fork alias `auto_edit` maps to `autoEdit`. |
| `-y`, `--yolo` | Shorthand for `--approval-mode=yolo`. Cannot be combined with `--approval-mode`. |
| `--mode <agent\|plan\|ask\|debug>` | Interaction mode for this run, overriding `sagittarius.defaultMode`. `ask` and `plan` enforce read-only tool policy. The fork's `--approval-mode plan` is not accepted; use `--mode plan` (AD-022). |
| `--slash <command>` | Run a single slash command headlessly (e.g. `--slash "/mode show"`, `--slash "/diff"`, `--slash "/undo"`) and exit. Mutually exclusive with `-p`. Commands that open an interactive dialog (bare `/providers`, `/models`) print a message and exit 2. |

The `stream-json` format emits these line types:

| Type | Shape |
|------|-------|
| `text` | `{"type":"text","text":"<delta>"}` |
| `tool_start` | `{"type":"tool_start","tool":"<name>"}` |
| `tool_result` | `{"type":"tool_result","tool":"<name>","text":"<summary>"}` |
| `info` | `{"type":"info","text":"<message>"}` |
| `error` | `{"type":"error","error":"<message>"}` |

`SAGITTARIUS_SESSION_ID` pins the session id across invocations so a headless
write and a later `--slash "/diff"` or `--slash "/undo"` share the same snapshot
history (see [snapshots-and-undo.md](../snapshots-and-undo.md)).

## Deferred commands

The following fork commands are **not** implemented yet. They will be added
incrementally; track gaps in `AGENTS.md`.

| Command | Planned phase |
|---------|----------------|
| `/bug`, `/commands`, `/directory`, `/extensions` | Post-parity / incremental |
| `/mcp auth`, `/mcp enable`/`disable` | Phase 12+ incremental |
| `/skills enable`/`disable`/`link` | Phase 12+ incremental |
| `/agents enable`/`disable`/`config` | Phase 12+ incremental |
| `/auth signin` / OAuth dialogs | Deferred auth paths |
| ACP headless registry | Post-parity |

Implemented: `/about`, `/chat`, `/clear`, `/compress`, `/copy`, `/diff`, `/init`,
`/memory reload`, `/mcp` (list, reload, add/edit/remove wizard), `/modes` (override,
clear headlessly), `/model`, `/models`, `/mouse`, `/reasoning`, `/resume`,
`/settings` (curated browser), `/skills` (list, reload), `/agents` (list, reload),
`/stats`, `/system-prompt`, `/theme`, `/tools` (list, desc, enable/disable),
`/undo`, `activate_skill` tool.
