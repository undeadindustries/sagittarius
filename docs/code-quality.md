# Code quality: verify and diagnostics

Sagittarius aims to leave code as clean as a well-configured IDE would: linted,
formatted, type-checked, and building. It does this the way OpenCode and similar
agents do — by running the project's own tooling, not by bundling linters into
the binary.

There are three layers, from lightest to most capable.

## 1. Verify via the project's tooling

The agent is prompted to verify after every code change and before declaring a
task done. It discovers how a project wants to be checked in this order:

1. **Project scripts** — `Makefile` targets, `package.json` `scripts`,
   `pyproject.toml`, `Cargo.toml`, pre-commit, CI config.
2. **Config files** — `.golangci.yml`, `eslint.config.*`, `ruff.toml`,
   `tsconfig.json`, and friends.
3. **Language defaults** — the standard checker for the detected language.

If the expected checker is not installed, the agent tells you once how to install
it (for example, "run `pip install ruff` and I can lint") instead of silently
skipping the check.

### The `run_project_checks` tool

`run_project_checks` is a built-in tool that auto-detects the stack from marker
files (`go.mod`, `package.json`, `pyproject.toml`, `Cargo.toml`) and runs the
read-only checks for that stack. It returns a structured result listing each
check, whether it passed, and any missing tools with an install hint.

| Parameter | Type | Default | Meaning |
|-----------|------|---------|---------|
| `paths` | string[] | (all) | Limit file-scoped checks (format, lint) to these paths. Build/test still run on the whole module. |
| `fix` | bool | `false` | Run mutating formatters/auto-fixers. Disabled unless `sagittarius.verify.allowFix` is `true`. |

Check-only runs (`fix=false`) are read-only and are allowed in `plan` and `ask`
modes. Mutating runs (`fix=true`) are blocked in `plan`/`ask` and gated behind
`sagittarius.verify.allowFix` (default off), because formatter rewrites are not
captured by `/undo`.

When the tool does not cover a project's needs, the agent falls back to
`run_shell_command` to run scripts like `make lint` or `npm test`.

### The `verify-after-edit` skill

A ready-to-use skill template lives at
[docs/skills/verify-after-edit/SKILL.md](skills/verify-after-edit/SKILL.md). Copy
it into your skills directory to reinforce the verify workflow:

- Global: `~/.sagittarius/skills/verify-after-edit/SKILL.md`
- Project: `<repo>/.sagittarius/skills/verify-after-edit/SKILL.md`

See [home-directory.md](home-directory.md) for skill discovery details.

## 2. Optional: suggest verification after writes

Set `sagittarius.verify.suggestAfterWrite` to `true` to have the agent receive a
one-line reminder to verify after a turn that wrote files. It is a hint only; the
agent still chooses what to run. This is off by default to avoid extra latency
and tokens.

```json
{
  "sagittarius": {
    "verify": {
      "suggestAfterWrite": true,
      "allowFix": false
    }
  }
}
```

## 3. Code navigation via find_symbol

The built-in `find_symbol` tool answers "where is this defined / referenced?"
across many languages using a syntax-aware parser (a pure-Go tree-sitter
runtime). Prefer it over `grep_search` when you know a symbol name and want its
definition or call sites; with no `symbol` argument it returns an outline of all
definitions in a file or directory.

It is stateless: each call parses only the files needed to answer that call, in
memory, and keeps no index, cache, or background watcher. There is nothing to
reindex when files change — the next call reparses from scratch. Because it is
read-only, it is available in every interaction mode (including `plan` and
`ask`).

It is on by default. To turn it off (for example to rely on an external
code-intelligence MCP instead), set:

```json
{
  "sagittarius": {
    "symbols": {
      "enabled": false
    }
  }
}
```

`symbols.preferGopls` (default `true`) only controls whether the tool's
description points at gopls MCP tools on Go modules; it never couples the tools
at runtime. Both keys resolve project-over-global.

Release binaries embed the curated Core100 grammar set (the `grammar_set_core`
build tag in the `Makefile` and `.goreleaser.yaml`) rather than all ~200
grammars, keeping the binary smaller. If you need `find_symbol` for a language
outside that set, build from source without the tag (`go build ./cmd/sagittarius`).

## 4. Optional: Go code intelligence via gopls (MCP)

For richer Go diagnostics and navigation (definitions, references, hover,
workspace diagnostics), Sagittarius can talk to `gopls`'s built-in MCP server
through its existing [MCP client](tools/mcp-server.md). This needs `gopls` v0.20+
on your `PATH`:

```bash
go install golang.org/x/tools/gopls@latest
```

Then add a server to `~/.sagittarius/settings.json`:

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

Notes:

- Detached `gopls mcp` sees saved files only, so the agent must write changes
  before asking for diagnostics.
- Tools appear as `mcp_gopls_*` and, like all MCP tools, are available in `agent`
  and `debug` modes but blocked in `plan`/`ask`.
- `trust: true` is reasonable for read-only LSP tools; keep `trust: false` for
  MCP servers that can mutate files.

Sagittarius does not embed a language server or linters. This keeps the binary
small and lets each project own its toolchain — the same trade-off OpenCode
makes.
