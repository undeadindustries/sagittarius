# CLI commands

Sagittarius supports built-in slash commands for session control and provider
configuration. Commands start with `/`. Provider credentials are managed inside
the `/providers` wizard rather than a separate `/auth` command.

This document mirrors the fork reference (`gemini-cli/docs/reference/commands.md`)
for the subset implemented in Sagittarius. Commands not listed here are deferred
to later phases — see [Deferred commands](#deferred-commands).

## User shell (`!` / `/run`)

- **Description:** Run a shell command in this session without sending it to the model.
- **Usage:** `!ls -la /tmp` or `/run ls -la /tmp`
- **TUI:** Output appears as a Shell tool card. While it runs, **Tab** moves typing into the PTY (sudo password, vim, htop). **Shift+Tab** returns to the composer. Esc still cancels the command when you are not focused in the PTY; when focused, Esc goes to the process.
- **Headless:** `sagittarius -p '!echo hi'` prints the command output and exits with the command's status.
- **Not recorded:** The command and its output are not added to conversation history or session JSONL.

## Slash commands (`/`)

### `/help`

- **Description:** List slash commands and subcommands with short descriptions.
- **Usage:** `/help`

### `/goal`

- **Description:** Manage autonomous run-until-done goals. Starts an objective that evaluates automatically across turns without user input until complete or blocked.
- **Usage:**
  - `/goal <objective>`: Starts a new goal (alias for `/goal start`). Requires agent mode.
  - `/goal status`: Show current goal status, turn counts, and evaluator notes.
  - `/goal pause`: Pauses the active goal so the user can intervene.
  - `/goal resume`: Resumes a paused goal.
  - `/goal clear`: Removes the goal entirely (also `/goal cancel`, `/goal stop`).
  - `/goal complete`: Manually mark the goal as achieved.
  - `/goal block`: Manually mark the goal as blocked.
- **Note:** Recommended to run under the `yolo` approval mode (`--yolo` or `/approval yolo`), otherwise tool confirmations will block the loop and require manual intervention anyway.
- **Evaluator:** After each worker turn a read-only judge agent (ask-mode tools only: `read_file`, `grep_search`, `find_symbol`, `run_project_checks`) verifies the objective against the working tree. It uses its own system prompt and does not share the worker persona. Configure `sagittarius.goal.evaluatorProvider` / `evaluatorModel` so a different model family does the judging; leaving them empty means the worker grades its own work, which `/goal start` and `/goal status` both state explicitly. The judge is capped at 6 tool rounds and 120 seconds (override with `evaluatorTimeout`). An unparseable judge reply is treated as not-done, not a dead goal.

### `/grill`

- **Description:** Socratic interrogation mode — the inverse of `/goal`. Instead
  of running autonomously, the agent interviews you one structured question at a
  time (recommended answer + free-text "Other" escape hatch), explores the
  codebase to resolve anything it can verify itself, refuses to write code while
  interrogating, and on `/grill done` generates a spec markdown file capturing
  every resolved decision. Requires agent mode.
- **Usage:**
  - `/grill <topic>`: Starts a new grill session on `<topic>` (alias for `/grill start`). The agent immediately asks its first question.
  - `/grill status`: Show the current topic, status, question count, and any note.
  - `/grill pause [note]`: Pauses interrogation so you can go do something else; runs even mid-turn.
  - `/grill resume [note]`: Resumes a paused session.
  - `/grill done` (alias `/grill finish`): Ends the interrogation, lifts the read-only gate, and has the agent write the spec (default `docs/specs/<topic>.md`) from the recorded decisions.
  - `/grill clear` (alias `/grill stop`): Cancels the session without writing a spec.
- **Read-only while active:** Tools other than the structured `ask_user` question
  tool are blocked (mirrors `/plan`'s tool gate) until you run `/grill done`,
  which switches the session to `summarizing` and allows the final `write_file`.
- **Answering questions:** The TUI renders each question as a numbered picker
  (arrow keys or digit to choose, `Enter` to confirm); the last option is always
  "Other" for a free-text answer.
- **Config:** `sagittarius.grill.specDir` (default `docs/specs`), `maxQuestions`,
  and `recommend` (whether the agent should suggest an answer) — see
  `/settings`.

### `/update`

- **Description:** Check for and install Sagittarius updates.
- **Usage:** `/update` (checks only), `/update install` (downloads and replaces binary).
- **Note:** Downloads the latest GitHub Release matching your OS/arch and replaces the running executable in place. If it fails with a permission error (e.g. installed in `/usr/local/bin`), run `sudo sagittarius --self-update` outside the TUI.

### `/quit`

- **Description:** Exit the interactive session.
- **Usage:** `/quit`
- **Note:** `Ctrl+C` exits when idle. While a turn is running, `Esc` (or `Ctrl+C`)
  cancels just that turn; a second `Ctrl+C` then exits.

### `run_script` (built-in tool)

Off by default. Set `sagittarius.scriptToolEnabled` to true (or toggle it in `/settings`)
to register a batch tool that runs several read-only operations in one turn — grep,
read, list, find_symbol, check-only `run_project_checks` — without an LLM hop between
them. The `script` argument is JSON:

```json
{"operations":[{"tool":"grep_search","args":{"pattern":"foo","dir_path":"src/"}}]}
```

A bare JSON array of the same objects is also accepted. Writes, shell, nested
`run_script`, `task`, and any tool that requires confirmation are rejected.
Each nested operation runs through the scheduler's project-boundary and
interaction-mode gates plus `BeforeTool`/`AfterTool` (with the nested tool's
own name). A hook deny or rewrite-revalidation failure is reported on that
operation and does not stop the rest of the batch.

### `task` / `code_task` (built-in tools)

Both off by default. `sagittarius.subagents.research.enabled` registers `task`,
which launches a read-only child that explores and reports back.
`sagittarius.subagents.coding.enabled` registers `code_task`, which launches a
write-capable child that must declare `write_paths` — the only paths it may
modify. Several coding subagents run in parallel; leases that overlap are
rejected before any of them starts, and a coding subagent's shell is read-only.
`code_task` asks for confirmation once, showing the lease. See
[subagents.md](../subagents.md).

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
| `Shift+Tab` | Cycle interaction mode (agent → plan → ask → debug) |
| `Alt+1` / `Alt+2` / `Alt+3` / `Alt+4` | Switch to agent / plan / ask / debug mode |
| `Ctrl+Shift+M` | Cycle interaction mode (alias of `Shift+Tab`) |
| `Ctrl+/` | Cycle forward through active models |
| `Ctrl+Shift+P` | Cycle backward through active models |
| `Alt+T` | Cycle the color theme (default ↔ greyscale) |
| `Ctrl+T` | Toggle the thinking ("reasoning") box |
| `Ctrl+Shift+D` | Export the last provider request to a JSON file (works mid-turn; same as `/chat debug`) |
| `Ctrl+O` | Expand or collapse a pasted text placeholder in place |
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

### `/chat`

- **Description:** Manage conversation checkpoints, title the session, fork it,
  export the chat, and debug the last request.
- **Usage:**
  - `/chat list`: List saved checkpoints and recent sessions.
  - `/chat save <tag> [force]`: Save the current conversation as a named checkpoint (a trailing `force` overwrites an existing one). A tag becomes a filename, so it is limited to letters, digits, `.`, `_` and `-`; a multi-word name is joined with dashes (`/chat save sglang testing` saves `sglang-testing`) and the resolved tag is echoed back.
  - `/chat resume <tag>` (alias `/chat load`): Restore a checkpoint into the live session. Tags are normalized as in `save`, so a two-word name resumes what it saved.
  - `/chat delete <tag>`: Delete a saved checkpoint.
  - `/chat rename <title>`: Set the current session's title (shown in session lists). Titles are trimmed, capped at 80 characters, and have control characters stripped.
  - `/chat fork`: Copy the current conversation into a **new** session and switch recording to it. The forked session inherits the title with a `" (fork)"` suffix and the recorded git branch. End-of-conversation only; fork-from-a-message is not yet supported.
  - `/chat share [file.md|file.json]`: Export the conversation to Markdown or JSON.
  - `/chat debug`: Write the most recent provider request to a JSON file. Works while a turn is running (`Ctrl+Shift+D` does the same export).
- **Session titles:** After your first full exchange Sagittarius proposes a short
  title automatically (see `sagittarius.sessions.autoTitle`: `prompt` asks with a
  one-key rename hint, `auto` applies silently, `off` disables it). `/chat rename`
  always overrides the title manually.

### `/toolkit`

- **Description:** Re-run the host toolkit checklist to see missing tools and install hints.
- **Usage:** `/toolkit` (run the scan), `/toolkit dismiss` (keep it from ever auto-showing again).
- **Notes:** The checklist runs automatically **once** — the first launch after
  install or first upgrade that surfaces it — and is marked as shown as soon as
  its report renders, so it never reappears on later launches. `/toolkit` is how
  you bring it back on demand. `/toolkit dismiss` sets the same flag before
  first launch (e.g. scripted installs) or clears any future auto-show.

### `/copy`

- **Description:** Copy the last assistant reply to the clipboard, without the
  TUI's scrollback chrome (no `✦` prefix, no fenced-code gutter).
- **Usage:**
  - `/copy`: the full last reply, including markdown fences and prose.
  - `/copy code`: only the bodies of fenced code blocks in that reply (language
    tags and ` ``` ` markers stripped). Multiple blocks are joined with a blank
    line. Use this when the model asked you to paste commands into a shell or
    another prompt.
- **Notes:** Mouse-select in the TUI still copies whatever is on screen. A
  fenced line that wrapped pastes with a newline at each wrap point; `/copy`
  and `/copy code` copy the exact last reply (or its fenced bodies) without
  those wraps. `/copy code` only covers the last assistant reply, so the hint
  under an older block still runs against the newest one. Fenced code is
  styled with no copyable `│ ` gutter. Tool-card borders (write_file diffs)
  are unchanged; `/copy` never includes those. If the last reply has no
  fenced code, `/copy code` says so and copies nothing.

### `/providers`

- **Description:** Manage provider connections — edit definitions, API keys, and
  activate models per provider.
- **Menu-first:** `/providers` opens directly at a provider list (the native
  Gemini built-in first, then custom providers alphabetically). Select a provider
  to open its edit sheet. Press `a` to add a provider; press `x` on a custom
  provider to open a delete confirmation screen (`y` or Enter confirms, Esc
  cancels). Gemini is the only native built-in and cannot be deleted. OpenAI,
  OpenAI-Responses, OpenRouter, and the other big-name providers are **templates**
  that create ordinary custom providers you can edit or remove.
- **Adding a provider (`a`) — template picker:** pressing `a` first shows a list
  of provider **templates** (OpenAI, OpenAI-Responses, OpenRouter, Anthropic,
  DeepSeek, xAI, z.ai, Groq, Together, Mistral, Fireworks) plus a **Custom Local (blank)**
  entry.
  - **Choosing a template** pre-fills the base URL, wire format, and API-key
    environment variable, then jumps straight to the **API key** step. The
    suggested provider id defaults to the template id (de-duplicated with a
    numeric suffix if it already exists). Any caveat (e.g. Anthropic's OpenAI-compat
    layer, z.ai's unavailable model discovery) is shown as a note.
  - **Custom Local (blank)** starts the field-by-field flow:
    1. **Provider name** — display name (required).
    2. **URL or host** — full URL (`http://127.0.0.1:8000`) or bare host (`127.0.0.1`).
    3. **Port** — shown only when the URL above has no port; defaults to `8000`.
    4. **Wire format** — toggle between `openai-chat` (default) and `openai-responses`.
    5. **API key env var** — optional environment variable name.
    6. **API key** — optional; stored in OS keychain.
    7. **Provider id** — auto-generated from the URL; edit to override before confirm.
  After submission the wizard discovers available models and prompts you to pick a
  default. When discovery returns nothing (e.g. a non-`/v1` endpoint like z.ai),
  the template's default model is offered and you can press `a` to type a model
  name manually.
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

- **Description:** Edit **per-model settings** — temperature, context limit,
  reasoning effort, thinking-box visibility, and the thinking budget — for any
  active `{Provider}/{Model}` pair.
- **Menu-first:** `/models` opens a global model list. Select a model to open its
  settings submenu. Changes are saved to `providers.<id>.models.<model>` in
  `settings.json` and take effect immediately for the active model.

#### Thinking budget

Two rows cap how long a model may reason before it has to act. They exist for
local models that reason indefinitely; hosted models with native adaptive
thinking rarely need them.

- **`thinkingBudgetTokens`** — reasoning tokens allowed per round. `0` (or
  clearing the row) turns it off. Whenever it is set, Sagittarius advertises it
  to the provider on every request: `reasoning_budget_tokens` plus a wrap-up
  message for llama.cpp-style servers, and `ThinkingConfig.ThinkingBudget` for
  Gemini 2.5. A server that enforces the budget itself steers the model into
  concluding mid-generation, which always beats cutting a stream after the fact.
  Gemini 3 has no numeric budget — use `/reasoning` there instead.
- **`hardThinkingBudget`** — `true` adds a client-side backstop for servers that
  ignore the advertised budget (vLLM and SGLang currently do). Once the streamed
  reasoning passes the budget and the model has not yet started an answer or a
  tool call, the round is abandoned and re-issued with the model's own partial
  reasoning quoted back to it and thinking switched off, so it answers from what
  it already worked out. The TUI prints a notice naming the budget. Default off:
  a cut costs a full extra round, so it is worth paying only for a model that
  genuinely loops.

A cut can never immediately follow another — the retry round runs with the
budget disabled — so a model that reasons at length twice in a row is left
alone rather than cut repeatedly. Later rounds in the same turn are protected
again.

### `/system-prompt`

- **Description:** Set the **project-wide** system-prompt personality (programmer,
  sysadmin, personal assistant, creative assistant × full/lite variants).
- **Menu-first:** `/system-prompt` opens a preset picker. Pass a preset id directly
  for headless use (e.g. `--slash "/system-prompt programmer-lite"`).
- **Persistence:** Saved to `<repo>/.sagittarius/settings.json` under
  `sagittarius.systemPrompt` and merged over the global default for the current
  workspace. Use `/providers` to set a per-provider override instead.

### `/mode`

- **Description:** Show the active interaction mode and its resolved model, or
  switch modes.
- **Usage:** `/mode` or `/mode show` (display only); `/mode agent`, `/mode plan`,
  `/mode ask`, `/mode debug` (switch); `/mode set <name>` (equivalent, explicit
  form).
- **Shortcuts:** `/agent`, `/plan`, `/ask`, `/debug` are top-level, argument-free
  aliases for `/mode <name>` — the same switch, one word instead of two. Prefer
  them when you already know which mode you want; `/mode` remains for checking
  the current mode or scripting the explicit `set` form.
- **See also:** `/modes` to assign a `{Provider}/{Model}` override per mode
  rather than switch which mode is active.

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
- **Categories:** General (`sagittarius.maxToolRounds`: default 100, `0` = no cap; interactive sessions show a Continue prompt at the cap), UI (`ui.theme`,
  `ui.showThinking`, `ui.hideBanner`), Security (`security.projectBoundary.enforce`),
  Snapshots (`sagittarius.snapshots.*`), Verify (`sagittarius.edit.enabled`
`sagittarius.verify.*`),
  Symbols (`sagittarius.symbols.*`).
- **Persistence:** Changes are saved immediately to the target scope file and take
  effect in the current session. Provider API keys and definitions are always global
  (edit them in `/providers`).

### `/memory`

- **Description:** Manage project memory files (`AGENTS.md`). Added memories live under
  one `## Sagittarius Added Memories` heading appended to the target file; everything
  else in the file is left untouched. A model-callable, confirmation-gated `save_memory`
  tool wraps the same `add` path (append-only — deletion is a user-only action here).

#### Sub-commands

- **`add [--project] <text>`**
  - **Description:** Append one memory entry. Defaults to the global
    `~/.sagittarius/AGENTS.md` (matching gemini-cli's `save_memory` target); `--project`
    targets the current repository's `AGENTS.md` instead (the same file `/init`
    populates). Reloads the system prompt so it applies to the very next turn.
  - **Usage:** `/memory add prefers pnpm over npm`, `/memory add --project CI takes about 40 minutes`
- **`list`**
  - **Description:** List every saved entry, numbered continuously with global entries
    first then project, e.g. `1. [global]  Prefers pnpm over npm in this repo.`
  - **Usage:** `/memory list`
- **`remove <n>`**
  - **Description:** Delete entry `n` (from `/memory list`'s numbering) and echo back the
    removed text. Re-reads the file fresh, so a hand-edit since the last `/memory list`
    is respected. Removing a file's last entry also removes its now-empty heading.
  - **Usage:** `/memory remove 2`
- **`reload`**
  - **Description:** Re-read memory files into the system prompt.
  - **Usage:** `/memory reload`

### `/constraints`

- **Description:** Pin a standing scope limit to the end of the system prompt,
  where it is re-injected every turn. **You should rarely need this.** Saying
  "just discuss this, don't change anything yet" in an ordinary message is the
  normal way to restrict a turn, and the system prompt is written so that a
  text-only reply to such a request is a correct and complete turn. `/constraints`
  exists for the case where that is not enough: a long session where context
  compression has summarized away the message you said it in, or a smaller local
  model that stops honoring a restriction several turns after you gave it.
  Constraints apply in every mode, outrank the mode suffix and the
  tool-invocation mandate, and survive `--resume`. They never expire on their
  own — clear them when the restriction lifts.
- **See also:** `/memory add --project` for a restriction that should outlive the
  session (it writes to `AGENTS.md`); `/mode plan` or `/mode ask` for a
  tool-level read-only gate rather than an instruction.

#### Sub-commands

- **`add <text>`**
  - **Description:** Add a standing constraint. A repeat of an existing one
    (case-insensitive) is a no-op.
  - **Usage:** `/constraints add do not modify AGENTS.md until I say so`
- **`list`**
  - **Description:** List active constraints, numbered.
  - **Usage:** `/constraints list` (runs mid-turn, unlike most slash commands)
- **`clear`**
  - **Description:** Remove every standing constraint.
  - **Usage:** `/constraints clear`

### `/readonly`

- **Description:** Set a durable, session-wide read-only inspection posture.
  While it is on, file writes, `edit`, `save_memory`, MCP tools, and any shell
  command the read-only classifier scores as mutating are denied by the
  scheduler, whatever the interaction mode says. Read-only shell commands
  (`systemctl status`, `journalctl`, `nginx -t`, SQL `SELECT`) still run;
  unrecognized ones prompt for confirmation. The posture survives `--resume`
  and never expires on its own.
- **Only two things gate tools:** the interaction mode (`/plan` and `/ask` are
  read-only) and this posture. Nothing is inferred from what you say — saying
  "don't change anything yet" is honored by the model through the system
  prompt, not by a hidden tool ban (see AD-107; before it, a phrase like that
  set a lock that `/readonly off` could not clear).
- **Getting out:** `/readonly off`, or switch to agent or debug mode, which
  lifts the posture and says so. Switching to plan or ask leaves it set, so a
  round trip through them does not silently drop it.
- **See also:** `--read-only` sets the posture at startup. `/constraints` is an
  instruction to the model rather than a tool gate.

#### Sub-commands

- **`on`**
  - **Description:** Enable the read-only inspection posture.
  - **Usage:** `/readonly on`
- **`off`**
  - **Description:** Disable it and allow changes again.
  - **Usage:** `/readonly off`
- **`status`**
  - **Description:** Report whether the posture is currently enabled. Bare
    `/readonly` does the same.
  - **Usage:** `/readonly status`

### `/skills`

- **Description:** Manage agent skills discovered from `SKILL.md` files.

#### Sub-commands

- **`list`**
  - **Description:** List discovered skills (user, workspace, extension paths).
  - **Usage:** `/skills list` or `/skills`
- **`reload`**
  - **Description:** Rescan skill directories and refresh the `activate_skill` tool schema.
  - **Usage:** `/skills reload`

To force a specific skill on a single message rather than leaving the choice to
the model, use an [`@skill:<name>` mention](#skillname).

### `/hooks`

- **Description:** Manage lifecycle hooks configured in `settings.json` (`hooks`).

#### Sub-commands

- **`list`**
  - **Description:** List configured lifecycle hooks, matching patterns, and status.
  - **Usage:** `/hooks list` or `/hooks`
- **`enable <name>`**
  - **Description:** Enable a specific hook by name or command key.
  - **Usage:** `/hooks enable mempalace-auto-save`
- **`disable <name>`**
  - **Description:** Disable a specific hook by name or command key.
  - **Usage:** `/hooks disable mempalace-auto-save`
- **`enable-all`**
  - **Description:** Globally enable execution of all lifecycle hooks.
  - **Usage:** `/hooks enable-all`
- **`disable-all`**
  - **Description:** Globally disable execution of all lifecycle hooks.
  - **Usage:** `/hooks disable-all`
- **`reload`**
  - **Description:** Reload lifecycle hook definitions from settings files.
  - **Usage:** `/hooks reload`
- **`test <name>`**
  - **Description:** Execute a dry-run test of a hook by name or command key.
  - **Usage:** `/hooks test mempalace-auto-save`
- **`trust <name>`**
  - **Description:** Trust a project hook by name or command key so it can run.
  - **Usage:** `/hooks trust mempalace-precompact`
- **`trust-all`**
  - **Description:** Trust every loaded project hook.
  - **Usage:** `/hooks trust-all`

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
- **`find_symbol`:** One of the built-in tools, this locates symbol definitions
  and references across source files using a syntax-aware parser (no persistent
  index). It is on by default; disable it with `sagittarius.symbols.enabled: false`
  (see [code quality](../code-quality.md)) to rely on an external code-intelligence
  MCP instead.

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

- **Description:** Show or override the reasoning effort / thinking depth for
  the active `{Provider}/{Model}`. Sagittarius defaults to genuine
  provider-native **adaptive** thinking where one exists (Gemini 3 / 2.5) and
  a capability-validated default everywhere else, rather than requiring an
  explicit opt-in per provider (AD-077).
- **Capability-aware:** the accepted levels and default behavior depend on the
  live model's resolved reasoning mechanism, not a fixed wire-format gate:
  - **Gemini 3 / 2.5 (`gemini-dynamic`):** adaptive by default (the model
    decides depth per turn via dynamic thinking). Pin a level
    (`minimal`/`low`/`medium`/`high`) to force one, or disable with
    `/reasoning none`. Only Gemini 3 supports a genuine fixed-level pin;
    pinning a level on Gemini 2.5 falls back to adaptive (its raw token budget
    isn't safely derivable from a generic effort string).
  - **OpenAI Responses reasoning families (`fixed-effort`)** (gpt-5 / gpt-5.1+
    / gpt-5.4+ / gpt-5-pro / o3 / o4): accepts that family's documented effort
    set (see `/reasoning show` for the live list); `gpt-5-pro` is
    **mandatory** and rejects `/reasoning none`.
  - **OpenRouter models with a discovered `reasoning` capability:** enabled by
    default with OpenRouter's own default effort once model discovery has run
    (`/providers` → discover models); no local effort validation beyond what
    OpenRouter reports.
  - **Everything else** (custom/z.ai/DeepSeek-preset/local vLLM endpoints, or
    any model with no known capability): stays opt-in — `/reasoning show`
    reports "not applicable" and a level set here is sent as a best-effort
    OpenRouter-style wire field that most non-OpenRouter servers ignore.
- **Live immediately:** a session override (or `save`) takes effect on the
  very next request — no restart or explicit rebuild needed.

#### Sub-commands

- **`show`**
  - **Description:** Show the resolved reasoning mechanism, effort, its
    source (session override / pinned setting / discovered default /
    provider default), and the valid levels for the live model.
  - **Usage:** `/reasoning` or `/reasoning show`
- **`clear`** (alias **`adaptive`**)
  - **Description:** Drop the session-only override and fall back to the
    capability-aware adaptive default (does not change `settings.json`).
  - **Usage:** `/reasoning clear` or `/reasoning adaptive`
- **`save <level>`**
  - **Description:** Persist `<level>` to `providers.<active>.reasoningEffort`
    (or the per-model override via `/models`).
  - **Usage:** `/reasoning save low`
- **`<none|minimal|low|medium|high|xhigh>`**
  - **Description:** Set a session-only reasoning override (not persisted),
    validated against the live model's accepted levels when known.
  - **Usage:** `/reasoning medium`

#### Notes

- The `/models` per-model settings editor shows a read-only "Reasoning:"
  capability hint (mechanism + valid levels) sourced from the same resolver.
- A model with no known reasoning capability (static rule or discovered
  OpenRouter data) reports "not applicable" — the command is always safe to
  run, but has no effect for such models beyond the best-effort wire field.

### `/diff`

- **Description:** Show the net unified diff of files Sagittarius changed this session (Sagittarius-specific; no fork equivalent).
- **Usage:** `/diff` for all changed files, or `/diff <path>` to filter by a path substring.
- **Notes:** Tracks `write_file` changes only. See [snapshots-and-undo.md](../snapshots-and-undo.md).

### `/undo`

- **Description:** Revert the most recent file change recorded this session (Sagittarius-specific; no fork equivalent).
- **Usage:** `/undo` reverts the last change; `/undo <n>` reverts the last `n` (most recent first).
- **Notes:** Restores prior file content (or removes newly created files). Disabled when `sagittarius.snapshots.enabled` is `false`.

## File and skill mentions (`@`)

An `@` token inside an ordinary message pulls extra context into **that one
message**. The scrollback and the session transcript keep exactly what you
typed; only the copy sent to the model carries the expansion, so a mention never
lingers in the conversation or in `settings.json`.

A mention is recognised only when the `@` starts a token, so `rob@example.com`
is left alone. Write `\@` to escape one. Both forms autocomplete inline as you
type, and an unresolvable mention cancels the turn with an error rather than
quietly dropping the context you asked for.

### `@path/to/file`

- **Description:** Inject a workspace file's contents into the message.
- **Usage:** `explain @internal/agent/app.go` — or `@"my file.go"` when the path
  contains spaces.
- **Notes:** The path must resolve inside the workspace and point at a readable
  text file; directories and binaries are rejected. Injection is capped at
  256 KiB per file and 512 KiB per message, with oversized content truncated.

### `@skill:<name>`

- **Description:** Load an installed skill's instructions for this message,
  instead of waiting for the model to pick it up on its own.
- **Usage:** `@skill:golang refactor this handler`
- **Notes:** The name is the skill's `name:` field (see `/skills list`), matched
  case-insensitively. The skill applies to that message only: the next message
  starts clean. Skill instructions are placed after any file contents in the
  same message so a large file cannot bury them, and they draw from the same
  512 KiB budget (skills are resolved first, so an explicit request is never
  starved by a big file).
- **Related:** `activate_skill` is the model's own path to the same content and
  stays available; a mention is the manual override for when you already know
  which skill applies.

## Headless CLI flags

Mentions work in headless runs too, since every entry point shares the same turn
loop: `sagittarius -p "@skill:golang fix the retry logic"`.

Unified keyboard + CLI quick reference: [README.md § Quick reference](../README.md#quick-reference).

These flags drive Sagittarius without a terminal, which is how agents (and CI)
exercise it. See [agent-testing.md](../agent-testing.md) for end-to-end recipes.

| Flag | Purpose |
|------|---------|
| `-p`, `--prompt <text>` | Run a single non-interactive turn and exit. |
| `--output-format <text\|json\|stream-json>` | Headless output shape. `stream-json` emits one JSON object per line. |
| `--approval-mode <default\|autoEdit\|yolo>` | Tool approval policy. `default` denies destructive tools headlessly; `yolo` runs all tools (path validation still applies). The fork alias `auto_edit` maps to `autoEdit`. |
| `-y`, `--yolo` | Shorthand for `--approval-mode=yolo`. Cannot be combined with `--approval-mode`. |
| `--mode <agent\|plan\|ask\|debug>` | Interaction mode for this run, overriding `sagittarius.defaultMode`. `ask` and `plan` enforce read-only tool policy. The fork's `--approval-mode plan` is not accepted; use `--mode plan` (AD-022). |
| `--read-only` | Start with the durable read-only inspection posture set (see [`/readonly`](#readonly)). Independent of `--mode`; lift it in-session with `/readonly off` or by switching to agent or debug. |
| `--slash <command>` | Run a single slash command headlessly (e.g. `--slash "/mode show"`, `--slash "/diff"`, `--slash "/undo"`) and exit. Mutually exclusive with `-p`. Commands that open an interactive dialog (bare `/providers`, `/models`) print a message and exit 2. |
| `-d`, `--debug` | Raise `slog`'s level to debug for operational log lines (interactive: `~/.sagittarius/logs/sagittarius.log`; headless: stderr). Independent of `--log-verbose` below. |
| `--log-verbose` | Write a full, human-readable transcript of every request sent to the model, every response, and every tool result to `~/.sagittarius/logs/chat-verbose-<session>.log`. Works with or without `--debug`, in interactive, headless (`-p`), and `--slash` runs. Intended for attaching to bug reports; rarely needed otherwise. Appends across `--resume` of the same session; a failure to open the file is a non-fatal warning, not a startup error. |

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

Implemented: `/about`, `/agent`, `/ask`, `/chat`, `/clear`, `/compress`,
`/constraints add|list|clear`,
`/copy` (`code`), `/debug`, `/diff`, `/goal`,
`/grill`, `/init`, `/memory add|list|remove|reload`, `/mcp` (list, reload, add/edit/remove wizard),
`/mode` (show, switch), `/modes` (override, clear headlessly), `/model`, `/models`, `/mouse`, `/plan`, `/reasoning`,
`/resume`, `/settings` (curated browser), `/skills` (list, reload), `/agents`
(list, reload), `/stats`, `/system-prompt`, `/theme`, `/tools` (list, desc,
enable/disable), `/undo`, `activate_skill` tool, `ask_user` tool, `save_memory` tool,
`run_script` tool (opt-in via `sagittarius.scriptToolEnabled`), `task` and
`code_task` tools (opt-in via `sagittarius.subagents.research.enabled` and
`sagittarius.subagents.coding.enabled`).
