# Local snapshots, `/diff`, and `/undo`

Sagittarius records the file changes it makes during a session so you can review
them before moving on and revert them if they are wrong. This is a lightweight,
local-only feature: it does not require git and never talks to a remote.

## How it works

Every time the agent runs the `write_file` tool, Sagittarius captures the file's
content immediately before and after the write. From that it can show you a
unified diff (`/diff`) and restore the previous bytes (`/undo`). Only changes
Sagittarius itself makes are tracked — unrelated edits you or other processes
make to the working tree are not.

Snapshots are stored outside your project, under
`~/.sagittarius/tmp/<slug>/snapshots/<sessionId>.jsonl`, so the agent's own file
tools (which are confined to the project root) can never reach or corrupt them.

> Design note: rather than a shadow git repository (the approach used by some
> other tools, which has had data-loss bugs around silent `git add` failures and
> multi-gigabyte diffs), Sagittarius stores before/after content directly and
> computes diffs in process. This is simpler, works in non-git projects, and is
> session-scoped by construction.

## Commands

| Command | Behavior |
|---------|----------|
| `/diff` | Show the net unified diff of every file changed this session. |
| `/diff <path>` | Only show files whose path contains `<path>`. |
| `/undo` | Revert the most recent file change. |
| `/undo <n>` | Revert the last `n` changes (most recent first). |

`/undo` restores the previous content of a file, or removes a file that did not
exist before the change. If a change cannot be reverted (see limitations) the
command reports it and still reverts everything else.

## Configuration

Snapshots are on by default. Configure them in `settings.json`, either globally
(`~/.sagittarius/settings.json`) or per project
(`<repo>/.sagittarius/settings.json`, which wins for that project):

```json
{
  "sagittarius": {
    "snapshots": {
      "enabled": true,
      "maxFileBytes": 2097152
    }
  }
}
```

- `enabled` — turn snapshotting on or off. Default `true`.
- `maxFileBytes` — files larger than this (default 2 MiB) are recorded as
  metadata only. Their diff is shown as "too large" and they cannot be reverted,
  which keeps a single huge generated file from blowing up memory or the index.

## Limitations

- Snapshotting covers `write_file` only. Files changed by `run_shell_command`
  are not snapshotted (use the project boundary or your own git history for
  those).
- `/diff` and `/undo` are interactive (TUI) commands. In headless mode (`-p`),
  inspect `~/.sagittarius/tmp/<slug>/snapshots/` or use git in your project.
- The session undo stack is in-memory for the current process; restarting the
  CLI starts a fresh stack (the on-disk index is kept for inspection).

## Project boundary enforcement

A related, independent feature blocks file mutations outside the project root.
See [SECURITY.md](../SECURITY.md#project-boundary-enforcement) for the boundary
flag and the shell heuristic.
