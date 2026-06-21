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
- **Interactive:** Running `/providers` with no sub-command opens an interactive
  menu (wizard): switch the active provider, edit a provider's settings, set an
  API key, add a custom provider (with immediate model discovery so you can pick
  a default model), remove a custom provider, or browse a provider's models.
- **API keys:** There is no separate `/auth` command. Set API keys from the
  wizard's "Set API key" screen, or via `/providers set <id> key <api-key>` for
  scripting.

#### Sub-commands (text operations)

- **`list`**
  - **Description:** List configured providers and which one is active.
  - **Usage:** `/providers list`
- **`use <id>`**
  - **Description:** Switch the active provider (persisted to `settings.json`).
  - **Usage:** `/providers use openai`
- **`show`**
  - **Description:** Show the active provider, model, wire format, and base URL.
  - **Usage:** `/providers show`
- **`set <id> <field> <value>`**
  - **Description:** Set any editable setting for a provider. Allowed fields depend
    on the provider's wire format (e.g. `model`, `baseUrl`, `temperature`,
    `contextLimit`, `toolCallParsing`, and for `openai-responses` also
    `reasoningEffort`, `useResponseChaining`). The special field `key` stores an
    API key in secure storage.
  - **Usage:** `/providers set openai temperature 0.2`
  - **Note:** Gemini providers expose no editable settings (upstream owns those
    defaults); only `key` is accepted for Gemini.
- **`add <id> <baseUrl> [displayName] [apiKeyEnvVar]`**
  - **Description:** Register a custom OpenAI-compatible provider.
  - **Usage:** `/providers add local-vllm http://127.0.0.1:8000/v1`
- **`remove <id>`**
  - **Description:** Remove a custom provider (built-ins cannot be removed).
  - **Usage:** `/providers remove local-vllm`

### `/model`

- **Description:** List or set the model for the active provider.

#### Sub-commands

- **`list`**
  - **Description:** Query `GET /v1/models` on the active OpenAI-compat endpoint.
  - **Usage:** `/model list`

#### Usage (set model)

- **Usage:** `/model <model-id>`
- **Example:** `/model gpt-4o-mini`

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
