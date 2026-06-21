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
