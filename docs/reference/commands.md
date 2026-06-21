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
- **Note:** `Ctrl+C` also exits.

### `/providers`

- **Description:** Manage providers (built-in and custom OpenAI-compatible backends).
- **Menu-first:** `/providers` is a single-surface, menu-first command — it has
  no typed subcommands. Running it opens an interactive wizard:
  - **Switch active provider** — change the active provider (persisted to `settings.json`).
  - **Edit a provider** — pick a provider, then edit its API key, default model,
    and wire-format-gated settings (e.g. `temperature`, `baseUrl`,
    `contextLimit`, `toolCallParsing`, and for `openai-responses` also
    `reasoningEffort`, `useResponseChaining`). Gemini providers expose no
    editable instance settings (upstream owns those defaults) beyond the API key
    and default model.
  - **Set API key** — store an API key for the active provider in secure storage.
    There is no separate `/auth` command.
  - **Add provider** — register a custom OpenAI-compatible provider, then connect
    and discover its models so you can pick a default and switch to it immediately.
  - **Remove provider** — delete a custom provider (built-ins cannot be removed).
  - **Manage models (activate/deactivate)** — browse the provider's discovered
    models and toggle which ones are active. Models are active by default; the
    checked subset is saved to `providers.<id>.activeModels`. Providers without a
    model-discovery endpoint (e.g. Gemini) skip activation — set their model on
    the edit sheet instead.

### `/models`

- **Description:** Pick the active model for the **active provider**, from that
  provider's activated models.
- **Menu-first:** `/models` is a single-surface, menu-first command with no typed
  subcommands. It opens an interactive list of the active provider's active
  models (curated via `/providers` → Manage models; falls back to the configured
  default model when uncurated). Selecting one sets it as the live model and
  rebuilds the runner. Per-model setting edits (temperature, reasoning, …) are a
  future enhancement (AD-024) and are not yet available.

### `/memory`

- **Description:** Manage project memory files (`GEMINI.md`, `AGENTS.md`).

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

#### Sub-commands

- **`list`**
  - **Description:** Show MCP server connection status and discovered tool counts.
  - **Usage:** `/mcp list` or `/mcp`
- **`reload`**
  - **Description:** Reconnect MCP servers and rediscover tools.
  - **Usage:** `/mcp reload`

See also: [MCP server configuration](../tools/mcp-server.md).

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

## Deferred commands

The following fork commands are **not** implemented yet. They will be added
incrementally; track gaps in `AGENTS.md`.

| Command | Planned phase |
|---------|----------------|
| `/about`, `/bug`, `/chat`, `/clear`, `/compress`, `/copy` | Post-parity / incremental |
| `/commands`, `/directory`, `/extensions` | Post-parity / incremental |
| `/mcp auth`, `/mcp enable`/`disable` | Phase 12+ incremental |
| `/skills enable`/`disable`/`link` | Phase 12+ incremental |
| `/agents enable`/`disable`/`config` | Phase 12+ incremental |
| `/auth signin` / OAuth dialogs | Deferred auth paths |
| ACP headless registry | Post-parity |

Implemented in Phase 12: `/mcp` (list, reload), `/skills` (list, reload), `/agents` (list, reload), `activate_skill` tool.
