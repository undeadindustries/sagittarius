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

## MCP tools in read-only modes

`agent` and `debug` modes run any enabled MCP tool. The read-only modes — `ask`,
`plan`, and the `/readonly` posture — admit an MCP tool only when it is marked
read-only, because nothing else about a remote tool tells us whether calling it
changes something. There are two ways to mark one:

1. **The server declares it.** A tool whose MCP annotations set `readOnlyHint`
   is admitted automatically, but only from a server you have set
   `"trust": true` on. The MCP specification is explicit that annotations are
   hints which "are not guaranteed to provide a faithful description of tool
   behavior" and that clients "should never make tool use decisions based on
   ToolAnnotations received from untrusted servers", so an untrusted server
   cannot talk its way into `ask` mode by asserting its own harmlessness.
2. **You vouch for it.** Add the tool to the server's `readOnlyTools` list.
   Press `a` on the tool's row in `/tools`, or edit `settings.json` directly.
   This works regardless of trust or annotations, and is the only route for the
   many servers that ship no annotations at all.

```json
{
  "mcpServers": {
    "math": {
      "command": "mcp-server-calculator",
      "readOnlyTools": ["calculate", "convert"]
    }
  }
}
```

`readOnlyTools` is an allowlist rather than a blocklist on purpose: a server you
add next month must not gain access to a mode whose whole promise is that
nothing changes, just because you have not gotten around to excluding it. In
`/tools` an admitted tool is labeled `read-only`, or `read-only (declared)` when
the server's own annotation carried it — the declared kind is not editable
there, since removing it from your allowlist could not revoke the server's
annotation.

A tool's read-only state is resolved when the server connects, the same as
`trust`, so run `/mcp reload` after hand-editing `readOnlyTools`. The `a` key in
`/tools` reloads for you.

## Go code intelligence (gopls)

For Go projects you can add language-server intelligence (diagnostics,
definitions, references, hover) by pointing Sagittarius at `gopls`'s built-in MCP
server. This needs `gopls` v0.20+ on your `PATH`
(`go install golang.org/x/tools/gopls@latest`):

```json
{
  "mcpServers": {
    "gopls": {
      "command": "gopls",
      "args": ["mcp"],
      "cwd": ".",
      "trust": true
    }
  }
}
```

Its tools appear as `mcp_gopls_*` and are available in `agent`/`debug` modes;
in `plan`/`ask` they need a read-only mark like any other MCP tool (see above).
Detached `gopls mcp` sees saved files only, so write changes before requesting
diagnostics. `trust: true` is reasonable for read-only LSP tools — and it also
lets their `readOnlyHint` annotations be honored — but keep it `false` for
write-capable servers.
See [code-quality.md](../code-quality.md) for the broader verify workflow.

## Extensions

Extensions installed under `~/.sagittarius/extensions/` can declare additional
MCP servers and skills in their extension manifest. Extension MCP servers are
merged into the active server set at reload time and appear in `/mcp` as
read-only entries (view and reload, but not edit or remove here).

## Related

- Fork reference: `gemini-cli/docs/tools/mcp-server.md`
- Activate skills: `docs/reference/commands.md` (`/skills`, `activate_skill` tool)
