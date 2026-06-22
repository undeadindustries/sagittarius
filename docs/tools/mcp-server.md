# MCP servers with Sagittarius

Sagittarius connects to Model Context Protocol (MCP) servers configured in
`~/.sagittarius/settings.json` under `mcpServers`. Discovered tools are
registered with the `mcp_<server>_<tool>` naming convention and appear alongside
built-in tools on every generate request.

## Configuration

The `/mcp` wizard adds, edits, enables/disables, and removes servers for you, so
you rarely need to hand-edit JSON. If you prefer, you can still add servers
directly to `settings.json`:

```json
{
  "mcpServers": {
    "my-server": {
      "command": "npx",
      "args": ["-y", "some-mcp-server"],
      "env": {
        "API_KEY": "${MY_API_KEY}"
      }
    },
    "remote": {
      "url": "http://127.0.0.1:8080/mcp",
      "type": "http",
      "headers": {
        "Authorization": "Bearer ${MCP_TOKEN}"
      }
    }
  }
}
```

### Transport types

| Field | Transport |
|-------|-----------|
| `command` + optional `args`, `env`, `cwd` | Stdio (subprocess) |
| `url` or `httpUrl` with `"type": "http"` | Streamable HTTP |
| `url` with `"type": "sse"` or URL containing `/sse` | SSE |

### Security

- Secrets belong in environment variables or secure storage — not in
  `settings.json` (see `SECURITY.md`).
- Optional bearer tokens for MCP servers can be stored via the credentials
  layer (`sagittarius-mcp-<server>` service naming).
- OAuth MCP authentication is **deferred** in v1; complex OAuth flows will be
  added in a follow-up.

## Slash commands

| Command | Description |
|---------|-------------|
| `/mcp` | Open the server wizard: add, edit, enable/disable, remove, reload |
| `/mcp list` | Show configured servers and connection status (text) |
| `/mcp reload` | Reconnect servers and rediscover tools |
| `/tools` | Browse the effective tool inventory and toggle MCP tools |
| `/tools list` | List built-in and MCP tools as text |
| `/tools desc` | List tools with descriptions |

In the `/mcp` wizard, bearer tokens entered for an HTTP/SSE server are stored in
the credentials layer (never written to `settings.json`). Per-tool enable and
disable lives in `/tools`, which persists each server's `includeTools` /
`excludeTools` filter.

## Extensions

Extensions installed under `~/.sagittarius/extensions/` can declare additional
MCP servers and skills in their extension manifest. Extension MCP servers are
merged into the active server set at reload time and appear in `/mcp` as
read-only entries (view and reload, but not edit or remove here).

## Related

- Fork reference: `gemini-cli/docs/tools/mcp-server.md`
- Activate skills: `docs/reference/commands.md` (`/skills`, `activate_skill` tool)
