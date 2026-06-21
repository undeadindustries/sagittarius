# MCP servers with Sagittarius

Sagittarius connects to Model Context Protocol (MCP) servers configured in
`~/.gemini/settings.json` under `mcpServers`. Discovered tools are registered
with the `mcp_<server>_<tool>` naming convention and appear alongside built-in
tools on every generate request.

## Configuration

Add servers to `settings.json`:

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
| `/mcp list` | Show configured servers and connection status |
| `/mcp reload` | Reconnect servers and rediscover tools |

## Extensions

Extensions installed under `~/.gemini/extensions/` can declare additional MCP
servers and skills in `gemini-extension.json`. Extension MCP servers are merged
into the active server set at reload time.

## Related

- Fork reference: `gemini-cli/docs/tools/mcp-server.md`
- Activate skills: `docs/reference/commands.md` (`/skills`, `activate_skill` tool)
