# Sagittarius home directory

Sagittarius keeps all of its global state under a dedicated home directory,
`~/.sagittarius`. This is separate from the gemini-cli fork's `~/.gemini`:
Sagittarius never reads or writes `~/.gemini`.

## First run

On first launch Sagittarius creates `~/.sagittarius` (mode `0700`). When you
start a session in a project for the first time it also records the project in a
small registry. Everything else is created lazily, only when the corresponding
feature is used, matching the fork's behavior.

### Interactive first run (no provider configured)

If you launch the TUI with no active provider or no API key for the active
provider, Sagittarius opens a first-run setup overlay instead of exiting:

1. Choose **Gemini**, **OpenRouter**, or a **custom OpenAI-compatible** endpoint.
2. Enter your API key (and base URL for custom endpoints).
3. Pick a starting model from a live discovery list.

Completing the wizard writes `settings.json`, stores credentials, and switches
you into a normal chat session. Headless mode (`-p`) still requires a configured
provider up front.

### Created eagerly

| Path | Purpose |
|------|---------|
| `~/.sagittarius/` | Global home directory |
| `~/.sagittarius/projects.json` | Maps each absolute project path to a short slug |
| `~/.sagittarius/tmp/<slug>/.project_root` | Ownership marker (contains the project path) |
| `~/.sagittarius/history/<slug>/.project_root` | Ownership marker (reserved for checkpointing) |

The slug is derived from the project's folder name (for example a project at
`/home/you/work/my-app` gets the slug `my-app`). If two different projects share
a folder name, the second one gets a numeric suffix (`my-app-1`).

### Created lazily (on first use)

| Path | Created when |
|------|--------------|
| `~/.sagittarius/settings.json` | You change a setting (first-run onboarding, providers wizard, `/mode`, etc.) |
| `~/.sagittarius/sagittarius-credentials.json` | You store an API key and no OS keychain is available |
| `~/.sagittarius/tmp/<slug>/chats/*.jsonl` | First conversation turn (session history) |
| `~/.sagittarius/tmp/<slug>/snapshots/<sessionId>.jsonl` | First snapshotted `write_file` (powers `/diff` and `/undo`) |
| `~/.sagittarius/skills/` | You add a user skill |
| `~/.sagittarius/agents/` | You add a user agent definition |
| `~/.sagittarius/extensions/<name>/` | You install an extension |

## Project-level directory

Per-project configuration lives in `<repo>/.sagittarius/`:

| Path | Purpose |
|------|---------|
| `<repo>/.sagittarius/settings.json` | Project settings (merged over global, trusted workspaces only) |
| `<repo>/.sagittarius/skills/` | Project skills |
| `<repo>/.sagittarius/agents/` | Project agent definitions |

The sibling `<repo>/.agents/skills/` and `~/.agents/skills/` skill roots are
still read when present.

### Project settings merge (dual-scope model)

Sagittarius uses a dual-scope settings model: a **global** file at
`~/.sagittarius/settings.json` and an optional **project** file at
`<repo>/.sagittarius/settings.json`. At startup both files are loaded and merged
into a single effective `Merged` view. The merged document is never written back,
so the project file can never leak into the global one.

**Merge rules (project wins):**

| Path pattern | Strategy |
|---|---|
| Scalars (`ui.theme`, `providers.active`, mode routing strings) | Project value replaces global when set |
| `mcpServers` | Shallow merge by server name (project adds/overrides; global is the base) |
| `providers.<id>.models` | Shallow merge by model id |
| `providers.<id>.activeModels` | Project replaces entirely when set |
| `sagittarius.modes.*` | Project value replaces global when set |
| `sagittarius.systemPrompt` | Project value replaces global when set |
| Credentials / API keys | Never in either file |

**What you can store per scope:**

| Setting | Default scope | Notes |
|---|---|---|
| Mode overrides (`/modes`) | **Project** | Per-repo agent/plan/ask/debug routing |
| Active model set (`/model`) | **Project** | Which models are active for this repo |
| MCP server definitions (`/mcp`) | **Project** | Servers can be global or project; scope is chosen in the wizard |
| System prompt preset (`/system-prompt`) | **Project** | Already writes project |
| UI preferences (`/settings` → UI) | **Global** | Personal prefs (theme, showThinking) |
| Provider definitions and API keys (`/providers`) | **Global** | Shared across all repos |
| Security / snapshot / verify settings (`/settings`) | Either | Use the scope radio in `/settings` |

**Saving to a scope:**
- In the TUI, overlay dialogs that write settings show an "Apply to" scope
  selector (Tab to focus, arrows to change between Global and Project).
- Headlessly, use `/modes override` and `/modes clear` with an optional
  `global|project` trailing argument (default: project).
- The `/settings` browser lets you view any key in the selected scope file,
  toggle booleans, edit values, and `Ctrl+L` to clear a key from that scope only
  (falling back to the other scope or the built-in default).

For snapshots and undo specifically see [snapshots-and-undo.md](snapshots-and-undo.md).

## Memory files: AGENTS.md

Sagittarius uses `AGENTS.md` for system-prompt memory, never `GEMINI.md`.

- Global memory: `~/.sagittarius/AGENTS.md` (optional; read if present).
- Project memory: `AGENTS.md` files discovered by walking up from your working
  directory to the home boundary. Outer files come first, inner files last.

Create these files yourself; Sagittarius does not generate them.

## Environment variables

| Variable | Effect |
|----------|--------|
| `SAGITTARIUS_HOME` | Overrides the home directory root. Paths become `$SAGITTARIUS_HOME/.sagittarius/…`. |
| `SAGITTARIUS_FORCE_FILE_STORAGE=true` | Store credentials in the encrypted file instead of the OS keychain. |
| `SAGITTARIUS_SYSTEM_SETTINGS_PATH` | Override the system settings path (`/etc/sagittarius/settings.json`). |
| `SAGITTARIUS_SYSTEM_DEFAULTS_PATH` | Override the system defaults path. |

## Migrating from gemini-cli

Sagittarius starts fresh. It does not import anything from `~/.gemini`. After
switching you will need to:

- Re-enter provider API keys (via first-run onboarding or the `/providers` wizard).
- Re-curate your providers and active models.

Session history recorded by gemini-cli under `~/.gemini/tmp/` will not appear in
`/resume`, because Sagittarius reads only `~/.sagittarius/tmp/`.
