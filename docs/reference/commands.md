# CLI commands

Sagittarius supports built-in slash commands for session control, provider
configuration, and authentication. Commands start with `/`.

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

### `/provider`

- **Description:** Manage providers (built-in and custom OpenAI-compatible backends).

#### Sub-commands

- **`list`**
  - **Description:** List configured providers and which one is active.
  - **Usage:** `/provider list`
- **`use <id>`**
  - **Description:** Switch the active provider (persisted to `settings.json`).
  - **Usage:** `/provider use openai`
- **`show`**
  - **Description:** Show the active provider, model, wire format, and base URL.
  - **Usage:** `/provider show`
- **`set <id> <field> <value>`**
  - **Description:** Set `model`, `baseUrl`, or `key` on a non-Gemini provider.
  - **Usage:** `/provider set openai model gpt-4o`
  - **Fork rule:** `/provider set gemini-apikey key …` is rejected — use `/auth`.
- **`add <id> <baseUrl> [displayName] [apiKeyEnvVar]`**
  - **Description:** Register a custom OpenAI-compatible provider.
  - **Usage:** `/provider add local-vllm http://127.0.0.1:8000/v1`
- **`remove <id>`**
  - **Description:** Remove a custom provider (built-ins cannot be removed).
  - **Usage:** `/provider remove local-vllm`

### `/model`

- **Description:** List or set the model for the active provider.

#### Sub-commands

- **`list`**
  - **Description:** Query `GET /v1/models` on the active OpenAI-compat endpoint.
  - **Usage:** `/model list`

#### Usage (set model)

- **Usage:** `/model <model-id>`
- **Example:** `/model gpt-4o-mini`

### `/auth`

- **Description:** Store an API key for the **active** provider in secure storage.
- **Usage:** `/auth <api-key>` or `/auth set <api-key>`
- **Note:** Keys are redacted in scrollback. For Gemini, use `/auth` (not `/provider set … key`).

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
